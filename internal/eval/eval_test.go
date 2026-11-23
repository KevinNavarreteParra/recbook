package eval

import (
	"context"
	"math"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/KevinNavarreteParra/recbook/internal/authors"
	"github.com/KevinNavarreteParra/recbook/internal/demo"
	"github.com/KevinNavarreteParra/recbook/internal/embed"
	"github.com/KevinNavarreteParra/recbook/internal/graph"
	"github.com/KevinNavarreteParra/recbook/internal/library"
	"github.com/KevinNavarreteParra/recbook/internal/recommend"
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
		titles[i].Subjects = demo.Subjects[titles[i].ID]
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
	if err := library.Build(ctx, st, nil); err != nil {
		t.Fatal(err)
	}
	if err := recommend.BuildGenre(ctx, st); err != nil {
		t.Fatal(err)
	}
	if _, err := embed.Run(ctx, st, demo.ThemeEmbedder{}, embed.Options{}); err != nil {
		t.Fatal(err)
	}
	if err := authors.Build(ctx, st); err != nil { // and the mean vector, content_mean
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
	if len(rep.Results) != len(Protocols)*len(Methods)-1 {
		t.Fatalf("%d results, want one per protocol and method, less one seed's for you", len(rep.Results))
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

func TestRunDiversified(t *testing.T) {
	rep, err := Run(context.Background(), setup(t), Options{PerAuthor: 3, Diversity: 0.5})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Results) != len(Protocols)*len(Methods)-1 {
		t.Fatalf("%d results, want one per protocol and method, less one seed's for you", len(rep.Results))
	}
}

func TestAUC(t *testing.T) {
	for _, c := range []struct {
		name     string
		scores   []float64
		positive []bool
		want     float64
	}{
		{"perfect", []float64{1, 2, 3, 4}, []bool{false, false, true, true}, 1},
		{"backwards", []float64{1, 2, 3, 4}, []bool{true, true, false, false}, 0},
		{"one swap", []float64{1, 2, 3, 4}, []bool{false, true, false, true}, 0.75},
		{"all tied", []float64{5, 5, 5, 5}, []bool{false, true, false, true}, 0.5},
	} {
		if got := auc(c.scores, c.positive); math.Abs(got-c.want) > 1e-9 {
			t.Errorf("%s: auc = %v, want %v", c.name, got, c.want)
		}
	}
	if got := midranks([]float64{3, 1, 3, 2}); !slices.Equal(got, []float64{3.5, 1, 3.5, 2}) {
		t.Errorf("midranks = %v", got)
	}
}

func TestDiscriminate(t *testing.T) {
	st := setup(t)
	g, err := graph.Load(context.Background(), st, embed.DefaultModel, graph.DefaultWeights)
	if err != nil {
		t.Fatal(err)
	}
	d, err := Discriminate(context.Background(), st, Options{Model: embed.DefaultModel}, g)
	if err != nil {
		t.Fatal(err)
	}
	// The demo loans: The Hunt for Red October and The Spy Who Came in
	// from the Cold, finished; The Da Vinci Code, returned early; and The
	// Silent Patient, whose loan ran out.
	if d.Finished != 2 || d.GaveUp != 2 || d.Lapsed != 1 {
		t.Errorf("finished %d, gave up %d (%d lapsed); want 2, 2 (1)", d.Finished, d.GaveUp, d.Lapsed)
	}
	if len(d.Scores) != len(BorrowMethods) {
		t.Fatalf("%d scores, want one per method", len(d.Scores))
	}
	auc := map[string]BorrowScore{}
	for _, s := range d.Scores {
		auc[s.Method] = s
	}
	// Same author: only The Spy Who Came in from the Cold has another
	// finished book by its author (Tinker Tailor), so it beats both books
	// I gave up on, and The Hunt for Red October ties with both: (1 + 1 +
	// 0.5 + 0.5) / 4.
	if got := auc["same author"]; got.AUC != 0.75 || got.Scored != 4 {
		t.Errorf("same author: AUC %v over %d borrows, want 0.75 over 4", got.AUC, got.Scored)
	}
	// Only the finished two have descriptions, so content can't compare a
	// finished borrow with a given-up one, and the blend falls back on
	// genre for the rest.
	if got := auc["content"]; !math.IsNaN(got.AUC) || got.Scored != 2 {
		t.Errorf("content: AUC %v over %d borrows, want NaN over 2", got.AUC, got.Scored)
	}
	for _, m := range []string{"genre", "graph", "blend"} {
		if got := auc[m]; got.Scored < 3 || got.AUC < 0 || got.AUC > 1 {
			t.Errorf("%s: AUC %v over %d borrows, want one over 3", m, got.AUC, got.Scored)
		}
	}
}
