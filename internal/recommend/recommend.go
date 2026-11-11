// Package recommend finds borrowable audiobooks like a set of seed books
// (spec §8).
//
// It works in two steps. Rank compares each seed with every borrowable
// work on its own, in each view asked for (content, genre or both), giving
// one ranking per seed and view. Fuse then merges those rankings by
// reciprocal-rank fusion, so a mixed set of seeds (Clancy and Dostoevsky)
// yields some of each rather than the muddle a single averaged query
// would, and two views blend without their similarities having to be on
// one scale. Seeds count in proportion to their tier's weight. Works close
// to an "avoid" seed are pushed down, and a cap per author keeps one
// author from taking over the list. Diversify can then trade some
// closeness for variety (diversify.go). Similar runs every step; the
// leave-one-out evaluation (internal/eval) runs them separately, so it can
// rank each seed once and fuse many times.
package recommend

import (
	"cmp"
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"slices"
	"strings"

	"golang.org/x/sync/errgroup"

	"github.com/KevinNavarreteParra/recbook/internal/embed"
	"github.com/KevinNavarreteParra/recbook/internal/store"
)

var (
	//go:embed seed.sql
	seedSQL string
	//go:embed content_neighbours.sql
	contentSQL string
	//go:embed genre_neighbours.sql
	genreSQL string
	//go:embed genre.sql
	genreBuildSQL string
)

// PerSeed is how far down each seed's ranking fusion looks.
const PerSeed = 200

// Fusion settings. rrfK is the usual constant for reciprocal-rank fusion:
// it keeps the top few ranks of any one list from dominating. avoidWeight
// is how hard an avoid seed pushes its neighbours down, relative to a
// positive seed.
const (
	rrfK        = 60
	avoidWeight = 0.5
)

// parallel is how many rankings Rank runs at once. Each is one DuckDB
// query, which uses a few cores of its own.
const parallel = 4

// A View is one way of saying two books are alike (spec §6).
type View int

// The views, in the order the "why" prefers them: a content similarity
// says more about a book than a genre one.
const (
	Content View = iota // their descriptions read alike (embedding cosine)
	Genre               // the library shelves them alike (subject cosine)
)

// Views are all the views, in order.
var Views = []View{Content, Genre}

func (v View) String() string {
	switch v {
	case Content:
		return "content"
	case Genre:
		return "genre"
	}
	return fmt.Sprintf("View(%d)", int(v))
}

// A Blend is how much each view counts in a query. A view it leaves out,
// or gives no weight, isn't compared at all.
type Blend map[View]float64

// DefaultBlend is what `similar` and the web app use. Of genre weights
// 0.25, 0.5, 0.75, 1 and 2, with content at 1, 0.75 ranked held-out books
// best in the leave-one-out evaluation's one-seed protocol (hit@10 34%
// against content's 29%), and each blend beat either view alone.
var DefaultBlend = Blend{Content: 1, Genre: 0.75}

// ParseView returns the blend a --view name stands for: one view alone,
// or "blend" for DefaultBlend.
func ParseView(name string) (Blend, error) {
	switch name {
	case "blend":
		return DefaultBlend, nil
	case "content":
		return Blend{Content: 1}, nil
	case "genre":
		return Blend{Genre: 1}, nil
	}
	return nil, fmt.Errorf("unknown view %q: want content, genre or blend", name)
}

// Options are a recommendation request.
type Options struct {
	Seeds []int64 // works I want more like (see search.Find)
	Avoid []int64 // works I want less like

	Views        Blend   // the views to compare in; nil means DefaultBlend
	Model        string  // embedding model; default embed.DefaultModel
	AvailableNow bool    // only what can be borrowed right now
	MaxWaitDays  int     // only what's available within this many days; <0 for no limit
	IncludeRead  bool    // also recommend books I've finished
	Unweighted   bool    // count every seed equally, ignoring tiers
	PerAuthor    int     // at most this many results by one author; 0 for no cap
	Diversity    float64 // 0..1: how much closeness to trade for variety (see Diversify)
	Limit        int     // at most this many results; 0 means 20
}

func (o Options) blend() Blend {
	if o.Views == nil {
		return DefaultBlend
	}
	return o.Views
}

// Seed is a seed as it was used.
type Seed struct {
	WorkID   int64
	Title    string
	Author   string
	Weight   float64
	Embedded bool // it has a description to compare in the content view
	Shelved  bool // it has subjects to compare in the genre view
	Skipped  bool // it has neither in the views asked for, so it was left out
	vector   []float32
}

// Has reports whether s can be compared in view v.
func (s Seed) Has(v View) bool {
	switch v {
	case Content:
		return s.Embedded
	case Genre:
		return s.Shelved
	}
	return false
}

// Rec is one recommendation.
type Rec struct {
	WorkID      int64
	TitleID     string // the recording's catalog id
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
	View       View    // ...in this view: content if any seed's content ranking found it
}

// SimilarityText is the similarity to the closest seed, for showing:
// "0.78", or "genre 0.97" when it isn't a content similarity, since the
// two run on different scales.
func (r Rec) SimilarityText() string {
	if r.View == Content {
		return fmt.Sprintf("%.2f", r.Similarity)
	}
	return fmt.Sprintf("%s %.2f", r.View, r.Similarity)
}

// Ranking is one seed's nearest borrowable works in one view, best first,
// and how much the ranking counts in fusion.
type Ranking struct {
	Seed   Seed
	View   View
	Weight float64
	Near   []Rec
}

// BuildGenre rebuilds the genre view's vectors (genre.sql). It needs
// resolve to have run.
func BuildGenre(ctx context.Context, st *store.Store) error {
	if err := st.RunScript(ctx, genreBuildSQL, nil); err != nil {
		return fmt.Errorf("genre view: %w", err)
	}
	return nil
}

// Similar returns recommendations for the seeds, best first, along with
// the seeds as they were used.
func Similar(ctx context.Context, st *store.Store, opts Options) ([]Rec, []Seed, error) {
	if opts.Model == "" {
		opts.Model = embed.DefaultModel
	}

	seeds, err := LoadSeeds(ctx, st, opts.Model, opts.Seeds)
	if err != nil {
		return nil, nil, err
	}
	avoid, err := LoadSeeds(ctx, st, opts.Model, opts.Avoid)
	if err != nil {
		return nil, nil, err
	}
	used := append(seeds, avoid...)
	for i := range used {
		used[i].Skipped = len(opts.blend().shares(used[i])) == 0
	}
	exclude := append(slices.Clone(opts.Seeds), opts.Avoid...)

	pos, err := Rank(ctx, st, seeds, exclude, opts, PerSeed)
	if err != nil {
		return nil, nil, err
	}
	// Every avoid seed pushes equally hard, whatever its tier.
	avoidOpts := opts
	avoidOpts.Unweighted = true
	neg, err := Rank(ctx, st, avoid, exclude, avoidOpts, PerSeed)
	if err != nil {
		return nil, nil, err
	}
	if opts.Diversity <= 0 {
		return Fuse(pos, neg, opts), used, nil
	}

	// Fuse a window deep enough for Diversify to choose from, then cut it.
	limit := cmp.Or(opts.Limit, 20)
	fuseOpts := opts
	fuseOpts.Limit = max(limit, DiverseDepth)
	recs := Fuse(pos, neg, fuseOpts)
	ids := make([]int64, 0, len(recs))
	for _, r := range recs[:min(len(recs), DiverseDepth)] {
		ids = append(ids, r.WorkID)
	}
	vectors, err := LoadVectors(ctx, st, opts.Model, ids)
	if err != nil {
		return nil, nil, err
	}
	recs = Diversify(recs, vectors, opts.Diversity)
	return recs[:min(len(recs), limit)], used, nil
}

// LoadSeeds looks up each work as a seed: its name, its tier's weight, its
// vector in model, if it has one, and whether it has a genre vector.
func LoadSeeds(ctx context.Context, st *store.Store, model string, ids []int64) ([]Seed, error) {
	var seeds []Seed
	for _, id := range ids {
		s := Seed{WorkID: id}
		var vector any
		err := st.DB().QueryRowContext(ctx, seedSQL, sql.Named("model", model), sql.Named("work_id", id)).
			Scan(&s.Title, &s.Author, &s.Weight, &vector, &s.Shelved)
		if err != nil {
			return nil, fmt.Errorf("seed %d: %w (has `recbook build` run since the last embed?)", id, err)
		}
		s.vector = toFloats(vector)
		s.Embedded = s.vector != nil
		seeds = append(seeds, s)
	}
	return seeds, nil
}

// Rank returns each seed's nearest borrowable works in each of opts' views
// that the seed has, at most depth of them, leaving out the works in
// exclude and whatever opts filters out. A seed with none of the views
// gets no ranking.
func Rank(ctx context.Context, st *store.Store, seeds []Seed, exclude []int64, opts Options, depth int) ([]Ranking, error) {
	if opts.Model == "" {
		opts.Model = embed.DefaultModel
	}
	var rankings []Ranking
	for _, s := range seeds {
		for _, v := range Views {
			if w, ok := opts.weight(s, v); ok {
				rankings = append(rankings, Ranking{Seed: s, View: v, Weight: w})
			}
		}
	}

	// Each goroutine fills its own ranking, so they keep the seeds' order
	// without any locking.
	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(parallel)
	for i := range rankings {
		r := &rankings[i]
		g.Go(func() error {
			near, err := neighbours(ctx, st.DB(), r.View, r.Seed, exclude, opts, depth)
			if err != nil {
				return fmt.Errorf("%s ranking for %q: %w", r.View, r.Seed.Title, err)
			}
			r.Near = near
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	return rankings, nil
}

// Weigh returns the rankings in opts' views, weighted for its blend as
// Rank would have weighted them. It lets the evaluation rank every view
// once and then fuse under several blends.
func Weigh(rankings []Ranking, opts Options) []Ranking {
	var out []Ranking
	for _, r := range rankings {
		if w, ok := opts.weight(r.Seed, r.View); ok {
			r.Weight = w
			out = append(out, r)
		}
	}
	return out
}

// weight is how much seed s's ranking in view v counts in fusion: the
// seed's tier weight (1 if unweighted) times v's share of the blend, and
// false if s isn't compared in v at all.
func (o Options) weight(s Seed, v View) (float64, bool) {
	share, ok := o.blend().shares(s)[v]
	if !ok {
		return 0, false
	}
	if o.Unweighted {
		return share, true
	}
	return s.Weight * share, true
}

// shares splits a seed's pull among the views in b that it has, in
// proportion to their weights, so the shares sum to 1. Renormalizing over
// the views the seed has is the spec's "don't penalize a missing view"
// (§8): a seed with no description pulls as hard through genre alone as
// another of its tier does through both.
//
// The spec asks the same for candidates, but it doesn't arise here: every
// borrowable work has subjects, and all but a handful have descriptions.
// Its other point, putting the views on one scale before blending, is
// what fusing by rank already does.
func (b Blend) shares(s Seed) map[View]float64 {
	var total float64
	for v, w := range b {
		if w > 0 && s.Has(v) {
			total += w
		}
	}
	out := map[View]float64{}
	for v, w := range b {
		if w > 0 && s.Has(v) {
			out[v] = w / total
		}
	}
	return out
}

// Fuse merges the seeds' rankings into one list, best first: each work
// scores the sum over the seeds' rankings of weight / (rrfK + rank), less
// the same for the avoid seeds' rankings at avoidWeight. It then applies
// the per-author cap and the limit.
func Fuse(pos, avoid []Ranking, opts Options) []Rec {
	if opts.Limit == 0 {
		opts.Limit = 20
	}

	type candidate struct {
		Rec
		score float64
	}
	cands := map[int64]*candidate{}

	for _, r := range pos {
		for rank, n := range r.Near {
			c, ok := cands[n.WorkID]
			if !ok {
				c = &candidate{Rec: n}
				c.Like = r.Seed.Title
				cands[n.WorkID] = c
			}
			c.score += r.Weight / float64(rrfK+rank+1)
			// The why: the most similar seed, in the first view that found it.
			if n.View < c.View || (n.View == c.View && n.Similarity > c.Similarity) {
				c.Like, c.Similarity, c.View = r.Seed.Title, n.Similarity, n.View
			}
		}
	}
	for _, r := range avoid {
		for rank, n := range r.Near {
			if c, ok := cands[n.WorkID]; ok {
				c.score -= avoidWeight * r.Weight / float64(rrfK+rank+1)
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
	return out
}

// neighbours runs one seed's ranking in view v: the content view compares
// the seed's vector with every embedded work's, the genre view the seed's
// subjects with every shelved work's.
func neighbours(ctx context.Context, db *sql.DB, v View, s Seed, exclude []int64, opts Options, depth int) ([]Rec, error) {
	if exclude == nil {
		exclude = []int64{} // a NULL list would exclude everything
	}
	query, args := contentSQL, []any{sql.Named("seed", s.vector), sql.Named("model", opts.Model)}
	if v == Genre {
		query, args = genreSQL, []any{sql.Named("seed", s.WorkID)}
	}
	rows, err := db.QueryContext(ctx, query, append(args,
		sql.Named("exclude", exclude),
		sql.Named("include_read", opts.IncludeRead),
		sql.Named("available_now", opts.AvailableNow),
		sql.Named("max_wait", opts.MaxWaitDays),
		sql.Named("k", depth))...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Rec
	for rows.Next() {
		r := Rec{View: v}
		var author, narrator, url, tier sql.NullString
		err := rows.Scan(&r.WorkID, &r.TitleID, &r.Title, &author, &narrator, &r.DurationMin, &r.Abridged,
			&r.Available, &r.WaitDays, &r.Holds, &url, &tier, &r.Similarity)
		if err != nil {
			return nil, err
		}
		r.Author, r.Narrator, r.URL, r.Tier = author.String, narrator.String, url.String, tier.String
		out = append(out, r)
	}
	return out, rows.Err()
}
