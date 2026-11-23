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
	"github.com/KevinNavarreteParra/recbook/internal/graph"
	"github.com/KevinNavarreteParra/recbook/internal/store"
	"github.com/KevinNavarreteParra/recbook/internal/textkey"
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
	//go:embed graph_neighbours.sql
	graphSQL string
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
	Graph               // a walk from the seed reaches it often (internal/graph)
)

// Views are all the views, in order.
var Views = []View{Content, Genre, Graph}

func (v View) String() string {
	switch v {
	case Content:
		return "content"
	case Genre:
		return "genre"
	case Graph:
		return "graph"
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
	case "graph":
		return Blend{Graph: 1}, nil
	}
	return nil, fmt.Errorf("unknown view %q: want content, genre, graph or blend", name)
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

	// Languages keeps only recordings in these languages (ISO 639-1
	// codes, "en"); nil keeps every language.
	Languages []string

	// Graph is the graph view's graph. If the blend uses the graph and
	// this is nil, Rank loads it, which takes a few seconds; the web app
	// and the evaluation load it once and pass it in.
	Graph *graph.Graph
	// GraphAlpha is the graph walk's restart probability; 0 means GraphAlpha.
	GraphAlpha float64
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

	// TextSource is where the description behind its content vector came
	// from ("catalog", or a shelf source such as "readinglist"), and
	// TextLanguage the description's language, if the source records one.
	// A seed described in another language than mine finds books in that
	// language, so this says why.
	TextSource, TextLanguage string

	vector []float32
}

// Has reports whether s can be compared in view v.
func (s Seed) Has(v View) bool {
	switch v {
	case Content:
		return s.Embedded
	case Genre:
		return s.Shelved
	case Graph:
		// Edges reach it through its subjects or its nearest texts (and its
		// author, which every book has, but that alone says little).
		return s.Shelved || s.Embedded
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
			Scan(&s.Title, &s.Author, &s.Weight, &vector, &s.TextSource, &s.TextLanguage, &s.Shelved)
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

	if opts.Graph == nil && slices.ContainsFunc(rankings, func(r Ranking) bool { return r.View == Graph }) {
		var err error
		if opts.Graph, err = graph.Load(ctx, st, opts.Model, graph.DefaultWeights); err != nil {
			return nil, err
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
		author := authorKey(c.Author)
		if opts.PerAuthor > 0 && byAuthor[author] >= opts.PerAuthor {
			continue
		}
		byAuthor[author]++
		c.Score = c.score
		out = append(out, c.Rec)
	}
	return out
}

// authorKey is who counts as one author for the per-author cap: the person
// a credit names (textkey.Person), so that "John le Carré" and "John Le
// Carre" share one cap, or else the credit itself, lowercased.
func authorKey(credit string) string {
	if key := textkey.Person(credit); key != "" {
		return key
	}
	return strings.ToLower(credit)
}

// neighbours runs one seed's ranking in view v: the content view compares
// the seed's vector with every embedded work's, the genre view the seed's
// subjects with every shelved work's.
func neighbours(ctx context.Context, db *sql.DB, v View, s Seed, exclude []int64, opts Options, depth int) ([]Rec, error) {
	query, args := contentSQL, []any{sql.Named("seed", s.vector), sql.Named("model", opts.Model)}
	switch v {
	case Genre:
		query, args = genreSQL, []any{sql.Named("seed", s.WorkID)}
	case Graph:
		walked := opts.Graph.Rank(map[int64]float64{s.WorkID: 1}, cmp.Or(opts.GraphAlpha, GraphAlpha))
		ids, scores := relative(walked[:min(len(walked), walkDepth)], s.WorkID)
		return walkedNeighbours(ctx, db, v, ids, scores, exclude, opts, depth)
	}
	return query1(ctx, db, v, query, args, exclude, opts, depth)
}

// walkedNeighbours keeps the borrowable works among walked ones (ids,
// with their scores, best first) that opts allows, at most depth of them,
// as recommendations in view v (graph_neighbours.sql).
func walkedNeighbours(ctx context.Context, db *sql.DB, v View, ids []int64, scores []float64, exclude []int64, opts Options, depth int) ([]Rec, error) {
	return query1(ctx, db, v, graphSQL, []any{sql.Named("ids", ids), sql.Named("scores", scores)}, exclude, opts, depth)
}

// query1 runs one view's ranking query with opts' filters, and reads its
// rows as recommendations in view v.
func query1(ctx context.Context, db *sql.DB, v View, query string, args []any, exclude []int64, opts Options, depth int) ([]Rec, error) {
	languages := opts.Languages
	if exclude == nil {
		exclude = []int64{} // a NULL list would exclude everything
	}
	if languages == nil {
		languages = []string{} // ...and a NULL list of languages would keep nothing
	}
	rows, err := db.QueryContext(ctx, query, append(args,
		sql.Named("exclude", exclude),
		sql.Named("include_read", opts.IncludeRead),
		sql.Named("available_now", opts.AvailableNow),
		sql.Named("max_wait", opts.MaxWaitDays),
		sql.Named("languages", languages),
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

// GraphAlpha is the graph walk's restart probability: how close to the seed
// it stays. In the evaluation's one-seed protocol 0.15, 0.3 and 0.5 did
// about equally well (hit@10 44%, 44% and 43%); 0.3 converges faster.
const GraphAlpha = 0.3

// walkDepth is how many of a walk's best works are handed to the query
// that filters them: enough that "available now" leaves plenty.
const walkDepth = 20000

// relative splits a walk's scores into ids and scores, leaving out the
// seed (where the walk starts, so it's always first; -1 for none), with
// the scores scaled so the best is 1: a PageRank is a tiny number, and
// only its order means anything.
func relative(scores []graph.Score, seed int64) ([]int64, []float64) {
	ids := make([]int64, 0, len(scores))
	out := make([]float64, 0, len(scores))
	for _, s := range scores {
		if s.WorkID == seed {
			continue
		}
		ids = append(ids, s.WorkID)
		out = append(out, s.Score)
	}
	if len(out) > 0 {
		top := out[0]
		for i := range out {
			out[i] /= top
		}
	}
	return ids, out
}
