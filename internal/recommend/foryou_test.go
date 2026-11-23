package recommend

import (
	"slices"
	"testing"
)

func TestGroupSeeds(t *testing.T) {
	mean := []float32{0, 0, 0}
	seeds := []Seed{
		{WorkID: 1, Title: "spy", Weight: 1, vector: []float32{1, 0, 0}},
		{WorkID: 2, Title: "spy too", Weight: 1.25, vector: []float32{0.9, 0.1, 0}},
		{WorkID: 3, Title: "physics", Weight: 1, vector: []float32{0, 1, 0}},
		{WorkID: 4, Title: "no description", Weight: 1},
	}
	groups := GroupSeeds(seeds, mean, 0.5)
	var got [][]int64
	for _, g := range groups {
		var ids []int64
		for _, s := range g.Seeds {
			ids = append(ids, s.WorkID)
		}
		got = append(got, ids)
	}
	// The heavier spy leads the spies' group, the heaviest; the others
	// follow in order of weight, then id.
	want := [][]int64{{2, 1}, {3}, {4}}
	if len(got) != len(want) {
		t.Fatalf("groups %v, want %v", got, want)
	}
	for i := range want {
		if !slices.Equal(got[i], want[i]) {
			t.Errorf("groups %v, want %v", got, want)
		}
	}
	if groups[0].Weight != 2.25 || groups[0].Leader().Title != "spy too" {
		t.Errorf("first group weighs %v and is led by %q", groups[0].Weight, groups[0].Leader().Title)
	}
}

func TestInterleave(t *testing.T) {
	list := func(author string, ids ...int64) []Rec {
		var out []Rec
		for _, id := range ids {
			out = append(out, Rec{WorkID: id, Author: author})
		}
		return out
	}
	groups := []Group{{Weight: 3}, {Weight: 1}}
	lists := [][]Rec{list("A", 1, 2, 3, 4), append(list("B", 1), list("C", 5, 6)...)}

	// Sainte-Laguë: the heavy group takes two places, then the light one
	// gets its first, skipping work 1, which is already listed.
	got := Interleave(groups, lists, Options{Limit: 4})
	var ids []int64
	for _, r := range got {
		ids = append(ids, r.WorkID)
	}
	if !slices.Equal(ids, []int64{1, 2, 5, 3}) {
		t.Errorf("order %v, want [1 2 5 3]", ids)
	}
	if got[2].Group != 1 || got[3].Group != 0 {
		t.Errorf("each result should record the group whose turn it filled: %+v", got)
	}

	// With at most two books by one author, the heavy group runs out and
	// the light one fills the rest.
	got = Interleave(groups, lists, Options{Limit: 5, PerAuthor: 2})
	ids = nil
	for _, r := range got {
		ids = append(ids, r.WorkID)
	}
	if !slices.Equal(ids, []int64{1, 2, 5, 6}) {
		t.Errorf("capped order %v, want [1 2 5 6]", ids)
	}
}
