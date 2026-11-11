// Package enrich looks up descriptions for the books I've finished that no
// source describes, so they can be seeds too (spec §3.5). The library
// describes every book it lends, and Reading List carries Google Books
// descriptions for most of the rest, but some finished books, mostly print
// ones the library doesn't carry, have no text to compare, and recommend
// has to skip them.
//
// Google Books is asked first: its descriptions are publisher copy, the
// same kind of text as the catalog's, and similarity is only fair between
// texts of the same kind. Open Library is the fallback. Books are looked up
// by ISBN first, then by title and author, with the answer checked against
// both. Every answer, "found nothing" included, is cached in
// description_lookup, so each question is asked once.
//
// What it finds becomes a shelf like any other source's ("enrich"): one
// item per book, with the book's own title and author, so the next build
// resolves it into the same work and the next embed picks up its text.
package enrich

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/KevinNavarreteParra/recbook/internal/blurb"
	"github.com/KevinNavarreteParra/recbook/internal/contract"
	"github.com/KevinNavarreteParra/recbook/internal/polite"
	"github.com/KevinNavarreteParra/recbook/internal/store"
	"github.com/KevinNavarreteParra/recbook/internal/textkey"
)

//go:embed wanting.sql
var wantingSQL string

// Source is the shelf this package writes.
const Source = "enrich"

// maxISBNs caps the ISBNs tried per book per service; a book with many
// editions would otherwise cost many requests.
const maxISBNs = 3

// Book is what a service found.
type Book struct {
	ID          string // the service's id for it
	Title       string // as the service names it
	Description string // raw; may be HTML
}

// Query is one question for a service: an ISBN, or else a title and author.
type Query struct {
	ISBN   string
	Title  string
	Author string
}

// key names the question in the cache. Titles and authors go in as
// matching keys, so small differences in spelling don't ask twice.
func (q Query) key() string {
	if q.ISBN != "" {
		return "isbn:" + q.ISBN
	}
	return "title:" + textkey.Title(q.Title) + "|author:" + textkey.Author(q.Author)
}

// A Service looks books up.
type Service interface {
	// Ref is the contract ref key for the service's ids. It also names the
	// service in reports and in the cache.
	Ref() string
	// Lookup returns the book q names, or false if the service doesn't
	// know it or has no description of it.
	Lookup(ctx context.Context, q Query) (Book, bool, error)
}

// Services returns Google Books and Open Library, in that order, each
// asked at most once a second. googleKey is a Google Books API key, or ""
// to go without.
func Services(googleKey string) []Service {
	const agent = "recbook/0.1 (personal reading list; descriptions of books I've read)"
	return []Service{
		&GoogleBooks{HTTP: polite.NewClient(polite.Config{UserAgent: agent}), Key: googleKey},
		&OpenLibrary{HTTP: polite.NewClient(polite.Config{UserAgent: agent})},
	}
}

// Report summarizes a run.
type Report struct {
	Wanting   int            // finished books no other source describes
	Described map[string]int // of those, how many each service described
	Thin      int            // described, but only briefly
	Asked     int            // questions sent this run; the rest were cached
	// Failed holds the services that failed and why. A service is left
	// alone for the rest of a run after its first failure (Google Books,
	// for one, refuses keyless requests once a shared daily quota is
	// spent), and nothing it failed to answer is cached, so a later run
	// asks again.
	Failed map[string]error
}

// want is a finished book without a description.
type want struct {
	workID int64
	title  string
	author string
	isbns  []string
}

// queries are the questions to ask about w, in order.
func (w want) queries() []Query {
	var qs []Query
	for _, isbn := range w.isbns[:min(len(w.isbns), maxISBNs)] {
		qs = append(qs, Query{ISBN: isbn})
	}
	if w.title != "" && w.author != "" {
		qs = append(qs, Query{Title: w.title, Author: w.author})
	}
	return qs
}

// run is one Run's state.
type run struct {
	st       *store.Store
	services []Service
	cache    map[[2]string]Book // (service, query key) → answer; Book{} for none
	report   Report
}

// Run looks up a description for every finished book that has none, asking
// the services only what the cache can't answer, and replaces the enrich
// shelf with everything found. An interrupted run leaves the shelf as it
// was; what it asked stays cached.
func Run(ctx context.Context, st *store.Store, services []Service) (Report, error) {
	r := &run{st: st, services: services, report: Report{Described: map[string]int{}, Failed: map[string]error{}}}
	wants, err := loadWanting(ctx, st)
	if err != nil {
		return r.report, err
	}
	r.report.Wanting = len(wants)
	if r.cache, err = loadCache(ctx, st); err != nil {
		return r.report, err
	}

	var shelf contract.Shelf
	ids := map[string]bool{}
	for _, w := range wants {
		book, ref, ok, err := r.describe(ctx, w)
		if err != nil {
			return r.report, err
		}
		// Two items sharing a ref would merge their works, so one book can
		// describe only one work.
		id := ref + ":" + book.ID
		if !ok || ids[id] {
			continue
		}
		ids[id] = true
		r.report.Described[ref]++
		if blurb.IsThin(blurb.Clean(book.Description)) {
			r.report.Thin++
		}
		author := w.author
		if author == "" {
			author = "Unknown"
		}
		shelf.Items = append(shelf.Items, contract.ShelfItem{
			ID:          id,
			Title:       w.title,
			Authors:     []string{author},
			ISBNs:       w.isbns,
			Format:      contract.UnknownFormat,
			Refs:        map[string]string{ref: book.ID},
			Description: book.Description,
		})
	}
	return r.report, st.ReplaceShelf(ctx, Source, "Google Books and Open Library", &shelf)
}

// describe finds w's description: the first that isn't thin, trying each
// service in order and, within one, each query in order; failing that, the
// longest thin one. ref says which service it came from.
func (r *run) describe(ctx context.Context, w want) (book Book, ref string, ok bool, err error) {
	for _, svc := range r.services {
		for _, q := range w.queries() {
			if r.report.Failed[svc.Ref()] != nil {
				break
			}
			b, found, err := r.lookup(ctx, svc, q)
			if err != nil {
				return Book{}, "", false, err
			}
			if !found {
				continue
			}
			if !blurb.IsThin(blurb.Clean(b.Description)) {
				return b, svc.Ref(), true, nil
			}
			if !ok || len(b.Description) > len(book.Description) {
				book, ref, ok = b, svc.Ref(), true
			}
		}
	}
	return book, ref, ok, nil
}

// lookup answers q from the cache, or else asks svc and caches the answer.
// When svc fails, it records the failure in the report and finds nothing;
// the error it returns is the store's, or the context's.
func (r *run) lookup(ctx context.Context, svc Service, q Query) (Book, bool, error) {
	k := [2]string{svc.Ref(), q.key()}
	if b, ok := r.cache[k]; ok {
		return b, b.ID != "", nil
	}
	b, found, err := svc.Lookup(ctx, q)
	if err != nil {
		if ctx.Err() != nil {
			return Book{}, false, ctx.Err()
		}
		r.report.Failed[svc.Ref()] = fmt.Errorf("%s: %w", k[1], err)
		return Book{}, false, nil
	}
	if !found {
		b = Book{}
	}
	r.report.Asked++
	if err := saveLookup(ctx, r.st, k, b); err != nil {
		return Book{}, false, err
	}
	r.cache[k] = b
	return b, found, nil
}

func loadWanting(ctx context.Context, st *store.Store) ([]want, error) {
	rows, err := st.DB().QueryContext(ctx, wantingSQL, sql.Named("source", Source))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var wants []want
	for rows.Next() {
		var w want
		var isbns []any
		if err := rows.Scan(&w.workID, &w.title, &w.author, &isbns); err != nil {
			return nil, err
		}
		for _, isbn := range isbns {
			w.isbns = append(w.isbns, isbn.(string))
		}
		wants = append(wants, w)
	}
	return wants, rows.Err()
}

func loadCache(ctx context.Context, st *store.Store) (map[[2]string]Book, error) {
	rows, err := st.DB().QueryContext(ctx,
		`SELECT service, query, coalesce(book_id, ''), coalesce(title, ''), coalesce(description, '') FROM description_lookup`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cache := map[[2]string]Book{}
	for rows.Next() {
		var k [2]string
		var b Book
		if err := rows.Scan(&k[0], &k[1], &b.ID, &b.Title, &b.Description); err != nil {
			return nil, err
		}
		cache[k] = b
	}
	return cache, rows.Err()
}

func saveLookup(ctx context.Context, st *store.Store, k [2]string, b Book) error {
	_, err := st.DB().ExecContext(ctx,
		`INSERT OR REPLACE INTO description_lookup VALUES (?, ?, ?, ?, ?, ?)`,
		k[0], k[1], time.Now().UTC(), nullIfEmpty(b.ID), nullIfEmpty(b.Title), nullIfEmpty(b.Description))
	return err
}

func nullIfEmpty(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}

// sameBook reports whether a book a service found by title is the one q
// asks for: by the same author, with the same title, either whole or
// before any subtitle ("Emile, or On Education" is "Emile: Or On
// Education"; "Tyrants" is "Tyrants: History's 100 Most Evil Despots").
func sameBook(q Query, title string, authors []string) bool {
	want, got := titleKeys(q.Title), titleKeys(title)
	sameTitle := slices.ContainsFunc(want, func(k string) bool { return slices.Contains(got, k) })
	return sameTitle && slices.ContainsFunc(authors, func(a string) bool {
		return textkey.Author(a) == textkey.Author(q.Author)
	})
}

// titleKeys are a title's matching keys, whole and before any colon.
func titleKeys(title string) []string {
	title = withoutSeries(title)
	return []string{textkey.Title(title), textkey.MainTitle(title)}
}

// searchTitle is the part of a title worth searching for: before any
// colon, without a trailing "(Series Name)".
func searchTitle(title string) string {
	main, _, _ := strings.Cut(withoutSeries(title), ":")
	return strings.TrimSpace(main)
}

// withoutSeries drops a trailing parenthesis, which in shelf exports is
// usually a series or imprint: "Second Treatise of Government (Hackett
// Classics)".
func withoutSeries(title string) string {
	title = strings.TrimSpace(title)
	if i := strings.LastIndex(title, " ("); i > 0 && strings.HasSuffix(title, ")") {
		title = title[:i]
	}
	return title
}

// surname is the part of an author's name worth searching for.
func surname(author string) string {
	name, _, _ := strings.Cut(textkey.Author(author), " ")
	return name
}
