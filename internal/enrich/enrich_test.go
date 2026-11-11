package enrich

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/KevinNavarreteParra/recbook/internal/contract"
	"github.com/KevinNavarreteParra/recbook/internal/demo"
	"github.com/KevinNavarreteParra/recbook/internal/resolve"
	"github.com/KevinNavarreteParra/recbook/internal/signals"
	"github.com/KevinNavarreteParra/recbook/internal/store"
)

// fake answers from a map of query keys, and counts what it's asked.
type fake struct {
	ref     string
	answers map[string]Book
	asked   int
}

func (f *fake) Ref() string { return f.ref }

func (f *fake) Lookup(_ context.Context, q Query) (Book, bool, error) {
	f.asked++
	b, ok := f.answers[q.key()]
	return b, ok, nil
}

func build(t *testing.T, st *store.Store) {
	t.Helper()
	ctx := context.Background()
	if _, err := resolve.Run(ctx, st, resolve.Options{}); err != nil {
		t.Fatal(err)
	}
	if err := signals.Run(ctx, st, signals.Options{}); err != nil {
		t.Fatal(err)
	}
}

func TestRun(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := demo.Load(ctx, st); err != nil {
		t.Fatal(err)
	}
	build(t, st)

	long := strings.Repeat("A desert planet, a noble family, and the spice that everyone wants. ", 4)
	google := &fake{ref: contract.RefGoogleBooks, answers: map[string]Book{
		// Dune: only a thin description, so Open Library is asked too.
		Query{Title: "Dune", Author: "Frank Herbert"}.key(): {ID: "g-dune", Title: "Dune", Description: "Spice."},
	}}
	openLibrary := &fake{ref: contract.RefOpenLibraryWork, answers: map[string]Book{
		Query{Title: "Dune", Author: "Frank Herbert"}.key(): {ID: "OL1W", Title: "Dune", Description: long},
	}}

	rep, err := Run(ctx, st, []Service{google, openLibrary})
	if err != nil {
		t.Fatal(err)
	}
	// The demo catalog has no blurbs, so none of the four finished books
	// has a description.
	if rep.Wanting != 4 {
		t.Errorf("wanting = %d, want 4", rep.Wanting)
	}
	if rep.Described[contract.RefOpenLibraryWork] != 1 || rep.Described[contract.RefGoogleBooks] != 0 {
		t.Errorf("described = %v; Open Library's long text should beat Google's thin one", rep.Described)
	}
	if rep.Asked != google.asked+openLibrary.asked || rep.Asked == 0 {
		t.Errorf("asked = %d, services saw %d", rep.Asked, google.asked+openLibrary.asked)
	}

	// The next build resolves the description into Dune's own work.
	build(t, st)
	var title, description string
	err = st.DB().QueryRowContext(ctx, `
		SELECT w.title, si.description
		FROM shelf_item si
		JOIN work_record wr ON wr.ref = si.source || ':' || si.item_id
		JOIN work w USING (work_id)
		JOIN work_signal s USING (work_id)
		WHERE si.source = 'enrich' AND s.finished`).Scan(&title, &description)
	if err != nil || title != "Dune" || description != long {
		t.Errorf("enrich item resolved to %q (%v)", title, err)
	}

	// Run again: everything comes from the cache, and the shelf stays.
	google.asked, openLibrary.asked = 0, 0
	rep, err = Run(ctx, st, []Service{google, openLibrary})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Asked != 0 || google.asked+openLibrary.asked != 0 {
		t.Errorf("second run asked %d questions; the cache should answer them all", rep.Asked)
	}
	if rep.Described[contract.RefOpenLibraryWork] != 1 {
		t.Errorf("second run described %v; Dune should still be described", rep.Described)
	}
}

// broken fails every lookup, like Google Books once its quota is spent.
type broken struct{ asked int }

func (b *broken) Ref() string { return contract.RefGoogleBooks }

func (b *broken) Lookup(context.Context, Query) (Book, bool, error) {
	b.asked++
	return Book{}, false, errors.New("429 Too Many Requests")
}

func TestFailingServiceIsSkipped(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := demo.Load(ctx, st); err != nil {
		t.Fatal(err)
	}
	build(t, st)

	google := &broken{}
	long := strings.Repeat("Three brothers and their father in provincial Russia. ", 4)
	openLibrary := &fake{ref: contract.RefOpenLibraryWork, answers: map[string]Book{
		Query{Title: "Crime and Punishment", Author: "Fyodor Dostoevsky"}.key(): {ID: "OL2W", Description: long},
	}}
	rep, err := Run(ctx, st, []Service{google, openLibrary})
	if err != nil {
		t.Fatal(err)
	}
	if google.asked != 1 || rep.Failed[contract.RefGoogleBooks] == nil {
		t.Errorf("Google was asked %d times (want 1, then skipped); failed = %v", google.asked, rep.Failed)
	}
	if rep.Described[contract.RefOpenLibraryWork] != 1 {
		t.Errorf("Open Library should still describe Crime and Punishment: %v", rep.Described)
	}
	var cached int
	st.DB().QueryRowContext(ctx, `SELECT count(*) FROM description_lookup WHERE service = 'google_books'`).Scan(&cached)
	if cached != 0 {
		t.Errorf("%d failed questions were cached; they should be asked again next run", cached)
	}
}

func TestSameBook(t *testing.T) {
	q := Query{Title: "Emile: Or On Education", Author: "Rousseau, Jean-Jacques"}
	if !sameBook(q, "Emile, or On Education", []string{"Jean-Jacques Rousseau"}) {
		t.Error("same book, different punctuation")
	}
	if !sameBook(q, "Emile", []string{"Translator Name", "Jean-Jacques Rousseau"}) {
		t.Error("the author can be any of the book's authors")
	}
	if sameBook(q, "Emile", []string{"Someone Else"}) {
		t.Error("another author's Emile")
	}
	if sameBook(q, "The Social Contract", []string{"Jean-Jacques Rousseau"}) {
		t.Error("another book by the author")
	}
}
