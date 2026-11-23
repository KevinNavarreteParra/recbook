package eval

import (
	"cmp"
	"context"
	"database/sql"
	_ "embed"
	"slices"

	"github.com/KevinNavarreteParra/recbook/internal/recommend"
	"github.com/KevinNavarreteParra/recbook/internal/store"
)

//go:embed borrows.sql
var borrowsSQL string

// BorrowMethods are the methods Discriminate scores borrows by, in the
// order they're reported, which is also the order of the by* columns.
// "shorter" isn't a likeness: it's there to show how much of finishing a
// borrow is just its length.
var BorrowMethods = []string{"content", "genre", "same author", "same shelf", "shorter", "blend"}

// The score columns. borrows.sql returns all but the blend, which is made
// from content and genre.
const (
	byContent = iota
	byGenre
	byAuthor
	byShelf
	byLength
	byBlend
)

// Discrimination is how well each method tells the books I borrowed and
// finished from those I borrowed and gave up on (spec §11.2), by how much
// each resembles the other books I finished.
type Discrimination struct {
	Finished int // borrows I finished
	GaveUp   int // borrows I didn't
	Lapsed   int // ...of which the loan just ran out (low confidence)
	Scores   []BorrowScore
}

// BorrowScore is one method's result.
type BorrowScore struct {
	Method string
	// AUC is the chance that a finished borrow, picked at random, scores
	// higher than a given-up one: 0.5 is chance, 1 a perfect split.
	AUC    float64
	Scored int // borrows the method could score at all
}

// borrow is one labeled borrow and its score by each of BorrowMethods
// (NULL where the method can't score it).
type borrow struct {
	finished bool
	scores   []sql.NullFloat64
}

// Discriminate scores each borrow I finished or gave up on by each method,
// and how well the scores separate the two.
func Discriminate(ctx context.Context, st *store.Store, opts Options) (*Discrimination, error) {
	rows, err := st.DB().QueryContext(ctx, borrowsSQL,
		sql.Named("model", opts.Model), sql.Named("unweighted", opts.Unweighted))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	d := &Discrimination{}
	var borrows []borrow
	for rows.Next() {
		var id int64
		var confidence string
		b := borrow{scores: make([]sql.NullFloat64, len(BorrowMethods))}
		columns := []any{&id, &b.finished, &confidence}
		for i := range byBlend {
			columns = append(columns, &b.scores[i])
		}
		if err := rows.Scan(columns...); err != nil {
			return nil, err
		}
		switch {
		case b.finished:
			d.Finished++
		case confidence == "low":
			d.GaveUp++
			d.Lapsed++
		default:
			d.GaveUp++
		}
		borrows = append(borrows, b)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	blend := opts.Blend
	if blend == nil {
		blend = recommend.DefaultBlend
	}
	blendScores(borrows, map[int]float64{byContent: blend[recommend.Content], byGenre: blend[recommend.Genre]}, byBlend)

	for i, m := range BorrowMethods {
		var scores []float64
		var finished []bool
		for _, b := range borrows {
			if b.scores[i].Valid {
				scores = append(scores, b.scores[i].Float64)
				finished = append(finished, b.finished)
			}
		}
		d.Scores = append(d.Scores, BorrowScore{Method: m, AUC: auc(scores, finished), Scored: len(scores)})
	}
	return d, nil
}

// blendScores fills column out with a blend of the columns in weights, as
// spec §8 blends views: each column's scores are turned into percentiles
// over the borrows, so they share a scale, and a borrow's blend is their
// weighted mean over the columns it has.
func blendScores(borrows []borrow, weights map[int]float64, out int) {
	percentiles := map[int][]float64{}
	for col := range weights {
		var scores []float64
		for _, b := range borrows {
			if b.scores[col].Valid {
				scores = append(scores, b.scores[col].Float64)
			}
		}
		ranks := midranks(scores)
		p := make([]float64, len(borrows))
		j := 0
		for i, b := range borrows {
			if b.scores[col].Valid {
				p[i] = ranks[j] / float64(len(scores))
				j++
			}
		}
		percentiles[col] = p
	}
	for i := range borrows {
		var sum, total float64
		for col, w := range weights {
			if w > 0 && borrows[i].scores[col].Valid {
				sum += w * percentiles[col][i]
				total += w
			}
		}
		if total > 0 {
			borrows[i].scores[out] = sql.NullFloat64{Float64: sum / total, Valid: true}
		}
	}
}

// auc is the area under the ROC curve of scores for telling the positives
// apart: the Mann-Whitney U of the positives' ranks, over every pair of a
// positive and a negative. Tied scores share their ranks, so a tie counts
// half. It's NaN with no positives or no negatives.
func auc(scores []float64, positive []bool) float64 {
	ranks := midranks(scores)
	var pos, neg, rankSum float64
	for i, p := range positive {
		if p {
			pos++
			rankSum += ranks[i]
		} else {
			neg++
		}
	}
	return (rankSum - pos*(pos+1)/2) / (pos * neg)
}

// midranks returns each score's rank among scores, 1 for the lowest, with
// tied scores all given the mean of the ranks they span.
func midranks(scores []float64) []float64 {
	order := make([]int, len(scores))
	for i := range order {
		order[i] = i
	}
	slices.SortFunc(order, func(a, b int) int { return cmp.Compare(scores[a], scores[b]) })
	ranks := make([]float64, len(scores))
	for lo := 0; lo < len(order); {
		hi := lo + 1
		for hi < len(order) && scores[order[hi]] == scores[order[lo]] {
			hi++
		}
		mid := float64(lo+hi+1) / 2 // mean of ranks lo+1..hi
		for _, i := range order[lo:hi] {
			ranks[i] = mid
		}
		lo = hi
	}
	return ranks
}
