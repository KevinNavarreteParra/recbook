package search

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/KevinNavarreteParra/recbook/internal/contract"
	"github.com/KevinNavarreteParra/recbook/internal/demo"
	"github.com/KevinNavarreteParra/recbook/internal/library"
	"github.com/KevinNavarreteParra/recbook/internal/resolve"
	"github.com/KevinNavarreteParra/recbook/internal/signals"
	"github.com/KevinNavarreteParra/recbook/internal/store"
)

func demoIndex(t *testing.T) *store.Store {
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
	// A recording in Spanish, of a book the catalog also has in English.
	titles, avail := demo.Catalog()
	titles = append(titles, contract.CatalogTitle{
		ID: "9040", Title: "El mesías de Dune", Format: contract.Audiobook, Language: "es", DurationMin: 600,
		Creators: []contract.Creator{{Name: "Frank Herbert", Role: contract.Author}, {Name: "Ramón Langa", Role: contract.Narrator}},
	})
	avail = append(avail, contract.Availability{TitleID: "9040", Available: true, CopiesOwned: 1, CopiesAvailable: 1})
	if err := st.SaveCatalog(ctx, demo.Library, titles, avail, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := resolve.Run(ctx, st, resolve.Options{}); err != nil {
		t.Fatal(err)
	}
	if err := signals.Run(ctx, st, signals.Options{}); err != nil {
		t.Fatal(err)
	}
	if err := library.Build(ctx, st, nil); err != nil {
		t.Fatal(err)
	}
	if err := Build(ctx, st); err != nil {
		t.Fatal(err)
	}
	return st
}

func titles(rs []Result) []string {
	var out []string
	for _, r := range rs {
		out = append(out, r.Title)
	}
	return out
}

func TestSearch(t *testing.T) {
	ctx := context.Background()
	st := demoIndex(t)
	search := func(q Query) []string {
		t.Helper()
		rs, err := Search(ctx, st, q)
		if err != nil {
			t.Fatal(err)
		}
		return titles(rs)
	}

	// Word forms, accents and case don't matter, and any word may match.
	got := search(Query{Text: "carre spies", MaxWaitDays: -1})
	if !slices.Contains(got, "Smiley's People") || !slices.Contains(got, "The Spy Who Came in from the Cold") {
		t.Errorf(`"carre spies" = %q`, got)
	}
	// Tinker Tailor is finished, so it's hidden unless asked for.
	if slices.Contains(got, "Tinker Tailor Soldier Spy") {
		t.Error("finished books should be hidden by default")
	}
	if got := search(Query{Text: "tinker", MaxWaitDays: -1, IncludeRead: true}); !slices.Equal(got, []string{"Tinker Tailor Soldier Spy"}) {
		t.Errorf("with IncludeRead: %q", got)
	}

	// Matching every word beats matching one word strongly.
	if got := search(Query{Text: "dune scott brick", MaxWaitDays: -1}); len(got) == 0 || got[0] != "Dune Messiah" {
		t.Errorf(`"dune scott brick" should put the one match for every word first: %q`, got)
	}

	// Narrators are searchable, and filters apply.
	if got := search(Query{Text: "scott brick", MaxWaitDays: -1}); !slices.Contains(got, "Dune Messiah") {
		t.Errorf("narrator search = %q", got)
	}
	if got := search(Query{Text: "clancy", MaxWaitDays: -1, AvailableNow: true}); len(got) != 0 {
		t.Errorf("no Clancy is available now, got %q", got)
	}
	if got := search(Query{Text: "zzzz", MaxWaitDays: -1}); len(got) != 0 {
		t.Errorf("nonsense should find nothing, got %q", got)
	}

	// Recordings in other languages show unless languages are given.
	if got := search(Query{Text: "herbert", MaxWaitDays: -1}); !slices.Contains(got, "El mesías de Dune") {
		t.Errorf("with any language, herbert = %q", got)
	}
	got = search(Query{Text: "herbert", MaxWaitDays: -1, Languages: []string{"en"}})
	if !slices.Equal(got, []string{"Dune Messiah"}) {
		t.Errorf("in English, herbert = %q", got)
	}
}

func TestFind(t *testing.T) {
	ctx := context.Background()
	st := demoIndex(t)

	works, err := Find(ctx, st, "tinker tailor", nil)
	if err != nil || len(works) == 0 || works[0].Title != "Tinker Tailor Soldier Spy" || !works[0].OnShelf {
		t.Fatalf("Find(tinker tailor) = %+v, %v", works, err)
	}
	// Every word must match: "dune messiah" is not Dune.
	works, err = Find(ctx, st, "dune messiah", nil)
	if err != nil || len(works) != 1 || works[0].Title != "Dune Messiah" {
		t.Errorf("Find(dune messiah) = %+v, %v", works, err)
	}
	// A shelf book comes before a catalog-only one that also matches.
	works, err = Find(ctx, st, "dune", nil)
	if err != nil || len(works) < 2 || works[0].Title != "Dune" {
		t.Errorf("Find(dune) = %+v, %v", works, err)
	}
	// Then a book in one of my languages, before one the library has only
	// in another.
	for _, tt := range []struct {
		languages []string
		want      string
	}{
		{[]string{"en"}, "Dune Messiah"},
		{[]string{"es"}, "El mesías de Dune"},
	} {
		works, err = Find(ctx, st, "dune herbert", tt.languages)
		if err != nil || len(works) != 3 || works[0].Title != "Dune" || works[1].Title != tt.want {
			t.Errorf("Find(dune herbert, %v) = %+v, %v; want Dune, then %s", tt.languages, works, err, tt.want)
		}
	}
}
