package recommend

import (
	"context"
	"slices"
	"testing"

	"github.com/KevinNavarreteParra/recbook/internal/authors"
)

func TestForYou(t *testing.T) {
	ctx := context.Background()
	st, _ := setup(t)
	if err := authors.Build(ctx, st); err != nil { // the graph's author edges
		t.Fatal(err)
	}
	recs, h, err := ForYou(ctx, st, Options{MaxWaitDays: -1, PerAuthor: 3, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	// Of the books the demo shelves finish, three have a description or
	// subjects to walk from; The Da Vinci Code went back early.
	if len(h.Seeds) != 3 || len(h.Avoid) != 1 || h.Avoid[0].Title != "The Da Vinci Code" {
		t.Errorf("seeds %+v, avoid %+v", h.Seeds, h.Avoid)
	}
	got := titles(recs)
	for _, read := range []string{"Tinker Tailor Soldier Spy", "The Hunt for Red October"} {
		if slices.Contains(got, read) {
			t.Errorf("for you shouldn't recommend %s, which I've finished: %q", read, got)
		}
	}
	// Smiley's People shares Tinker Tailor's author, series and themes.
	i := slices.Index(got, "Smiley's People")
	if i < 0 || i > 2 {
		t.Fatalf("Smiley's People should be near the top: %q", got)
	}
	if recs[i].Like != "Tinker Tailor Soldier Spy" || recs[i].View != Content {
		t.Errorf("Smiley's People's why: %q (%v)", recs[i].Like, recs[i].View)
	}
}
