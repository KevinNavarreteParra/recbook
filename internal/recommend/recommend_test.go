package recommend

import (
	"context"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/KevinNavarreteParra/recbook/internal/demo"
	"github.com/KevinNavarreteParra/recbook/internal/embed"
	"github.com/KevinNavarreteParra/recbook/internal/library"
	"github.com/KevinNavarreteParra/recbook/internal/resolve"
	"github.com/KevinNavarreteParra/recbook/internal/search"
	"github.com/KevinNavarreteParra/recbook/internal/signals"
	"github.com/KevinNavarreteParra/recbook/internal/sources/jsonl"
	"github.com/KevinNavarreteParra/recbook/internal/store"
)

func setup(t *testing.T) (*store.Store, func(name string) int64) {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := demo.Load(ctx, st); err != nil {
		t.Fatal(err)
	}
	// A finished book the library doesn't have, with no description but
	// with the subjects a reading app would give it.
	shelf, err := jsonl.Read(strings.NewReader(`
{"item": {"id": "t-1", "title": "Foundation", "authors": ["Isaac Asimov"], "format": "print", "subjects": ["Fiction", "Science Fiction", "Space Opera"]}}
{"event": {"item_id": "t-1", "kind": "finished", "on": "2023-01-01", "date_quality": "user"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.ReplaceShelf(ctx, "test", "test.jsonl", shelf); err != nil {
		t.Fatal(err)
	}
	titles, avail := demo.Catalog()
	for i := range titles {
		titles[i].Description = demo.Blurbs[titles[i].ID]
		titles[i].Subjects = demo.Subjects[titles[i].ID]
	}
	if err := st.SaveCatalog(ctx, demo.Library, titles, avail, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := resolve.Run(ctx, st, resolve.Options{}); err != nil {
		t.Fatal(err)
	}
	for _, step := range []func(context.Context, *store.Store) error{
		func(ctx context.Context, st *store.Store) error { return signals.Run(ctx, st, signals.Options{}) },
		library.Build, BuildGenre, search.Build,
	} {
		if err := step(ctx, st); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := embed.Run(ctx, st, demo.ThemeEmbedder{}, embed.Options{}); err != nil {
		t.Fatal(err)
	}

	find := func(name string) int64 {
		t.Helper()
		works, err := search.Find(ctx, st, name)
		if err != nil || len(works) == 0 {
			t.Fatalf("Find(%q) = %v, %v", name, works, err)
		}
		return works[0].ID
	}
	return st, find
}

func titles(recs []Rec) []string {
	var out []string
	for _, r := range recs {
		out = append(out, r.Title)
	}
	return out
}

func TestSimilar(t *testing.T) {
	ctx := context.Background()
	st, find := setup(t)
	tinker, redOctober := find("tinker tailor"), find("hunt red october")

	recs, seeds, err := Similar(ctx, st, Options{Seeds: []int64{tinker}, MaxWaitDays: -1})
	if err != nil {
		t.Fatal(err)
	}
	if len(seeds) != 1 || !seeds[0].Embedded || seeds[0].Weight != 1.25 {
		t.Errorf("Tinker Tailor is owned and finished (T2): seed = %+v", seeds[0])
	}
	got := titles(recs)
	if len(got) < 2 || !(got[0] == "Smiley's People" || got[0] == "The Spy Who Came in from the Cold") ||
		!(got[1] == "Smiley's People" || got[1] == "The Spy Who Came in from the Cold") {
		t.Errorf("the two other le Carré spy novels should come first: %q", got)
	}
	if recs[0].Like != "Tinker Tailor Soldier Spy" || recs[0].Similarity < 0.9 {
		t.Errorf("why = %q (%.2f)", recs[0].Like, recs[0].Similarity)
	}
	for _, r := range recs {
		if r.Title == "Tinker Tailor Soldier Spy" || r.Title == "The Hunt for Red October" {
			t.Errorf("seeds and finished books shouldn't be recommended: %q", r.Title)
		}
	}

	// One per author: the second le Carré gives way.
	recs, _, _ = Similar(ctx, st, Options{Seeds: []int64{tinker}, MaxWaitDays: -1, PerAuthor: 1})
	if got := titles(recs); len(got) < 2 || recs[1].Author == recs[0].Author {
		t.Errorf("PerAuthor 1: %q", got)
	}

	// Mixed seeds get some of each.
	recs, _, _ = Similar(ctx, st, Options{Seeds: []int64{tinker, redOctober}, MaxWaitDays: -1, Limit: 3})
	if got := titles(recs); !strings.Contains(strings.Join(got, "|"), "Red Storm Rising") {
		t.Errorf("a navy seed should bring in Red Storm Rising: %q", got)
	}
}

func TestAvoidPushesDown(t *testing.T) {
	ctx := context.Background()
	st, find := setup(t)
	tinker, redOctober := find("tinker tailor"), find("hunt red october")

	rank := func(recs []Rec, title string) int {
		for i, r := range recs {
			if r.Title == title {
				return i
			}
		}
		return len(recs)
	}
	plain, _, _ := Similar(ctx, st, Options{Seeds: []int64{tinker}, MaxWaitDays: -1})
	avoided, _, _ := Similar(ctx, st, Options{Seeds: []int64{tinker}, Avoid: []int64{redOctober}, MaxWaitDays: -1})
	if rank(avoided, "Red Storm Rising") <= rank(plain, "Red Storm Rising") {
		t.Errorf("avoiding a navy thriller should push Red Storm Rising down: %q vs %q", titles(plain), titles(avoided))
	}
}

func TestGenreView(t *testing.T) {
	ctx := context.Background()
	st, find := setup(t)
	tinker, foundation := find("tinker tailor"), find("foundation asimov")
	genre, _ := ParseView("genre")

	// Shelved alike: the two other le Carrés are the only other Espionage.
	recs, _, err := Similar(ctx, st, Options{Seeds: []int64{tinker}, Views: genre, MaxWaitDays: -1, Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if got := titles(recs); len(got) != 2 || recs[0].Author != "John le Carré" || recs[1].Author != "John le Carré" {
		t.Errorf("genre view of Tinker Tailor = %q, want the other le Carrés", got)
	}
	if recs[0].View != Genre || recs[0].SimilarityText() != "genre 1.00" {
		t.Errorf("why = %s (%s)", recs[0].Like, recs[0].SimilarityText())
	}

	// Foundation has only the reading app's subjects, which are BISAC
	// headings in pieces; put back together they shelve it with Dune
	// Messiah (Space Opera), then Project Hail Mary (Science Fiction).
	content, _ := ParseView("content")
	_, seeds, err := Similar(ctx, st, Options{Seeds: []int64{foundation}, Views: content, MaxWaitDays: -1})
	if err != nil {
		t.Fatal(err)
	}
	if !seeds[0].Skipped {
		t.Errorf("with no description, Foundation can't be compared by content: %+v", seeds[0])
	}
	recs, seeds, err = Similar(ctx, st, Options{Seeds: []int64{foundation}, MaxWaitDays: -1, Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if seeds[0].Skipped || !seeds[0].Shelved {
		t.Errorf("Foundation should be compared by genre: %+v", seeds[0])
	}
	if got := titles(recs); len(got) != 2 || got[0] != "Dune Messiah" || got[1] != "Project Hail Mary" {
		t.Errorf("blend of Foundation = %q, want Dune Messiah, Project Hail Mary", got)
	}
}

func TestWeigh(t *testing.T) {
	both := Seed{WorkID: 1, Weight: 1.5, Embedded: true, Shelved: true}
	genreOnly := Seed{WorkID: 2, Weight: 1.5, Shelved: true}
	rankings := []Ranking{{Seed: both, View: Content}, {Seed: both, View: Genre}, {Seed: genreOnly, View: Genre}}

	opts := Options{Views: Blend{Content: 1, Genre: 0.5}}
	got := Weigh(rankings, opts)
	want := []float64{1.5 * 2 / 3, 1.5 * 1 / 3, 1.5} // a seed's views share its weight
	if len(got) != len(want) {
		t.Fatalf("Weigh kept %d rankings, want %d", len(got), len(want))
	}
	for i, r := range got {
		if math.Abs(r.Weight-want[i]) > 1e-9 {
			t.Errorf("%s ranking of seed %d weighs %v, want %v", r.View, r.Seed.WorkID, r.Weight, want[i])
		}
	}

	opts = Options{Views: Blend{Content: 1}, Unweighted: true}
	if got := Weigh(rankings, opts); len(got) != 1 || got[0].View != Content || got[0].Weight != 1 {
		t.Errorf("content alone, unweighted: %+v", got)
	}
}
