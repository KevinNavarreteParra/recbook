// Package libby reads Libby's timeline export (Shelf → Timeline → Actions →
// Export Timeline, as JSON) into the shelf contract.
//
// What the export holds, as of November 2026:
//
//   - {"version": 1, "timeline": [...]}, one entry per activity.
//   - Every entry seen so far is "Borrowed". The timeline records no
//     returns, renewals or holds, so it can't tell an early return from an
//     expired loan. A renewal shows up as another "Borrowed" of the same
//     title, typically 21 days after the first, and a later re-borrow looks
//     the same.
//   - Each entry carries the OverDrive title id (title.titleId), which
//     matches the catalog directly, and usually an ISBN for the edition
//     borrowed.
//   - "author" lists several names separated by commas, sometimes including
//     the narrator. Only the first is reliably the author.
//
// Timestamps are recorded by Libby, so every date has system quality.
package libby

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/KevinNavarreteParra/recbook/internal/contract"
)

// export is the file's shape, limited to the fields used.
type export struct {
	Version  int `json:"version"`
	Timeline []struct {
		Activity  string `json:"activity"`
		Timestamp int64  `json:"timestamp"` // milliseconds since the Unix epoch
		Author    string `json:"author"`
		ISBN      string `json:"isbn"`
		Title     struct {
			Text    string `json:"text"`
			TitleID string `json:"titleId"`
		} `json:"title"`
		Cover struct {
			Format string `json:"format"` // "audiobook", "ebook"
		} `json:"cover"`
	} `json:"timeline"`
}

// activities maps Libby's activity names to contract event kinds. Only
// "Borrowed" has been seen in a real export. The others are guesses at
// Libby's wording for the other loan steps; an activity not listed here is
// an error, so a new one can't be dropped silently.
var activities = map[string]contract.EventKind{
	"Borrowed":    contract.Borrowed,
	"Renewed":     contract.Renewed,
	"Returned":    contract.Returned,
	"Placed hold": contract.HoldPlaced,
}

// Read parses a Libby timeline export. Each title becomes one item, keyed
// by its OverDrive id, with one event per timeline entry.
func Read(r io.Reader) (*contract.Shelf, error) {
	var ex export
	if err := json.NewDecoder(r).Decode(&ex); err != nil {
		return nil, fmt.Errorf("not a Libby timeline export: %w", err)
	}
	if ex.Version != 1 {
		return nil, fmt.Errorf("Libby timeline version %d; this reader knows version 1", ex.Version)
	}

	shelf := &contract.Shelf{}
	seen := map[string]bool{}
	for i, e := range ex.Timeline {
		kind, ok := activities[e.Activity]
		if !ok {
			return nil, fmt.Errorf("entry %d: unknown activity %q; add it to libby.activities", i, e.Activity)
		}
		id := e.Title.TitleID
		if id == "" {
			return nil, fmt.Errorf("entry %d (%q): no title id", i, e.Title.Text)
		}

		if !seen[id] {
			seen[id] = true
			item := contract.ShelfItem{
				ID:      id,
				Title:   e.Title.Text,
				Authors: splitAuthors(e.Author),
				Format:  formatOf(e.Cover.Format),
				Refs:    map[string]string{contract.RefOverDrive: id},
			}
			if isbn, err := contract.NormalizeISBN(e.ISBN); err == nil {
				item.ISBNs = []string{isbn}
			}
			shelf.Items = append(shelf.Items, item)
		}

		shelf.Events = append(shelf.Events, contract.ShelfEvent{
			ItemID:      id,
			Kind:        kind,
			On:          contract.DayOf(time.UnixMilli(e.Timestamp).UTC()),
			DateQuality: contract.DateSystem,
		})
	}
	return shelf, nil
}

func formatOf(f string) contract.Format {
	switch f {
	case "audiobook":
		return contract.Audiobook
	case "ebook":
		return contract.Ebook
	default:
		return contract.UnknownFormat
	}
}

// splitAuthors splits Libby's comma-separated names, keeping suffixes such
// as "Jr." attached to the name before them.
func splitAuthors(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		switch {
		case part == "":
		case len(out) > 0 && isSuffix(part):
			out[len(out)-1] += ", " + part
		default:
			out = append(out, part)
		}
	}
	if len(out) == 0 {
		return []string{"Unknown"}
	}
	return out
}

func isSuffix(s string) bool {
	switch strings.ToLower(strings.TrimSuffix(s, ".")) {
	case "jr", "sr", "ii", "iii", "iv", "phd", "md":
		return true
	}
	return false
}
