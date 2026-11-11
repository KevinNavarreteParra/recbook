// Package eval measures how well recommend ranks books I already liked,
// by leave-one-out over the books I've finished (spec §11).
//
// Each finished book the library lends is held out in turn, as if I hadn't
// read it, and the question is how high recommend ranks it. Two protocols
// ask that question, and in neither does a similarity view choose the
// seeds (the spec's "use its nearest finished books as seeds" would favour
// whichever view picked them):
//
//   - history: every other finished book is a seed at once, weighted by
//     tier, as in a "for you" list.
//   - one seed: every other finished book is tried as a seed on its own,
//     and the held-out book gets the best rank any of them gives it. This
//     is query by example, with the best query I could have made.
//
// The views (content, genre, and the blend of both) are ranked by the real
// recommend code. Two baselines and chance rank the same books from the
// same pool (baselines.sql), so the numbers have something to beat.
package eval

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"math"
	"slices"

	"github.com/KevinNavarreteParra/recbook/internal/embed"
	"github.com/KevinNavarreteParra/recbook/internal/recommend"
	"github.com/KevinNavarreteParra/recbook/internal/store"
)

var (
	//go:embed finished.sql
	finishedSQL string
	//go:embed baselines.sql
	baselinesSQL string
)

// The protocols and methods, in the order they're reported. baselines.sql
// uses the same names.
var (
	Protocols = []string{"history", "one seed"}
	Methods   = []string{"content", "genre", "blend", "same author", "same shelf", "random"}
)

// Cutoffs are the list lengths hit rates are reported at. The CLI shows 20
// recommendations and the web app 30.
var Cutoffs = []int{10, 20, 50}

// Options tune an evaluation run.
type Options struct {
	Model      string          // embedding model; default embed.DefaultModel
	Blend      recommend.Blend // the "blend" method's weights; nil means recommend.DefaultBlend
	Unweighted bool            // count every seed equally, ignoring tiers
	PerAuthor  int             // as in recommend.Options; 0 for no cap
	Diversity  float64         // as in recommend.Options; 0 for none
	// MaxRank is how deep the history protocol looks; a book ranked below
	// it counts as missed. 0 means 1000. The one-seed protocol looks as
	// deep as a seed's ranking goes, recommend.PerSeed.
	MaxRank int
}

// Report is the result of a run.
type Report struct {
	Seeds    int // finished books with a description or subjects, all used as seeds
	Finished int // finished books, with a vector or not
	Pool     int // unread works that could be recommended
	Results  []Result
	Targets  []Target
}

// Result is how one method did under one protocol, over every target.
type Result struct {
	Protocol string
	Method   string
	Hits     []float64 // share of targets ranked within each of Cutoffs
	MRR      float64   // mean reciprocal rank
	// MedianRank is the middle target's rank, or +Inf when more than half
	// were missed.
	MedianRank float64
}

// Target is one held-out book and where it ranked, in the order of
// Report.Results. A baseline's rank is its expected rank over ties; +Inf
// means missed.
type Target struct {
	WorkID int64
	Title  string
	Author string
	Tier   string
	Ranks  []float64
}

// outcome is where one method put one target: below higher candidates, and
// in a tie with ties-1 others, each order of which is equally likely.
type outcome struct {
	higher, ties int
	missed       bool
}

var missed = outcome{missed: true}

// hitWithin is the chance the target lands in the top k.
func (o outcome) hitWithin(k int) float64 {
	if o.missed {
		return 0
	}
	return min(max(float64(k-o.higher)/float64(o.ties), 0), 1)
}

// reciprocal is the expected 1/rank.
func (o outcome) reciprocal() float64 {
	if o.missed {
		return 0
	}
	var sum float64
	for r := o.higher + 1; r <= o.higher+o.ties; r++ {
		sum += 1 / float64(r)
	}
	return sum / float64(o.ties)
}

// rank is the expected rank.
func (o outcome) rank() float64 {
	if o.missed {
		return math.Inf(1)
	}
	return float64(o.higher) + float64(o.ties+1)/2
}

// better reports whether o is a better rank than p.
func (o outcome) better(p outcome) bool {
	return !o.missed && (p.missed || o.rank() < p.rank())
}

// outcomes holds each protocol and method's outcomes, one per target in
// the targets' order, keyed by protocol then method.
type outcomes map[string]map[string][]outcome

func (oc outcomes) set(protocol, method string, results []outcome) {
	if oc[protocol] == nil {
		oc[protocol] = map[string][]outcome{}
	}
	oc[protocol][method] = results
}

// Run holds out each finished book the library lends and ranks it by every
// method under both protocols. Ranking the catalog for every seed is the
// slow part: a few minutes for a few hundred seeds.
func Run(ctx context.Context, st *store.Store, opts Options) (*Report, error) {
	if opts.Model == "" {
		opts.Model = embed.DefaultModel
	}
	if opts.MaxRank == 0 {
		opts.MaxRank = 1000
	}
	if opts.Blend == nil {
		opts.Blend = recommend.DefaultBlend
	}

	finished, targets, err := loadFinished(ctx, st, opts.Model)
	if err != nil {
		return nil, err
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("no finished book has a borrowable, embedded recording to hold out (have build and embed run?)")
	}

	results := outcomes{}
	seeds, err := rankViews(ctx, st, finished, targets, opts, results)
	if err != nil {
		return nil, err
	}
	pool, err := rankBaselines(ctx, st, targets, opts, results)
	if err != nil {
		return nil, err
	}

	rep := &Report{Seeds: seeds, Finished: len(finished), Pool: pool, Targets: targets}
	for _, p := range Protocols {
		for _, m := range Methods {
			got := results[p][m]
			if len(got) != len(targets) {
				return nil, fmt.Errorf("%s / %s ranked %d of %d targets", p, m, len(got), len(targets))
			}
			rep.Results = append(rep.Results, summarize(p, m, got))
			for i := range rep.Targets {
				rep.Targets[i].Ranks = append(rep.Targets[i].Ranks, got[i].rank())
			}
		}
	}
	return rep, nil
}

// loadFinished returns the ids of every finished book, and those to hold out.
func loadFinished(ctx context.Context, st *store.Store, model string) ([]int64, []Target, error) {
	rows, err := st.DB().QueryContext(ctx, finishedSQL, sql.Named("model", model))
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var ids []int64
	var targets []Target
	for rows.Next() {
		var t Target
		var isTarget bool
		if err := rows.Scan(&t.WorkID, &t.Title, &t.Author, &t.Tier, &isTarget); err != nil {
			return nil, nil, err
		}
		ids = append(ids, t.WorkID)
		if isTarget {
			targets = append(targets, t)
		}
	}
	return ids, targets, rows.Err()
}

// rankViews ranks each target by each view method under both protocols,
// and returns how many seeds had anything to compare.
//
// It asks recommend for each seed's ranking in every view once, deep
// enough that after dropping the other finished books (which a real query
// would never return) recommend.PerSeed remain. Then, for each method and
// target, it weighs and fuses those rankings exactly as recommend.Similar
// would if that one book were unread.
func rankViews(ctx context.Context, st *store.Store, finished []int64, targets []Target, opts Options, results outcomes) (int, error) {
	seeds, err := recommend.LoadSeeds(ctx, st, opts.Model, finished)
	if err != nil {
		return 0, err
	}
	every := recommend.Blend{}
	for _, v := range recommend.Views {
		every[v] = 1
	}
	rankOpts := recommend.Options{Views: every, Model: opts.Model, IncludeRead: true, MaxWaitDays: -1}
	rankings, err := recommend.Rank(ctx, st, seeds, nil, rankOpts, recommend.PerSeed+len(finished))
	if err != nil {
		return 0, err
	}

	f := fusion{targets: targets, opts: opts, index: map[int64]int{}, isFinished: map[int64]bool{}}
	for _, id := range finished {
		f.isFinished[id] = true
	}
	for i, t := range targets {
		f.index[t.WorkID] = i
	}
	if opts.Diversity > 0 {
		if f.vectors, err = loadVectors(ctx, st, opts.Model, rankings); err != nil {
			return 0, err
		}
	}
	content, _ := recommend.ParseView("content")
	genre, _ := recommend.ParseView("genre")
	for _, m := range []struct {
		method string
		blend  recommend.Blend
	}{{"content", content}, {"genre", genre}, {"blend", opts.Blend}} {
		weighed := recommend.Weigh(rankings, recommend.Options{Views: m.blend, Unweighted: opts.Unweighted})
		results.set("history", m.method, f.history(weighed))
		results.set("one seed", m.method, f.oneSeed(weighed))
	}

	seeded := map[int64]bool{}
	for _, r := range rankings {
		seeded[r.Seed.WorkID] = true
	}
	return len(seeded), nil
}

// fusion fuses rankings for each held-out target in turn.
type fusion struct {
	targets    []Target
	opts       Options
	index      map[int64]int  // target work id → position in targets
	isFinished map[int64]bool // every finished book, target or not
	vectors    map[int64][]float32
}

// loadVectors loads the content vector of every work in the rankings, so
// that fused lists can be diversified.
func loadVectors(ctx context.Context, st *store.Store, model string, rankings []recommend.Ranking) (map[int64][]float32, error) {
	seen := map[int64]bool{}
	var ids []int64
	for _, r := range rankings {
		for _, n := range r.Near {
			if !seen[n.WorkID] {
				seen[n.WorkID] = true
				ids = append(ids, n.WorkID)
			}
		}
	}
	return recommend.LoadVectors(ctx, st, model, ids)
}

// rank is where the target lands in the fused rankings, diversified as
// recommend.Similar would. Diversify only reorders the top
// recommend.DiverseDepth, so a target below that keeps its fused rank,
// which saves diversifying most lists.
func (f fusion) rank(target int64, pos []recommend.Ranking, limit int) outcome {
	recs := recommend.Fuse(pos, nil, recommend.Options{PerAuthor: f.opts.PerAuthor, Limit: limit})
	if f.opts.Diversity > 0 {
		if o := rankOf(target, recs); o.missed || o.higher >= recommend.DiverseDepth {
			return o
		}
		recs = recommend.Diversify(recs, f.vectors, f.opts.Diversity)
	}
	return rankOf(target, recs)
}

// history fuses every other seed's rankings at once for each target.
func (f fusion) history(rankings []recommend.Ranking) []outcome {
	out := make([]outcome, len(f.targets))
	for i, t := range f.targets {
		var pos []recommend.Ranking
		for _, r := range rankings {
			if r.Seed.WorkID != t.WorkID { // held out: not a seed
				pos = append(pos, holdOut(r, t.WorkID, f.isFinished))
			}
		}
		out[i] = f.rank(t.WorkID, pos, f.opts.MaxRank)
	}
	return out
}

// oneSeed fuses each seed's rankings alone (a seed has one per view), for
// every target they reach at all, and keeps each target's best rank.
func (f fusion) oneSeed(rankings []recommend.Ranking) []outcome {
	bySeed := map[int64][]recommend.Ranking{}
	for _, r := range rankings {
		bySeed[r.Seed.WorkID] = append(bySeed[r.Seed.WorkID], r)
	}
	out := make([]outcome, len(f.targets))
	for i := range out {
		out[i] = missed
	}
	for seed, group := range bySeed {
		reached := map[int64]bool{}
		for _, r := range group {
			for _, n := range r.Near {
				if _, ok := f.index[n.WorkID]; ok && n.WorkID != seed {
					reached[n.WorkID] = true
				}
			}
		}
		for t := range reached {
			pos := make([]recommend.Ranking, len(group))
			for j, r := range group {
				pos[j] = holdOut(r, t, f.isFinished)
			}
			o := f.rank(t, pos, recommend.PerSeed)
			if o.better(out[f.index[t]]) {
				out[f.index[t]] = o
			}
		}
	}
	return out
}

// holdOut returns r as a real query would have returned it with held
// unread: without the other finished books, and cut to recommend.PerSeed.
func holdOut(r recommend.Ranking, held int64, isFinished map[int64]bool) recommend.Ranking {
	near := make([]recommend.Rec, 0, recommend.PerSeed)
	for _, n := range r.Near {
		if isFinished[n.WorkID] && n.WorkID != held {
			continue
		}
		if near = append(near, n); len(near) == recommend.PerSeed {
			break
		}
	}
	r.Near = near
	return r
}

// rankOf finds work id in a list of recommendations.
func rankOf(id int64, recs []recommend.Rec) outcome {
	i := slices.IndexFunc(recs, func(r recommend.Rec) bool { return r.WorkID == id })
	if i < 0 {
		return missed
	}
	return outcome{higher: i, ties: 1}
}

// rankBaselines ranks the targets by each baseline in baselines.sql, under
// both protocols, and returns the size of the candidate pool.
func rankBaselines(ctx context.Context, st *store.Store, targets []Target, opts Options, results outcomes) (int, error) {
	rows, err := st.DB().QueryContext(ctx, baselinesSQL,
		sql.Named("model", opts.Model), sql.Named("unweighted", opts.Unweighted))
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	index := map[int64]int{}
	for i, t := range targets {
		index[t.WorkID] = i
	}
	pool := 0
	for rows.Next() {
		var id int64
		var protocol, method string
		var higher int
		var ties sql.NullInt64 // NULL: the baseline can't rank this target
		if err := rows.Scan(&id, &protocol, &method, &higher, &ties); err != nil {
			return 0, err
		}
		i, ok := index[id]
		if !ok {
			return 0, fmt.Errorf("baseline %s ranked work %d, which isn't a target", method, id)
		}
		if results[protocol][method] == nil {
			results.set(protocol, method, make([]outcome, len(targets)))
		}
		o := missed
		if ties.Valid {
			o = outcome{higher: higher, ties: int(ties.Int64)}
		}
		results[protocol][method][i] = o
		if method == "random" {
			pool = o.ties - 1
		}
	}
	return pool, rows.Err()
}

func summarize(protocol, method string, outcomes []outcome) Result {
	res := Result{Protocol: protocol, Method: method, Hits: make([]float64, len(Cutoffs))}
	ranks := make([]float64, len(outcomes))
	for i, o := range outcomes {
		for j, k := range Cutoffs {
			res.Hits[j] += o.hitWithin(k)
		}
		res.MRR += o.reciprocal()
		ranks[i] = o.rank()
	}
	n := float64(len(outcomes))
	for j := range res.Hits {
		res.Hits[j] /= n
	}
	res.MRR /= n
	res.MedianRank = median(ranks)
	return res
}

// median of xs, which it sorts. +Inf counts as larger than everything.
func median(xs []float64) float64 {
	slices.Sort(xs)
	mid := len(xs) / 2
	if len(xs)%2 == 1 {
		return xs[mid]
	}
	return (xs[mid-1] + xs[mid]) / 2
}
