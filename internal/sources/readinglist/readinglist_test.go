package readinglist

import (
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/KevinNavarreteParra/recbook/internal/contract"
)

func TestRead(t *testing.T) {
	f, err := os.Open("testdata/export.csv")
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
	if len(shelf.Items) != 5 {
		t.Fatalf("got %d items", len(shelf.Items))
	}

	first := shelf.Items[0]
	if first.Subtitle != "A Novel" || first.Refs[contract.RefGoogleBooks] != "gb-001" || first.Pages != 352 ||
		first.PubYear != 2021 || first.Format != contract.UnknownFormat || !slices.Equal(first.Subjects, []string{"Fiction", "Literary"}) {
		t.Errorf("item 0 = %+v", first)
	}
	if got := shelf.Items[1].Authors; !slices.Equal(got, []string{"Okafor, June", "Reyes, Tomas"}) {
		t.Errorf("authors = %q", got)
	}
	if got := shelf.Items[2].Subjects; !slices.Equal(got, []string{"Odd; Subject", "Second"}) {
		t.Errorf(`escaped "\;" should stay inside one subject: %q`, got)
	}
	if len(shelf.Items[3].ISBNs) != 0 {
		t.Error("an invalid ISBN should be dropped, not passed on")
	}

	var kinds []string
	for _, ev := range shelf.Events {
		kinds = append(kinds, ev.ItemID+" "+string(ev.Kind)+" "+ev.On.String())
	}
	want := []string{
		"AAAA-1 started 2023-01-02", "AAAA-1 finished 2023-01-20",
		// No dates at all: on the to-read list.
		"AAAA-2 wanted ",
		// Did not finish: its finish date is the day I gave up.
		"AAAA-3 started 2024-02-01", "AAAA-3 abandoned 2024-02-10",
		// Paused is ignored.
		"AAAA-4 started 2026-10-01",
		// Did not finish, with no finish date.
		"AAAA-5 started 2025-03-01", "AAAA-5 abandoned ",
	}
	if !slices.Equal(kinds, want) {
		t.Errorf("events =\n  %q\nwant\n  %q", kinds, want)
	}
}

func TestReadRejectsOddFlags(t *testing.T) {
	f, err := os.ReadFile("testdata/export.csv")
	if err != nil {
		t.Fatal(err)
	}
	odd := strings.Replace(string(f), "2024-02-10,Y,", "2024-02-10,2024-02-10,", 1)
	if _, err := Read(strings.NewReader(odd)); err == nil || !strings.Contains(err.Error(), "want Y or nothing") {
		t.Errorf("a Did Not Finish that isn't Y should be refused, got %v", err)
	}
}

func TestReadRejectsOtherCSVs(t *testing.T) {
	_, err := Read(strings.NewReader("Book Id,Title,Author\n1,Dune,Frank Herbert\n"))
	if err == nil || !strings.Contains(err.Error(), "missing columns") {
		t.Errorf("want a missing-columns error, got %v", err)
	}
}
