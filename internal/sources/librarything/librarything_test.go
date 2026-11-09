package librarything

import (
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/KevinNavarreteParra/recbook/internal/contract"
)

func TestRead(t *testing.T) {
	f, err := os.Open("testdata/export.json")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	shelf, err := Read(f)
	if err != nil {
		t.Fatal(err)
	}
	if err := shelf.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(shelf.Items) != 3 || len(shelf.Events) != 3 {
		t.Fatalf("got %d items, %d events", len(shelf.Items), len(shelf.Events))
	}

	ledger := shelf.Items[0]
	checks := []struct {
		what string
		ok   bool
	}{
		{"translator left out of authors", slices.Equal(ledger.Authors, []string{"Fenwick, Ada"})},
		{"only valid ISBNs, once each", slices.Equal(ledger.ISBNs, []string{"9798999900036"})},
		{"workcode as a ref", ledger.Refs[contract.RefLibraryThingWork] == "55501"},
		{"paperback is print", ledger.Format == contract.Print},
		{"subjects and genres merged", slices.Equal(ledger.Subjects, []string{"Lighthouses -- Fiction", "Coasts", "Fiction", "General Fiction"})},
		{"no generated summary", ledger.Description == ""},
		{"language mapped to ISO 639-1", ledger.Language == "en"},
		{"series, year, pages", ledger.Series == "Northern Coast" && ledger.PubYear == 2021 && ledger.Pages == 352},
	}
	for _, c := range checks {
		if !c.ok {
			t.Errorf("%s: %+v", c.what, ledger)
		}
	}

	collected := shelf.Items[1]
	if collected.Title != "Okafor's Collected Tides & Songs" {
		t.Errorf("HTML entities should be unescaped: %q", collected.Title)
	}
	if !slices.Equal(collected.Subjects, []string{"Tides", "Poetry -- Collections"}) {
		t.Errorf("subjects given as a list = %q", collected.Subjects)
	}
	if !slices.Equal(collected.Authors, []string{"June Okafor"}) || collected.Format != contract.Audiobook || collected.Language != "" {
		t.Errorf("an editor-only book falls back to the primary author: %+v", collected)
	}
	if got := shelf.Items[2].Authors; !slices.Equal(got, []string{"Unknown"}) {
		t.Errorf("no author at all: %q", got)
	}

	if ev := shelf.Events[0]; ev.Kind != contract.Acquired || ev.On.String() != "2023-05-21" || ev.DateQuality != contract.DateUser {
		t.Errorf("entry date should be a user-quality acquisition: %+v", ev)
	}
	if !shelf.Events[2].On.IsZero() {
		t.Error("an empty entrydate is an undated acquisition")
	}
}

func TestReadRejectsOtherFiles(t *testing.T) {
	if _, err := Read(strings.NewReader(`[1, 2, 3]`)); err == nil {
		t.Error("want an error for a JSON list")
	}
}
