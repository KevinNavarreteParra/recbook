package recommend

import (
	"context"
	"database/sql"
	_ "embed"
	"math"
	"slices"

	"github.com/KevinNavarreteParra/recbook/internal/store"
)

//go:embed vectors.sql
var vectorsSQL string

// DiverseDepth is how far down a fused list Diversify looks: it reorders
// the top DiverseDepth and leaves the rest as they were. Greedy MMR's
// first picks don't depend on how many more it goes on to make, so the
// top 20 of its order are the 20 it would pick if asked for only 20; a
// window five times the usual list gives it room to move sideways without
// dredging up books the seeds barely reach.
const DiverseDepth = 100

// DefaultDiversity is what `similar` and the web app use. The
// leave-one-out evaluation can't see variety, only whether a held-out
// book still ranks high. By the blend's one-seed mean reciprocal rank that
// slips a little with every step of diversity (0.218 at 0, 0.214 at 0.3,
// 0.206 at 0.6) while its hit rates wobble by a few books either way, so
// the default is none, and the knob is there for when I want it.
//
// Diversity complements the per-author cap rather than replacing it. The
// cap is a rule I can predict ("at most three by one author"); MMR is a
// soft push away from whatever the list already holds, a series or a
// subgenre as much as an author. The evaluation does like no cap and
// diversity 0.3 best of all (hit@10 37% against 34%), but mostly because
// a held-out book is often by an author I've read, which is just what the
// cap pushes down.
const DefaultDiversity = 0.0

// Diversify reorders the top of recs, a list Fuse returned, by maximal
// marginal relevance (spec §8). Each next pick is the work that maximizes
//
//	(1 − diversity) · relevance − diversity · (its highest similarity to a work already picked)
//
// Diversity 0 leaves recs as they are; 1 ignores the seeds and only
// spreads out. Relevance is a work's fused score and similarity the cosine
// of two works' content vectors, each scaled to 0..1 over the window, so
// that the two terms trade evenly. Unscaled they wouldn't: the cosines
// between one query's neighbours bunch between about 0.7 and 0.85, so
// even a high diversity would barely move anything.
//
// Content vectors stand in for "these two are alike" whatever view ranked
// them: they are dense, nearly every candidate has one, and two books in
// one series read alike even when the library shelves them apart.
func Diversify(recs []Rec, vectors map[int64][]float32, diversity float64) []Rec {
	if diversity <= 0 || len(recs) < 3 {
		return recs
	}
	window := recs[:min(len(recs), DiverseDepth)]
	relevance := make([]float64, len(window))
	for i, r := range window {
		relevance[i] = r.Score
	}
	scale(relevance)
	similarity := pairwise(window, vectors)

	picked := make([]bool, len(window))
	closest := make([]float64, len(window)) // to the works picked so far
	out := make([]Rec, 0, len(recs))
	for len(out) < len(window) {
		best, bestScore := -1, math.Inf(-1)
		for i := range window {
			s := (1-diversity)*relevance[i] - diversity*closest[i]
			if !picked[i] && s > bestScore {
				best, bestScore = i, s
			}
		}
		picked[best] = true
		out = append(out, window[best])
		for i := range window {
			closest[i] = max(closest[i], similarity[best][i])
		}
	}
	return append(out, recs[len(window):]...)
}

// pairwise returns the cosine of every two works' content vectors, scaled
// to 0..1 over all the pairs. A work with no vector is unlike every other
// (0), as is each work with itself, which no pick ever compares.
func pairwise(recs []Rec, vectors map[int64][]float32) [][]float64 {
	n := len(recs)
	out := make([][]float64, n)
	for i := range out {
		out[i] = make([]float64, n)
	}
	lo, hi := math.Inf(1), math.Inf(-1)
	for i := range n {
		for j := range i {
			c, ok := cosine(vectors[recs[i].WorkID], vectors[recs[j].WorkID])
			if !ok {
				c = math.NaN() // scaled to 0 below
			} else {
				lo, hi = min(lo, c), max(hi, c)
			}
			out[i][j] = c
		}
	}
	for i := range n {
		for j := range i {
			if math.IsNaN(out[i][j]) {
				out[i][j] = 0
			} else {
				out[i][j] = scaled(out[i][j], lo, hi)
			}
			out[j][i] = out[i][j]
		}
	}
	return out
}

// cosine of two unit-length vectors, and false if either is missing.
func cosine(a, b []float32) (float64, bool) {
	if len(a) == 0 || len(a) != len(b) {
		return 0, false
	}
	var dot float32
	for i := range a {
		dot += a[i] * b[i]
	}
	return float64(dot), true
}

// scale scales xs in place to 0..1, lowest to highest.
func scale(xs []float64) {
	lo, hi := slices.Min(xs), slices.Max(xs)
	for i, x := range xs {
		xs[i] = scaled(x, lo, hi)
	}
}

// scaled is where x falls between lo and hi, as 0..1; 1 if they're equal.
func scaled(x, lo, hi float64) float64 {
	if hi == lo {
		return 1
	}
	return (x - lo) / (hi - lo)
}

// LoadVectors returns the content vectors in model of those of ids that
// have one, for Diversify.
func LoadVectors(ctx context.Context, st *store.Store, model string, ids []int64) (map[int64][]float32, error) {
	rows, err := st.DB().QueryContext(ctx, vectorsSQL, sql.Named("model", model), sql.Named("ids", ids))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64][]float32{}
	for rows.Next() {
		var id int64
		var vector any
		if err := rows.Scan(&id, &vector); err != nil {
			return nil, err
		}
		if v := toFloats(vector); v != nil {
			out[id] = v
		}
	}
	return out, rows.Err()
}

// toFloats converts a FLOAT[] as the DuckDB driver returns it, or nil if
// it isn't one (a NULL).
func toFloats(list any) []float32 {
	xs, ok := list.([]any)
	if !ok {
		return nil
	}
	out := make([]float32, len(xs))
	for i, x := range xs {
		out[i] = x.(float32)
	}
	return out
}
