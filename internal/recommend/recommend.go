// Package recommend finds borrowable audiobooks like a set of seed books
// (spec §8, content view).
//
// Each seed is compared with every borrowable work on its own, and the
// per-seed rankings are then merged by reciprocal-rank fusion, so a mixed
// set of seeds (Clancy and Dostoevsky) yields some of each rather than the
// muddle a single averaged query would. Seeds count in proportion to their
// tier's weight. Works close to an "avoid" seed are pushed down, and a cap
// per author keeps one author from taking over the list.
package recommend

import (
	"cmp"
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"slices"
	"strings"

	"github.com/KevinNavarreteParra/recbook/internal/embed"
	"github.com/KevinNavarreteParra/recbook/internal/store"
)

var (
	//go:embed seed.sql
	seedSQL string
	//go:embed neighbours.sql
	neighboursSQL string
)

// Fusion settings. rrfK is the usual constant for reciprocal-rank fusion:
// it keeps the top few ranks of any one list from dominating. perSeed is how
// far down each seed's ranking fusion looks. avoidWeight is how hard an
// avoid seed pushes its neighbours down, relative to a positive seed.
const (
	rrfK        = 60
	perSeed     = 200
	avoidWeight = 0.5
)

// Options are a recommendation request.
type Options struct {
	Seeds []int64 // works I want more like (see search.Find)
	Avoid []int64 // works I want less like

	Model        string // embedding model; default embed.DefaultModel
	AvailableNow bool   // only what can be borrowed right now
	MaxWaitDays  int    // only what's available within this many days; <0 for no limit
	IncludeRead  bool   // also recommend books I've finished
	Unweighted   bool   // count every seed equally, ignoring tiers
	PerAuthor    int    // at most this many results by one author; 0 for no cap
	Limit        int    // at most this many results; 0 means 20
}

// Seed is a seed as it was used.
type Seed struct {
	WorkID   int64
	Title    string
	Author   string
	Weight   float64
	Embedded bool // false: it has no description to compare, so it was skipped
	vector   []float32
}

// Rec is one recommendation.
type Rec struct {
	WorkID      int64
	Title       string
	Author      string
	Narrator    string
	DurationMin sql.NullInt64
	Abridged    bool
	Available   bool
	WaitDays    sql.NullInt64
	Holds       sql.NullInt64
	URL         string
	Tier        string // my relation to it, if any

	Score      float64 // fused score; only the order means anything
	Like       string  // the seed it's most similar to: the "why"
	Similarity float64 // ...and how similar, as a cosine
}

// Similar returns recommendations for the seeds, best first, along with
// the seeds as they were used.
func Similar(ctx context.Context, st *store.Store, opts Options) ([]Rec, []Seed, error) {
	if opts.Model == "" {
		opts.Model = embed.DefaultModel
	}
	if opts.Limit == 0 {
		opts.Limit = 20
	}

	seeds, err := loadSeeds(ctx, st.DB(), opts.Model, opts.Seeds)
	if err != nil {
		return nil, nil, err
	}
	avoid, err := loadSeeds(ctx, st.DB(), opts.Model, opts.Avoid)
	if err != nil {
		return nil, nil, err
	}
	exclude := append(slices.Clone(opts.Seeds), opts.Avoid...)

	type candidate struct {
		Rec
		score float64
	}
	cands := map[int64]*candidate{}

	for _, s := range seeds {
		if !s.Embedded {
			continue
		}
		weight := s.Weight
		if opts.Unweighted {
			weight = 1
		}
		near, err := neighbours(ctx, st.DB(), s.vector, exclude, opts)
		if err != nil {
			return nil, nil, err
		}
		for rank, n := range near {
			c, ok := cands[n.WorkID]
			if !ok {
				c = &candidate{Rec: n}
				c.Similarity = -1
				cands[n.WorkID] = c
			}
			c.score += weight / float64(rrfK+rank+1)
			if n.Similarity > c.Similarity {
				c.Like, c.Similarity = s.Title, n.Similarity
			}
		}
	}
	for _, a := range avoid {
		if !a.Embedded {
			continue
		}
		near, err := neighbours(ctx, st.DB(), a.vector, exclude, opts)
		if err != nil {
			return nil, nil, err
		}
		for rank, n := range near {
			if c, ok := cands[n.WorkID]; ok {
				c.score -= avoidWeight / float64(rrfK+rank+1)
			}
		}
	}

	ranked := make([]*candidate, 0, len(cands))
	for _, c := range cands {
		ranked = append(ranked, c)
	}
	slices.SortFunc(ranked, func(a, b *candidate) int {
		if c := cmp.Compare(b.score, a.score); c != 0 {
			return c // higher score first
		}
		return cmp.Compare(a.WorkID, b.WorkID) // ties: a stable order
	})

	var out []Rec
	byAuthor := map[string]int{}
	for _, c := range ranked {
		if len(out) == opts.Limit {
			break
		}
		author := strings.ToLower(c.Author)
		if opts.PerAuthor > 0 && byAuthor[author] >= opts.PerAuthor {
			continue
		}
		byAuthor[author]++
		c.Score = c.score
		out = append(out, c.Rec)
	}
	return out, append(seeds, avoid...), nil
}

func loadSeeds(ctx context.Context, db *sql.DB, model string, ids []int64) ([]Seed, error) {
	var seeds []Seed
	for _, id := range ids {
		s := Seed{WorkID: id}
		var vector any
		err := db.QueryRowContext(ctx, seedSQL, sql.Named("model", model), sql.Named("work_id", id)).
			Scan(&s.Title, &s.Author, &s.Weight, &vector)
		if err != nil {
			return nil, fmt.Errorf("seed %d: %w (has `recbook build` run since the last embed?)", id, err)
		}
		if list, ok := vector.([]any); ok {
			s.vector = make([]float32, len(list))
			for i, x := range list {
				s.vector[i] = x.(float32)
			}
			s.Embedded = true
		}
		seeds = append(seeds, s)
	}
	return seeds, nil
}

func neighbours(ctx context.Context, db *sql.DB, vector []float32, exclude []int64, opts Options) ([]Rec, error) {
	rows, err := db.QueryContext(ctx, neighboursSQL,
		sql.Named("seed", vector),
		sql.Named("model", opts.Model),
		sql.Named("exclude", exclude),
		sql.Named("include_read", opts.IncludeRead),
		sql.Named("available_now", opts.AvailableNow),
		sql.Named("max_wait", opts.MaxWaitDays),
		sql.Named("k", perSeed))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Rec
	for rows.Next() {
		var r Rec
		var author, narrator, url, tier sql.NullString
		err := rows.Scan(&r.WorkID, &r.Title, &author, &narrator, &r.DurationMin, &r.Abridged,
			&r.Available, &r.WaitDays, &r.Holds, &url, &tier, &r.Similarity)
		if err != nil {
			return nil, err
		}
		r.Author, r.Narrator, r.URL, r.Tier = author.String, narrator.String, url.String, tier.String
		out = append(out, r)
	}
	return out, rows.Err()
}
