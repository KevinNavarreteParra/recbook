package embed

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/KevinNavarreteParra/recbook/internal/contract"
	"github.com/KevinNavarreteParra/recbook/internal/demo"
	"github.com/KevinNavarreteParra/recbook/internal/resolve"
	"github.com/KevinNavarreteParra/recbook/internal/store"
)

// fakeEmbedder returns a fixed vector and records what it was sent.
type fakeEmbedder struct{ inputs []string }

func (f *fakeEmbedder) Embed(_ context.Context, _ string, inputs []string) ([][]float32, error) {
	f.inputs = append(f.inputs, inputs...)
	out := make([][]float32, len(inputs))
	for i := range out {
		out[i] = []float32{1, 0}
	}
	return out, nil
}

// demoWithBlurbs loads the demo and gives a few catalog titles descriptions.
func demoWithBlurbs(t *testing.T) *store.Store {
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
		"9016": "<p>“A masterpiece.” —Invented Weekly</p><p>Smiley hunts his old adversary across Europe, one favour at a time, " +
			"calling in debts from agents long retired, until the trap he has spent his life setting closes in Berlin.</p>",
		"9017": "World War Three, fought by submarines.",
		"9018": "Twelve years after the desert war, the emperor finds that ruling a galaxy is harder than winning it, " +
			"and the prophecies that made him begin to close around him like a trap he set himself.",
	}
	for i := range titles {
		titles[i].Description = blurbs[titles[i].ID]
	}
	if err := st.SaveCatalog(ctx, demo.Library, titles, avail, contract.NewDay(2026, 11, 1).Time()); err != nil {
		t.Fatal(err)
	}
	if _, err := resolve.Run(ctx, st, resolve.Options{}); err != nil {
		t.Fatal(err)
	}
	return st
}

func TestRunEmbedsOnceAndResumes(t *testing.T) {
	ctx := context.Background()
	st := demoWithBlurbs(t)

	// A run stopped after the first batch keeps what it embedded.
	f := &fakeEmbedder{}
	stop := errors.New("GPU busy")
	calls := 0
	rep, err := Run(ctx, st, f, Options{BatchSize: 1, Between: func(context.Context) error {
		if calls++; calls > 1 {
			return stop
		}
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Works != 3 || rep.Embedded != 1 || rep.Left != 2 || !errors.Is(rep.Stopped, stop) {
		t.Fatalf("stopped run: %+v", rep)
	}
	if rep.Thin != 1 {
		t.Errorf("the one-line blurb should be thin: %+v", rep)
	}

	// The next run embeds only what's left, then nothing.
	rep, err = Run(ctx, st, f, Options{})
	if err != nil || rep.Embedded != 2 || rep.Left != 0 {
		t.Fatalf("resumed run: %+v, %v", rep, err)
	}
	rep, err = Run(ctx, st, f, Options{})
	if err != nil || rep.Embedded != 0 || rep.Left != 0 {
		t.Fatalf("third run should find everything cached: %+v, %v", rep, err)
	}

	// A new record that sorts first shifts every work id on the next build.
	// work_text is stale until WriteTexts rebuilds it; the cached vectors
	// still apply because they're keyed by text.
	early := &contract.Shelf{Items: []contract.ShelfItem{{ID: "1", Title: "Aardvarks", Authors: []string{"Ada Fenwick"}, Format: contract.Print}}}
	if err := st.ReplaceShelf(ctx, "aaa", "test", early); err != nil {
		t.Fatal(err)
	}
	if _, err := resolve.Run(ctx, st, resolve.Options{}); err != nil {
		t.Fatal(err)
	}
	stale := func() int {
		var n int
		st.DB().QueryRow(`SELECT count(*) FROM work_text t JOIN work w USING (work_id)
			WHERE NOT starts_with(t.text, w.title)`).Scan(&n)
		return n
	}
	if stale() == 0 {
		t.Fatal("test setup: the new record should have shifted the work ids")
	}
	if _, err := WriteTexts(ctx, st, nil); err != nil {
		t.Fatal(err)
	}
	if n := stale(); n != 0 {
		t.Errorf("%d work_text rows point at the wrong work after WriteTexts", n)
	}
	if rep, _ := Run(ctx, st, f, Options{}); rep.Embedded != 0 {
		t.Errorf("renumbering shouldn't cause re-embedding: %+v", rep)
	}

	for _, in := range f.inputs {
		if !strings.HasPrefix(in, "clustering: ") {
			t.Errorf("inputs need the model's task prefix: %q", in)
		}
		if strings.Contains(in, "masterpiece") || strings.Contains(in, "<p>") {
			t.Errorf("blurbs should be cleaned before embedding: %q", in)
		}
	}

	var joined int
	st.DB().QueryRow(`SELECT count(*) FROM work_text JOIN embedding USING (text_hash)`).Scan(&joined)
	if joined != 3 {
		t.Errorf("every work_text row should join to its vector; got %d", joined)
	}
}

func TestRunStopsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	rep, err := Run(ctx, demoWithBlurbs(t), &fakeEmbedder{}, Options{})
	if err != nil || rep.Embedded != 0 || !errors.Is(rep.Stopped, context.Canceled) {
		t.Errorf("a cancelled run should stop cleanly: %+v, %v", rep, err)
	}
}

func TestWriteTextsPrefersMyLanguages(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := demo.Load(ctx, st); err != nil {
		t.Fatal(err)
	}
	// The library has Project Hail Mary only in Spanish, described in
	// Spanish; my reading app describes it in English. The loan of the
	// Spanish recording joins the two.
	titles, avail := demo.Catalog()
	for i := range titles {
		if titles[i].ID == "9005" {
			titles[i].Title, titles[i].Language = "Proyecto Hail Mary", "es"
			titles[i].Description = "Ryland Grace es el único superviviente de una misión desesperada."
		}
	}
	if err := st.SaveCatalog(ctx, demo.Library, titles, avail, contract.NewDay(2026, 11, 1).Time()); err != nil {
		t.Fatal(err)
	}
	shelf := &contract.Shelf{Items: []contract.ShelfItem{{
		ID: "rl-1", Title: "Project Hail Mary", Authors: []string{"Andy Weir"}, Format: contract.Print, Language: "en",
		Description: "Ryland Grace is the sole survivor on a desperate, last-chance mission.",
	}}}
	if err := st.ReplaceShelf(ctx, "reading", "test", shelf); err != nil {
		t.Fatal(err)
	}
	if _, err := resolve.Run(ctx, st, resolve.Options{}); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		languages                []string
		source, language, prefix string
	}{
		{[]string{"en"}, "reading", "en", "Project Hail Mary. Ryland Grace is"},
		{nil, "catalog", "es", "Proyecto Hail Mary. Ryland Grace es"}, // any language: the catalog's blurb
		{[]string{"es", "en"}, "catalog", "es", "Proyecto Hail Mary. Ryland Grace es"},
	}
	for _, tt := range tests {
		if _, err := WriteTexts(ctx, st, tt.languages); err != nil {
			t.Fatal(err)
		}
		var source, language, text string
		err := st.DB().QueryRow(`SELECT source, language, text FROM work_text
			WHERE work_id = (SELECT work_id FROM work_record WHERE ref = 'catalog:demo:9005')`).Scan(&source, &language, &text)
		if err != nil {
			t.Fatal(err)
		}
		if source != tt.source || language != tt.language || !strings.HasPrefix(text, tt.prefix) {
			t.Errorf("languages %v: %s (%s) %q, want %s (%s) %q...", tt.languages, source, language, text,
				tt.source, tt.language, tt.prefix)
		}
	}
}
