package recommend

import (
	"slices"
	"testing"
)

func TestDiversify(t *testing.T) {
	// A sequel to the top pick ranks second, a different book third.
	recs := []Rec{
		{WorkID: 1, Title: "Smiley", Score: 1.0},
		{WorkID: 2, Title: "Smiley again", Score: 0.9},
		{WorkID: 3, Title: "Submarine", Score: 0.8},
		{WorkID: 4, Title: "Another submarine", Score: 0.0},
	}
	vectors := map[int64][]float32{1: {1, 0}, 2: {1, 0}, 3: {0, 1}, 4: {0, 1}}
	order := func(recs []Rec) []string {
		var out []string
		for _, r := range recs {
			out = append(out, r.Title)
		}
		return out
	}

	if got := order(Diversify(recs, vectors, 0)); !slices.Equal(got, order(recs)) {
		t.Errorf("diversity 0 should keep the fused order: %q", got)
	}
	// At 0.5 the sequel scores 0.5·0.9 − 0.5·1 < 0, the submarine 0.5·0.8.
	want := []string{"Smiley", "Submarine", "Smiley again", "Another submarine"}
	if got := order(Diversify(recs, vectors, 0.5)); !slices.Equal(got, want) {
		t.Errorf("diversity 0.5 = %q, want %q", got, want)
	}
	if recs[1].Title != "Smiley again" {
		t.Errorf("Diversify reordered its input in place: %q", order(recs))
	}
}
