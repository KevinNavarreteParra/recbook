// Package librarything reads LibraryThing's JSON export (More → Import/Export
// → Export, JSON) into the shelf contract: the books I own.
//
// What the export holds, as of November 2026:
//
//   - An object keyed by book id, one entry per copy I've catalogued.
//   - "workcode", LibraryThing's id for the work across editions, which
//     makes a strong cross-source match.
//   - "format" separating print from ebook and audiobook.
//   - No acquisition date. The only date is "entrydate", the day the book
//     was catalogued, and books were mostly catalogued in large batches.
//     It is used as a fuzzy acquisition date with user quality, so batch
//     detection discards the batched ones.
//   - "summary" is generated ("Title by Author (year)"), so it isn't kept
//     as a description.
//
// DDC and LCC classifications are left out until the genre view needs them;
// re-ingesting the export brings them in.
package librarything

import (
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/KevinNavarreteParra/recbook/internal/contract"
)

// book is one entry, limited to the fields used. Several fields change
// shape between entries, so they're decoded by hand below.
type book struct {
	BooksID       string            `json:"books_id"`
	Title         string            `json:"title"`
	PrimaryAuthor string            `json:"primaryauthor"`
	Authors       []json.RawMessage `json:"authors"` // usually {lf, fl, role}; sometimes []
	Workcode      string            `json:"workcode"`
	EntryDate     string            `json:"entrydate"` // "2023-05-21"
	Date          string            `json:"date"`      // publication year, "1965"
	Pages         string            `json:"pages"`     // "416 "
	ISBN          json.RawMessage   `json:"isbn"`      // {"0": isbn10, "2": isbn13}, a list, or absent
	OriginalISBN  string            `json:"originalisbn"`
	Format        []struct {
		Code string `json:"code"` // "1.1.1" paperback, "1.2.1" digital audiobook, "1.3" ebook
	} `json:"format"`
	Subject   json.RawMessage `json:"subject"` // headings: {"0": [...], ...} or [[...], ...]
	Genre     []string        `json:"genre"`
	Series    []string        `json:"series"`
	Languages []string        `json:"language_codeA"` // ISO 639-2: "eng"
}

type author struct {
	LF   string `json:"lf"` // "Foucault, Michel"
	Role string `json:"role"`
}

// Read parses a LibraryThing JSON export.
func Read(r io.Reader) (*contract.Shelf, error) {
	var books map[string]book
	if err := json.NewDecoder(r).Decode(&books); err != nil {
		return nil, fmt.Errorf("not a LibraryThing JSON export: %w", err)
	}

	// Map order is random; sort so the same file always reads the same.
	ids := make([]string, 0, len(books))
	for id := range books {
		ids = append(ids, id)
	}
	slices.Sort(ids)

	shelf := &contract.Shelf{}
	for _, id := range ids {
		b := books[id]
		item := contract.ShelfItem{
			ID:       id,
			Title:    strings.TrimSpace(b.Title),
			Authors:  authorsOf(b),
			ISBNs:    isbnsOf(b),
			Format:   formatOf(b),
			Subjects: subjectsOf(b),
			Language: iso6391[firstOr(b.Languages, "")],
		}
		if b.Workcode != "" {
			item.Refs = map[string]string{contract.RefLibraryThingWork: b.Workcode}
		}
		if len(b.Series) > 0 {
			item.Series = b.Series[0]
		}
		if len(b.Date) >= 4 {
			item.PubYear, _ = strconv.Atoi(b.Date[:4])
		}
		item.Pages, _ = strconv.Atoi(strings.TrimSpace(b.Pages))
		shelf.Items = append(shelf.Items, item)

		acquired := contract.ShelfEvent{ItemID: id, Kind: contract.Acquired}
		if b.EntryDate != "" {
			day, err := contract.ParseDay(time.DateOnly, b.EntryDate)
			if err != nil {
				return nil, fmt.Errorf("book %s: entrydate %q: %w", id, b.EntryDate, err)
			}
			acquired.On, acquired.DateQuality = day, contract.DateUser
		}
		shelf.Events = append(shelf.Events, acquired)
	}
	return shelf, nil
}

// authorsOf returns the people credited as authors, "Last, First", falling
// back to the primary author (who may be an editor) when no one is.
func authorsOf(b book) []string {
	var out []string
	for _, raw := range b.Authors {
		var a author
		if json.Unmarshal(raw, &a) != nil || strings.TrimSpace(a.LF) == "" {
			continue // an empty [] stands in for "no author" in some entries
		}
		switch a.Role {
		case "", "Author", "Autor":
			out = append(out, strings.TrimSpace(a.LF))
		}
	}
	if len(out) == 0 && b.PrimaryAuthor != "" {
		out = []string{b.PrimaryAuthor}
	}
	if len(out) == 0 {
		out = []string{"Unknown"}
	}
	return out
}

// isbnsOf collects every valid ISBN from the "isbn" field, whichever shape
// it has, and "originalisbn".
func isbnsOf(b book) []string {
	var candidates []string
	var asMap map[string]string
	var asList []string
	switch {
	case json.Unmarshal(b.ISBN, &asMap) == nil:
		for _, v := range asMap {
			candidates = append(candidates, v)
		}
	case json.Unmarshal(b.ISBN, &asList) == nil:
		candidates = asList
	}
	candidates = append(candidates, b.OriginalISBN)

	var out []string
	for _, c := range candidates {
		if isbn, err := contract.NormalizeISBN(c); err == nil && !slices.Contains(out, isbn) {
			out = append(out, isbn)
		}
	}
	slices.Sort(out)
	return out
}

func formatOf(b book) contract.Format {
	if len(b.Format) == 0 {
		return contract.UnknownFormat
	}
	switch code := b.Format[0].Code; {
	case strings.HasPrefix(code, "1.1"):
		return contract.Print
	case strings.HasPrefix(code, "1.2"):
		return contract.Audiobook
	case strings.HasPrefix(code, "1.3"):
		return contract.Ebook
	default:
		return contract.UnknownFormat
	}
}

// subjectsOf merges subject headings and genres, without repeats.
func subjectsOf(b book) []string {
	var out []string
	add := func(s string) {
		if s = strings.TrimSpace(s); s != "" && !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	for _, heading := range headings(b.Subject) {
		add(strings.Join(heading, " -- ")) // e.g. "Civilization -- History"
	}
	for _, g := range b.Genre {
		add(g)
	}
	return out
}

// headings decodes the subject field, which is either an object of
// headings keyed "0", "2", ... or a plain list of them.
func headings(raw json.RawMessage) [][]string {
	var asList [][]string
	if json.Unmarshal(raw, &asList) == nil {
		return asList
	}
	var asMap map[string][]string
	if json.Unmarshal(raw, &asMap) != nil {
		return nil
	}
	keys := make([]string, 0, len(asMap))
	for k := range asMap {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	var out [][]string
	for _, k := range keys {
		out = append(out, asMap[k])
	}
	return out
}

func firstOr(s []string, fallback string) string {
	if len(s) == 0 {
		return fallback
	}
	return s[0]
}

// iso6391 maps the ISO 639-2 codes LibraryThing uses to the contract's ISO
// 639-1. Codes not listed are dropped rather than guessed.
var iso6391 = map[string]string{
	"eng": "en", "spa": "es", "por": "pt", "fre": "fr", "fra": "fr", "ger": "de", "deu": "de",
	"ita": "it", "rus": "ru", "gre": "el", "ell": "el", "lat": "la", "jpn": "ja", "chi": "zh", "zho": "zh",
}
