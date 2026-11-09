package jsonl

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KevinNavarreteParra/recbook/internal/contract"
)

func TestRead(t *testing.T) {
	in := `{"item": {"id": "1", "title": "Dune", "authors": ["Frank Herbert"], "format": "print", "refs": {"goodreads": "44767458"}}}

{"event": {"item_id": "1", "kind": "finished", "on": "2021-05-03", "date_quality": "user"}}
{"event": {"item_id": "1", "kind": "acquired"}}
`
	shelf, err := Read(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(shelf.Items) != 1 || len(shelf.Events) != 2 {
		t.Fatalf("got %d items, %d events", len(shelf.Items), len(shelf.Events))
	}
	if ev := shelf.Events[0]; ev.On != contract.NewDay(2021, 5, 3) || ev.DateQuality != contract.DateUser {
		t.Errorf("event 0 = %+v", ev)
	}
	if !shelf.Events[1].On.IsZero() {
		t.Error("an event without \"on\" is undated")
	}
	if err := shelf.Validate(); err != nil {
		t.Error(err)
	}
}

func TestReadRejects(t *testing.T) {
	for name, in := range map[string]string{
		"misspelled field": `{"item": {"id": "1", "titel": "Dune"}}`,
		"both kinds":       `{"item": {"id": "1"}, "event": {"item_id": "1"}}`,
		"neither":          `{}`,
		"not json":         `id,title`,
	} {
		if _, err := Read(strings.NewReader(in)); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

// The demo files must always satisfy the contract.
func TestDemoFilesAreValid(t *testing.T) {
	paths, _ := filepath.Glob("../../demo/*.jsonl")
	if len(paths) == 0 {
		t.Fatal("no demo files found")
	}
	for _, path := range paths {
		f, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		shelf, err := Read(f)
		f.Close()
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if err := shelf.Validate(); err != nil {
			t.Errorf("%s: %v", path, err)
		}
	}
}

func TestWriteRoundTrips(t *testing.T) {
	in := &contract.Shelf{
		Items:  []contract.ShelfItem{{ID: "1", Title: "Dune", Authors: []string{"Frank Herbert"}, Format: contract.Audiobook}},
		Events: []contract.ShelfEvent{{ItemID: "1", Kind: contract.Finished, On: contract.NewDay(2026, 11, 9), DateQuality: contract.DateSystem}},
	}
	var buf strings.Builder
	if err := Write(&buf, in); err != nil {
		t.Fatal(err)
	}
	out, err := Read(strings.NewReader(buf.String()))
	if err != nil {
		t.Fatalf("%v\n%s", err, buf.String())
	}
	if len(out.Items) != 1 || out.Items[0].Title != "Dune" || len(out.Events) != 1 || out.Events[0].On != in.Events[0].On {
		t.Errorf("round trip changed the shelf:\n%s", buf.String())
	}
}
