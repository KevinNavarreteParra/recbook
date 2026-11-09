package libby

import (
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/KevinNavarreteParra/recbook/internal/contract"
)

func TestRead(t *testing.T) {
	f, err := os.Open("testdata/timeline.json")
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

	if len(shelf.Items) != 2 || len(shelf.Events) != 3 {
		t.Fatalf("got %d items, %d events; a re-borrow should add an event, not an item", len(shelf.Items), len(shelf.Events))
	}
	ledger := shelf.Items[0]
	if ledger.Refs[contract.RefOverDrive] != "424242" || ledger.Format != contract.Audiobook ||
		!slices.Equal(ledger.ISBNs, []string{"9798999900029"}) || ledger.Authors[0] != "Ada Fenwick" {
		t.Errorf("item 0 = %+v", ledger)
	}
	if got := shelf.Items[1].Authors; !slices.Equal(got, []string{"Martin Luther King, Jr.", "Tomas Reyes"}) {
		t.Errorf("suffixes should stay with their name: %q", got)
	}
	if ev := shelf.Events[0]; ev.Kind != contract.Borrowed || ev.DateQuality != contract.DateSystem || ev.On.String() != "2023-11-14" {
		t.Errorf("event 0 = %+v", ev)
	}
}

func TestReadRejects(t *testing.T) {
	for name, in := range map[string]string{
		"wrong version":    `{"version": 2, "timeline": []}`,
		"unknown activity": `{"version": 1, "timeline": [{"activity": "Sampled", "title": {"titleId": "1"}}]}`,
		"not json":         `cover,title`,
	} {
		if _, err := Read(strings.NewReader(in)); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}
