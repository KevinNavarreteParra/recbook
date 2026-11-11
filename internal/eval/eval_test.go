package eval

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
	"github.com/KevinNavarreteParra/recbook/internal/signals"
	"github.com/KevinNavarreteParra/recbook/internal/sources/jsonl"
	"github.com/KevinNavarreteParra/recbook/internal/store"
)

func TestOutcome(t *testing.T) {
	// Fourth of ten tied books, after three that score higher: ranks 4..13.
	o := outcome{higher: 3, ties: 10}
	if got := o.rank(); got != 8.5 {
		t.Errorf("rank = %v, want 8.5", got)
	}
	for k, want := range map[int]float64{3: 0, 4: 0.1, 8: 0.5, 13: 1, 50: 1} {
		if got := o.hitWithin(k); math.Abs(got-want) > 1e-9 {
			t.Errorf("hitWithin(%d) = %v, want %v", k, got, want)
		}
	}
	if got, want := (outcome{higher: 0, ties: 2}).reciprocal(), (1+0.5)/2; got != want {
		t.Errorf("reciprocal = %v, want %v", got, want)
	}
	if missed.hitWithin(10) != 0 || missed.reciprocal() != 0 || !math.IsInf(missed.rank(), 1) {
		t.Errorf("a missed book should score nothing")
	}
	if !(outcome{higher: 4, ties: 1}).better(missed) || missed.better(outcome{higher: 4, ties: 1}) {
		t.Errorf("any rank beats a miss")
	}
}

func TestMedian(t *testing.T) {
	if got := median([]float64{3, 1, 2}); got != 2 {
		t.Errorf("odd: %v", got)
	}
	if got := median([]float64{4, 1, 3, 2}); got != 2.5 {
		t.Errorf("even: %v", got)
	}
	if got := median([]float64{1, math.Inf(1), math.Inf(1)}); !math.IsInf(got, 1) {
		t.Errorf("mostly missed: %v", got)
	}
}

// setup loads the demo shelves and catalog, with descriptions, plus one
// more finished book, and runs what `recbook build` and `recbook embed`
// would.
func setup(t *testing.T) *store.Store {
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
	// The demo shelves finish one le Carré spy novel; finish a second, so
	// that holding out either leaves the other as a seed.
	shelf, err := jsonl.Read(strings.NewReader(`
{"item": {"id": "t-1", "title": "The Spy Who Came in from the Cold", "authors": ["John le Carré"], "format": "audiobook"}}
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
	}
	if err := st.SaveCatalog(ctx, demo.Library, titles, avail, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := resolve.Run(ctx, st, resolve.Options{}); err != nil {
		t.Fatal(err)
	}
	if err := signals.Run(ctx, st, signals.Options{}); err != nil {
		t.Fatal(err)
	}
	if err := library.Build(ctx, st); err != nil {
		t.Fatal(err)
	}
	if _, err := embed.Run(ctx, st, demo.ThemeEmbedder{}, embed.Options{}); err != nil {
		t.Fatal(err)
	}
	return st
}

// result finds one protocol and method's result.
func result(t *testing.T, rep *Report, protocol, method string) Result {
	t.Helper()
	for _, r := range rep.Results {
		if r.Protocol == protocol && r.Method == method {
			return r
		}
	}
	t.Fatalf("no result for %s / %s", protocol, method)
	return Result{}
}

func TestRun(t *testing.T) {
	st := setup(t)
	rep, err := Run(context.Background(), st, Options{PerAuthor: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Results) != len(Protocols)*len(Methods) {
		t.Fatalf("%d results, want one per protocol and method", len(rep.Results))
	}

	// The theme embedder puts the three le Carré spy novels side by side,
	// and Smiley's People (unread) nearest of all to each finished one. So
	// held out, each finished one ranks second, from the other as the one
	// seed. (In history, The Hunt for Red October is a seed too, and with
	// so few books its ranking shuffles the order.)
	ranks := map[string][]float64{}
	for _, tg := range rep.Targets {
		ranks[tg.Title] = tg.Ranks
		if len(tg.Ranks) != len(rep.Results) {
			t.Errorf("%s has %d ranks, want %d", tg.Title, len(tg.Ranks), len(rep.Results))
		}
	}
	for _, title := range []string{"Tinker Tailor Soldier Spy", "The Spy Who Came in from the Cold"} {
		got, ok := ranks[title]
		if !ok {
			t.Fatalf("%s should be held out: targets = %v", title, ranks)
		}
		for i, r := range rep.Results {
			if r.Protocol == "one seed" && r.Method == "content" && got[i] != 2 {
				t.Errorf("%s / content ranks %s %v, want 2", r.Protocol, title, got[i])
			}
		}
	}

	// Chance: every target ties with the whole pool.
	random := result(t, rep, "history", "random")
	if want := float64(rep.Pool+2) / 2; random.MedianRank != want {
		t.Errorf("random median rank = %v, want %v", random.MedianRank, want)
	}
	// Same author, one seed: each le Carré's best seed is the other, which
	// lists the one unread le Carré and the target, in either order.
	for i, r := range rep.Results {
		if r.Protocol == "one seed" && r.Method == "same author" {
			if got := ranks["Tinker Tailor Soldier Spy"][i]; got != 1.5 {
				t.Errorf("one seed / same author ranks Tinker Tailor %v, want 1.5 (a tie for first)", got)
			}
		}
	}
}
