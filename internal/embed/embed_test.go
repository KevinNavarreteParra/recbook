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
	if err != nil || rep.Cached != 1 || rep.Embedded != 2 || rep.Left != 0 {
		t.Fatalf("resumed run: %+v, %v", rep, err)
	}
	rep, err = Run(ctx, st, f, Options{})
	if err != nil || rep.Embedded != 0 || rep.Cached != 3 {
		t.Fatalf("third run should find everything cached: %+v, %v", rep, err)
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
