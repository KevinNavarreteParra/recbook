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
	"github.com/KevinNavarreteParra/recbook/internal/store"
)

// themeEmbedder makes vectors from theme words, so that "similar" is
// predictable: one dimension per theme, plus a constant.
type themeEmbedder struct{}

var themes = [][]string{
	{"spy", "smiley", "circus", "berlin", "agent"},
	{"desert", "emperor", "prophec", "galaxy"},
	{"submarine", "navy", "war"},
	{"brothers", "faith", "russia"},
}

func (themeEmbedder) Embed(_ context.Context, _ string, inputs []string) ([][]float32, error) {
	var out [][]float32
	for _, in := range inputs {
		in = strings.ToLower(in)
		v := []float32{0.1}
		for _, words := range themes {
			var n float32
			for _, w := range words {
				n += float32(strings.Count(in, w))
			}
			v = append(v, n)
		}
		var sum float64
		for _, x := range v {
			sum += float64(x * x)
		}
		for i := range v {
			v[i] /= float32(math.Sqrt(sum))
		}
		out = append(out, v)
	}
	return out, nil
}

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
	titles, avail := demo.Catalog()
	blurbs := map[string]string{
		"9013": "A mole inside the Circus. Smiley, a retired spy, hunts him through his old Berlin agents.",
		"9016": "Smiley hunts his old adversary across Europe, calling in agents from Berlin. A spy's last case.",
		"9003": "A burned-out spy in Berlin takes one last mission for the Circus.",
		"9018": "The emperor of the desert finds his own prophecies closing around him across the galaxy.",
		"9017": "A submarine war in the North Atlantic, fought by the navy.",
		"9001": "A Soviet submarine captain defects, and the navy races to find him before war breaks out.",
		"9010": "Three brothers in provincial Russia, a murdered father, and a long argument about faith.",
	}
	for i := range titles {
		titles[i].Description = blurbs[titles[i].ID]
	}
	if err := st.SaveCatalog(ctx, demo.Library, titles, avail, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := resolve.Run(ctx, st, resolve.Options{}); err != nil {
		t.Fatal(err)
	}
	for _, step := range []func(context.Context, *store.Store) error{
		func(ctx context.Context, st *store.Store) error { return signals.Run(ctx, st, signals.Options{}) },
		library.Build, search.Build,
	} {
		if err := step(ctx, st); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := embed.Run(ctx, st, themeEmbedder{}, embed.Options{}); err != nil {
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
