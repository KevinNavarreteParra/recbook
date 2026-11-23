package recommend

import (
	"cmp"
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"slices"

	"github.com/KevinNavarreteParra/recbook/internal/embed"
	"github.com/KevinNavarreteParra/recbook/internal/graph"
	"github.com/KevinNavarreteParra/recbook/internal/store"
)

// For you: recommendations from my whole history at once, with every book
// I've finished as a seed (#3).
//
// Fusing that many seeds' rankings by reciprocal rank, as Similar does for
// a few, rewards breadth: a book fairly near fifty seeds outscores the
// nearest book of all to five. With four hundred seeds the top fills with
// surveys that every seed lists somewhere, and forty-odd philosophy seeds
// shelved alike vote as one block; the evaluation's history protocol put
// 0.4% of held-out books in the top 50.
//
// Two cures were measured. Grouping the seeds by taste, fusing each group
// alone and giving the groups places in proportion to their weight put
// 6.7% there. One walk of the graph view from every finished book at once
// put 9.9%, with an MRR twice as high, and it's the one used. A walk adds
// up what it reaches from each seed, so a book near several seeds gains,
// but it mostly stays near each seed, so no one taste takes over: the top
// 30 still spans two dozen shelves.

var (
	//go:embed history_seeds.sql
	historySeedsSQL string
	//go:embed for_you.sql
	forYouSQL string
	//go:embed for_you_list.sql
	forYouListSQL string
)

// ForYouDepth is how long the stored list is (BuildForYou): deep enough
// that a page is left after keeping only what's available now, say.
const ForYouDepth = 500

// History is the seeds of a "for you" list: every book I've finished
// that the graph can walk from, and the books I gave up on, whose likes
// are pushed down.
type History struct {
	Seeds []Seed
	Avoid []Seed
}

var errNoHistory = errors.New("no finished book has a description or subjects to compare (have build and embed run?)")

// LoadHistory loads my finished books as seeds and the books I gave up
// on as avoid seeds, leaving out any the graph view can't compare.
func LoadHistory(ctx context.Context, st *store.Store, model string) (*History, error) {
	if model == "" {
		model = embed.DefaultModel
	}
	rows, err := st.DB().QueryContext(ctx, historySeedsSQL)
	if err != nil {
		return nil, err
	}
	var finished, gaveUp []int64
	for rows.Next() {
		var id int64
		var isFinished bool
		if err := rows.Scan(&id, &isFinished); err != nil {
			rows.Close()
			return nil, err
		}
		if isFinished {
			finished = append(finished, id)
		} else {
			gaveUp = append(gaveUp, id)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	h := &History{}
	for _, list := range []struct {
		ids  []int64
		into *[]Seed
	}{{finished, &h.Seeds}, {gaveUp, &h.Avoid}} {
		seeds, err := LoadSeeds(ctx, st, model, list.ids)
		if err != nil {
			return nil, err
		}
		for _, s := range seeds {
			if s.Has(Graph) {
				*list.into = append(*list.into, s)
			}
		}
	}
	if len(h.Seeds) == 0 {
		return nil, errNoHistory
	}
	return h, nil
}

// WalkHistory walks the graph from every seed at once, each weighted by
// its tier's weight, less avoidWeight times a walk from the avoid seeds,
// and returns every work's score, best first. Works the walks don't
// reach, or reach less than the avoid walk does, are left out.
func WalkHistory(g *graph.Graph, seeds, avoid []Seed, alpha float64) []graph.Score {
	weights := func(seeds []Seed) map[int64]float64 {
		out := map[int64]float64{}
		for _, s := range seeds {
			out[s.WorkID] += s.Weight
		}
		return out
	}
	score := map[int64]float64{}
	for _, s := range g.Rank(weights(seeds), alpha) {
		score[s.WorkID] += s.Score
	}
	if len(avoid) > 0 {
		for _, s := range g.Rank(weights(avoid), alpha) {
			score[s.WorkID] -= avoidWeight * s.Score
		}
	}
	out := make([]graph.Score, 0, len(score))
	for id, s := range score {
		if s > 0 {
			out = append(out, graph.Score{WorkID: id, Score: s})
		}
	}
	slices.SortFunc(out, func(a, b graph.Score) int {
		if c := cmp.Compare(b.Score, a.Score); c != 0 {
			return c
		}
		return cmp.Compare(a.WorkID, b.WorkID)
	})
	return out
}

// ForYou recommends from my whole history: one walk of the graph from
// every finished book, less half a walk from the books I gave up on, kept
// to what opts allows, with opts' per-author cap. Each result's "why" is
// the finished book nearest it in the content view. opts.Seeds, Avoid,
// Views and Diversity are ignored.
func ForYou(ctx context.Context, st *store.Store, opts Options) ([]Rec, *History, error) {
	if opts.Model == "" {
		opts.Model = embed.DefaultModel
	}
	h, err := LoadHistory(ctx, st, opts.Model)
	if err != nil {
		return nil, nil, err
	}
	if opts.Graph == nil {
		if opts.Graph, err = graph.Load(ctx, st, opts.Model, graph.DefaultWeights); err != nil {
			return nil, nil, err
		}
	}
	walked := WalkHistory(opts.Graph, h.Seeds, h.Avoid, cmp.Or(opts.GraphAlpha, GraphAlpha))
	ids, scores := relative(walked[:min(len(walked), walkDepth)], -1)

	var exclude []int64
	for _, s := range append(slices.Clone(h.Seeds), h.Avoid...) {
		exclude = append(exclude, s.WorkID)
	}
	limit := cmp.Or(opts.Limit, 20)
	// Deep enough that the per-author cap still leaves limit.
	recs, err := walkedNeighbours(ctx, st.DB(), Graph, ids, scores, exclude, opts, 3*limit)
	if err != nil {
		return nil, nil, err
	}
	recs = Fuse([]Ranking{{Weight: 1, Near: recs}}, nil, Options{PerAuthor: opts.PerAuthor, Limit: limit})
	if err := explain(ctx, st, opts.Model, recs, h.Seeds); err != nil {
		return nil, nil, err
	}
	return recs, h, nil
}

// explain sets each recommendation's "why" to the seed nearest it in the
// content view, where both have vectors, and clears it where they don't.
func explain(ctx context.Context, st *store.Store, model string, recs []Rec, seeds []Seed) error {
	ids := make([]int64, len(recs))
	for i, r := range recs {
		ids[i] = r.WorkID
	}
	vectors, err := LoadVectors(ctx, st, model, ids)
	if err != nil {
		return err
	}
	for i := range recs {
		r := &recs[i]
		r.Like, r.Similarity, r.View = "", 0, Content
		for _, s := range seeds {
			if c, ok := cosine(vectors[r.WorkID], s.vector); ok && (r.Like == "" || c > r.Similarity) {
				r.Like, r.Similarity = s.Title, c
			}
		}
	}
	return nil
}

// BuildForYou makes the "for you" list of recordings in languages (nil
// for any), ForYouDepth long and unfiltered otherwise, and stores it with
// the seeds behind it (for_you.sql), for ListForYou to read. It loads the
// graph, which takes a few seconds, so build runs it, and embed and
// neighbours when they change what the graph holds. With nothing to walk
// from yet, the list is empty.
func BuildForYou(ctx context.Context, st *store.Store, languages []string) error {
	recs, h, err := ForYou(ctx, st, Options{Languages: languages, MaxWaitDays: -1, PerAuthor: 3, Limit: ForYouDepth})
	if errors.Is(err, errNoHistory) {
		recs, h, err = nil, &History{}, nil
	}
	if err != nil {
		return fmt.Errorf("for you: %w", err)
	}
	if err := st.RunScript(ctx, forYouSQL, nil); err != nil {
		return fmt.Errorf("for you: %w", err)
	}
	err = st.AppendRows(ctx, "for_you", func(add func(...any) error) error {
		for i, r := range recs {
			if err := add(i+1, r.WorkID, r.Like, r.Similarity); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("for you: %w", err)
	}
	return st.AppendRows(ctx, "for_you_seed", func(add func(...any) error) error {
		for _, list := range []struct {
			seeds  []Seed
			gaveUp bool
		}{{h.Seeds, false}, {h.Avoid, true}} {
			for _, s := range list.seeds {
				if err := add(s.WorkID, s.Title, s.Author, s.Weight, list.gaveUp); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// ListOptions filter the stored "for you" list.
type ListOptions struct {
	AvailableNow bool     // only what can be borrowed right now
	MaxWaitDays  int      // only what's available within this many days; <0 for no limit
	MinMinutes   int      // only recordings at least this long; 0 for no bound
	MaxMinutes   int      // only recordings shorter than this; 0 for no bound
	Languages    []string // only recordings in these languages; nil for any
	Limit        int      // at most this many; 0 means 30
}

// Walked is what the stored list was made from: how many finished books
// it walked from, and how many books I gave up on it walked away from.
type Walked struct {
	Finished, GaveUp int
}

// ListForYou reads the stored "for you" list, in its order, keeping what
// opts allow, and says what it was made from.
func ListForYou(ctx context.Context, st *store.Store, opts ListOptions) ([]Rec, Walked, error) {
	var w Walked
	err := st.DB().QueryRowContext(ctx,
		`SELECT count(*) FILTER (NOT gave_up), count(*) FILTER (gave_up) FROM for_you_seed`).Scan(&w.Finished, &w.GaveUp)
	if err != nil {
		return nil, w, fmt.Errorf("for you (has `recbook build` run?): %w", err)
	}
	languages := opts.Languages
	if languages == nil {
		languages = []string{} // a NULL list would keep nothing
	}
	rows, err := st.DB().QueryContext(ctx, forYouListSQL,
		sql.Named("available_now", opts.AvailableNow),
		sql.Named("max_wait", opts.MaxWaitDays),
		sql.Named("min_minutes", opts.MinMinutes),
		sql.Named("max_minutes", opts.MaxMinutes),
		sql.Named("languages", languages),
		sql.Named("limit", cmp.Or(opts.Limit, 30)))
	if err != nil {
		return nil, w, err
	}
	defer rows.Close()
	var recs []Rec
	for rows.Next() {
		r := Rec{View: Content}
		var author, narrator, url, tier sql.NullString
		err := rows.Scan(&r.WorkID, &r.TitleID, &r.Title, &author, &narrator, &r.DurationMin, &r.Abridged,
			&r.Available, &r.WaitDays, &r.Holds, &url, &tier, &r.Like, &r.Similarity)
		if err != nil {
			return nil, w, err
		}
		r.Author, r.Narrator, r.URL, r.Tier = author.String, narrator.String, url.String, tier.String
		recs = append(recs, r)
	}
	return recs, w, rows.Err()
}
