// Package readinglist reads the CSV export of the Reading List iOS app
// (Settings → Export) into the shelf contract.
//
// What the export holds, as of November 2026:
//
//   - One row per book, with a stable "Reading List ID".
//   - "Started Reading", "Finished Reading" and "Did Not Finish" dates,
//     typed in by hand. They often share one day, and early ones were
//     logged in catch-up batches.
//   - A book with none of those dates is on the to-read list. There is no
//     "date added" column, so those wanted events are undated, and a book's
//     to-read history is lost once it is started.
//   - No format: the same app logs print books and audiobooks.
//
// Ratings, notes, lists and the "Paused" date are not carried over.
package readinglist

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/KevinNavarreteParra/recbook/internal/contract"
)

// Read parses a Reading List CSV export.
func Read(r io.Reader) (*contract.Shelf, error) {
	cr := csv.NewReader(r)
	header, err := cr.Read()
	if err != nil {
		return nil, fmt.Errorf("reading header: %w", err)
	}
	col, err := columns(header)
	if err != nil {
		return nil, err
	}

	shelf := &contract.Shelf{}
	for {
		rec, err := cr.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		get := func(name string) string { return strings.TrimSpace(rec[col[name]]) }
		line, _ := cr.FieldPos(0)
		at := func(field string) error { return fmt.Errorf("line %d: %s", line, field) }

		item := contract.ShelfItem{
			ID:          get("Reading List ID"),
			Title:       get("Title"),
			Subtitle:    get("Subtitle"),
			Authors:     splitList(get("Authors")),
			Format:      contract.UnknownFormat,
			Description: get("Description"),
			Subjects:    splitList(get("Subjects")),
			Language:    get("Language Code"),
		}
		if len(item.Authors) == 0 {
			item.Authors = []string{"Unknown"}
		}
		if isbn, err := contract.NormalizeISBN(get("ISBN-13")); err == nil {
			item.ISBNs = []string{isbn}
		}
		if id := get("Google Books ID"); id != "" {
			item.Refs = map[string]string{contract.RefGoogleBooks: id}
		}
		item.Pages, _ = strconv.Atoi(get("Page Count"))
		if date := get("Publication Date"); len(date) >= 4 {
			item.PubYear, _ = strconv.Atoi(date[:4])
		}
		shelf.Items = append(shelf.Items, item)

		// Each reading date becomes an event; a book with none is to-read.
		dated := false
		for _, d := range []struct {
			column string
			kind   contract.EventKind
		}{
			{"Started Reading", contract.Started},
			{"Finished Reading", contract.Finished},
			{"Did Not Finish", contract.Abandoned},
		} {
			value := get(d.column)
			if value == "" {
				continue
			}
			day, err := contract.ParseDay(time.DateOnly, value)
			if err != nil {
				return nil, at(fmt.Sprintf("%s %q: %v", d.column, value, err))
			}
			shelf.Events = append(shelf.Events, contract.ShelfEvent{
				ItemID: item.ID, Kind: d.kind, On: day, DateQuality: contract.DateUser,
			})
			dated = true
		}
		if !dated {
			shelf.Events = append(shelf.Events, contract.ShelfEvent{ItemID: item.ID, Kind: contract.Wanted})
		}
	}
	return shelf, nil
}

// required lists the columns Read uses; an export missing any is rejected.
var required = []string{
	"Reading List ID", "Google Books ID", "ISBN-13", "Title", "Subtitle", "Authors", "Page Count",
	"Publication Date", "Description", "Subjects", "Language Code",
	"Started Reading", "Finished Reading", "Did Not Finish",
}

// columns maps each required column name to its position.
func columns(header []string) (map[string]int, error) {
	col := map[string]int{}
	for i, name := range header {
		col[strings.TrimPrefix(name, "\ufeff")] = i // tolerate a byte-order mark
	}
	var missing []string
	for _, name := range required {
		if _, ok := col[name]; !ok {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("not a Reading List export: missing columns %q", missing)
	}
	return col, nil
}

// splitList splits a "; "-separated field. The app escapes a literal
// semicolon inside a value as "\;".
func splitList(s string) []string {
	const placeholder = "\x00"
	s = strings.ReplaceAll(s, `\;`, placeholder)
	var out []string
	for _, part := range strings.Split(s, ";") {
		if part = strings.TrimSpace(strings.ReplaceAll(part, placeholder, ";")); part != "" {
			out = append(out, part)
		}
	}
	return out
}
