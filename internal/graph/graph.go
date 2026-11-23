package graph

import (
	"cmp"
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"math"
	"slices"

	"github.com/KevinNavarreteParra/recbook/internal/store"
)

var (
	//go:embed edges.sql
	edgesSQL string
	//go:embed similar_edges.sql
	similarEdgesSQL string
)

// The kinds of edge out of a work.
const (
	Author   = "author"
	Series   = "series"
	Subject  = "subject"
	Narrator = "narrator"
	Similar  = "similar" // to a work nearest it in the content view
)

// Weights are how much of a walk leaving a work goes along each kind of
// edge, when the work has edges of that kind. They're the "what kind of
// similar" knob of spec §7: more on Series keeps to series mates, more on
// Similar follows how books read.
type Weights map[string]float64

// DefaultWeights follow the spec's ordering: series mates are the
// strongest signal, a shared narrator a weak one.
var DefaultWeights = Weights{Author: 1, Series: 2, Subject: 1, Narrator: 0.2, Similar: 1}

// Graph is the graph in memory, as a random walk's transition
// probabilities. Works are nodes 0..len(works)-1; authors, series,
// subjects and narrators follow.
type Graph struct {
	works  []int64         // node → work id, for works
	nodeOf map[int64]int32 // work id → node

	// The edges out of node u are to[start[u]:start[u+1]], taken with
	// probabilities prob[start[u]:start[u+1]], which sum to 1.
	start []int32
	to    []int32
	prob  []float32
}

// Works is how many works the graph has.
func (g *Graph) Works() int { return len(g.works) }

// Has reports whether the work is in the graph: whether it has any edge.
func (g *Graph) Has(work int64) bool {
	_, ok := g.nodeOf[work]
	return ok
}

// edge is an edge as loaded: to a node, of a kind, with a weight.
type edge struct {
	to     int32
	kind   string
	weight float64
}

// Load reads the graph from the derived tables, with work-to-work edges in
// model's content view if text_neighbour has been built, and turns it into
// transition probabilities (see transitions). It takes a few seconds.
func Load(ctx context.Context, st *store.Store, model string, weights Weights) (*Graph, error) {
	g := &Graph{nodeOf: map[int64]int32{}}
	out := map[int32][]edge{} // every edge, both ways, by its start
	keys := map[string]int32{}

	work := func(id int64) int32 {
		n, ok := g.nodeOf[id]
		if !ok {
			n = int32(len(g.works))
			g.nodeOf[id] = n
			g.works = append(g.works, id)
		}
		return n
	}
	// Edges are kept as loaded until every work has its node, since the
	// other nodes are numbered after the works.
	type toKey struct {
		work int64
		key  string // kind:key, the other node
		edge edge
	}
	type pair struct {
		a, b       int64
		similarity float64
	}
	var structural []toKey
	var similar []pair

	rows, err := st.DB().QueryContext(ctx, edgesSQL)
	if err != nil {
		return nil, fmt.Errorf("graph edges: %w", err)
	}
	for rows.Next() {
		var kind, key string
		var id int64
		var weight float64
		if err := rows.Scan(&kind, &id, &key, &weight); err != nil {
			rows.Close()
			return nil, err
		}
		work(id)
		structural = append(structural, toKey{id, kind + ":" + key, edge{kind: kind, weight: weight}})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if ok, err := hasTable(ctx, st, "text_neighbour"); err != nil {
		return nil, err
	} else if ok {
		rows, err := st.DB().QueryContext(ctx, similarEdgesSQL, sql.Named("model", model))
		if err != nil {
			return nil, fmt.Errorf("graph similar edges: %w", err)
		}
		for rows.Next() {
			var a, b int64
			var sim float64
			if err := rows.Scan(&a, &b, &sim); err != nil {
				rows.Close()
				return nil, err
			}
			work(a)
			work(b)
			similar = append(similar, pair{a, b, sim})
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}

	for _, t := range structural {
		w := g.nodeOf[t.work]
		n, ok := keys[t.key]
		if !ok {
			n = int32(len(g.works) + len(keys))
			keys[t.key] = n
		}
		out[w] = append(out[w], edge{to: n, kind: t.edge.kind, weight: t.edge.weight})
		out[n] = append(out[n], edge{to: w, kind: t.edge.kind, weight: t.edge.weight})
	}
	for _, p := range similar {
		a, b := g.nodeOf[p.a], g.nodeOf[p.b]
		out[a] = append(out[a], edge{to: b, kind: Similar, weight: p.similarity})
		out[b] = append(out[b], edge{to: a, kind: Similar, weight: p.similarity})
	}

	g.transitions(len(g.works)+len(keys), out, weights)
	return g, nil
}

// transitions turns the edges into each node's probabilities of stepping
// along each of its edges. From a work, a step first picks a kind of edge
// in proportion to weights (among the kinds the work has), then an edge of
// that kind in proportion to the edges' own weights, so a work with ten
// subjects isn't drawn to subjects ten times as hard as one with one. From
// any other node, a step picks one of its works in proportion to the
// edges' weights.
func (g *Graph) transitions(nodes int, out map[int32][]edge, weights Weights) {
	g.start = make([]int32, nodes+1)
	for u := range nodes {
		edges := out[int32(u)]
		g.start[u+1] = g.start[u] + int32(len(edges))
	}
	g.to = make([]int32, g.start[nodes])
	g.prob = make([]float32, g.start[nodes])

	for u := range nodes {
		edges := out[int32(u)]
		if len(edges) == 0 {
			continue
		}
		// kindTotal: the sum of each kind's edge weights at u.
		kindTotal := map[string]float64{}
		for _, e := range edges {
			kindTotal[e.kind] += e.weight
		}
		var share func(e edge) float64
		if u < len(g.works) {
			var all float64
			for kind := range kindTotal {
				all += weights[kind]
			}
			share = func(e edge) float64 {
				if all == 0 || kindTotal[e.kind] == 0 {
					return 0
				}
				return weights[e.kind] / all * e.weight / kindTotal[e.kind]
			}
		} else {
			var all float64
			for _, e := range edges {
				all += e.weight
			}
			share = func(e edge) float64 { return e.weight / all }
		}
		at := g.start[u]
		for i, e := range edges {
			g.to[int(at)+i] = e.to
			g.prob[int(at)+i] = float32(share(e))
		}
	}
}

func hasTable(ctx context.Context, st *store.Store, name string) (bool, error) {
	var n int
	err := st.DB().QueryRowContext(ctx,
		`SELECT count(*) FROM duckdb_tables() WHERE table_name = ? AND NOT temporary`, name).Scan(&n)
	return n > 0, err
}

// Score is a work's personalized PageRank.
type Score struct {
	WorkID int64
	Score  float64
}

// Rank returns every work's personalized PageRank from the seeds (work id
// → weight; works not in the graph are ignored), best first, leaving out
// works that score nothing. A walk restarts at a seed, chosen by weight,
// with probability alpha at each step, so a higher alpha keeps to the
// seeds' near neighbours and a lower one wanders further. Mass that
// reaches a node with no edges goes back to the seeds.
func (g *Graph) Rank(seeds map[int64]float64, alpha float64) []Score {
	n := len(g.start) - 1
	restart := make([]float64, n)
	var total float64
	for id, w := range seeds {
		if u, ok := g.nodeOf[id]; ok && w > 0 {
			restart[u] += w
			total += w
		}
	}
	if total == 0 {
		return nil
	}
	for u := range restart {
		restart[u] /= total
	}

	// Power iteration: x ← alpha·restart + (1-alpha)·(x stepped once).
	x := slices.Clone(restart)
	next := make([]float64, n)
	for range 50 {
		clear(next)
		var stranded float64 // mass at nodes with no edges
		for u := range n {
			if x[u] == 0 {
				continue
			}
			lo, hi := g.start[u], g.start[u+1]
			if lo == hi {
				stranded += x[u]
				continue
			}
			for i := lo; i < hi; i++ {
				next[g.to[i]] += x[u] * float64(g.prob[i])
			}
		}
		var change float64
		for u := range n {
			v := alpha*restart[u] + (1-alpha)*(next[u]+stranded*restart[u])
			change += math.Abs(v - x[u])
			x[u] = v
		}
		if change < 1e-9 {
			break
		}
	}

	var out []Score
	for u, id := range g.works {
		if x[u] > 0 {
			out = append(out, Score{id, x[u]})
		}
	}
	slices.SortFunc(out, func(a, b Score) int {
		if c := cmp.Compare(b.Score, a.Score); c != 0 {
			return c
		}
		return cmp.Compare(a.WorkID, b.WorkID)
	})
	return out
}
