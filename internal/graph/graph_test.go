package graph

import (
	"context"
	"slices"
	"testing"
)

func TestNearest(t *testing.T) {
	// Four unit vectors: 0 and 1 close, 2 between 1 and 3.
	vectors := []float32{
		1, 0,
		0.8, 0.6,
		0, 1,
		-0.6, 0.8,
	}
	near, err := nearest(context.Background(), vectors, 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	want := [][]int{{1, 2}, {0, 2}, {3, 1}, {2, 1}}
	for i, list := range near {
		var got []int
		for _, n := range list {
			got = append(got, n.index)
		}
		if !slices.Equal(got, want[i]) {
			t.Errorf("nearest to %d: %v, want %v", i, got, want[i])
		}
	}
}

func TestRank(t *testing.T) {
	// Works 1 and 2 share an author; 3 shares a subject with 2; 4 is alone.
	edges := map[int32][]edge{}
	g := &Graph{nodeOf: map[int64]int32{}}
	for _, id := range []int64{1, 2, 3, 4} {
		g.nodeOf[id] = int32(len(g.works))
		g.works = append(g.works, id)
	}
	author, subject := int32(4), int32(5)
	link := func(w int64, node int32, kind string) {
		u := g.nodeOf[w]
		edges[u] = append(edges[u], edge{to: node, kind: kind, weight: 1})
		edges[node] = append(edges[node], edge{to: u, kind: kind, weight: 1})
	}
	link(1, author, Author)
	link(2, author, Author)
	link(2, subject, Subject)
	link(3, subject, Subject)
	g.transitions(6, edges, DefaultWeights)

	scores := g.Rank(map[int64]float64{1: 1}, 0.3)
	var order []int64
	for _, s := range scores {
		order = append(order, s.WorkID)
	}
	// From 1: itself, then 2 (one hop through the author), then 3 (two
	// hops); 4 is never reached.
	if !slices.Equal(order, []int64{1, 2, 3}) {
		t.Errorf("order %v, want [1 2 3]", order)
	}
	var total float64
	for _, s := range scores {
		total += s.Score
	}
	if total < 0.5 || total > 1+1e-9 {
		t.Errorf("work scores sum to %v; the rest sits on author and subject nodes", total)
	}
	if g.Rank(map[int64]float64{99: 1}, 0.3) != nil {
		t.Error("a seed not in the graph should rank nothing")
	}
}
