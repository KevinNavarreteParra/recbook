package authors

import (
	"context"
	"math"
	"slices"
	"testing"
	"time"

	"github.com/KevinNavarreteParra/recbook/internal/contract"
	"github.com/KevinNavarreteParra/recbook/internal/demo"
	"github.com/KevinNavarreteParra/recbook/internal/embed"
	"github.com/KevinNavarreteParra/recbook/internal/library"
	"github.com/KevinNavarreteParra/recbook/internal/resolve"
	"github.com/KevinNavarreteParra/recbook/internal/signals"
	"github.com/KevinNavarreteParra/recbook/internal/store"
)

// extra are catalog titles added to the demo catalog, each testing one
// thing about authors. The blurbs use demo.ThemeEmbedder's theme words.
var extra = []struct {
	id, title, blurb string
	creators         []contract.Creator
}{
	// Another spy author, so le Carré has a neighbour.
	{"9901", "The Polish Officer", "A spy in occupied Europe runs agents out of Berlin.",
		[]contract.Creator{{Name: "Alan Furst", Role: contract.Author}}},
	// Written with le Carré: in "more by le Carré", never a reason to call
	// Fleming similar to him.
	{"9902", "A Joint Effort", "Two spy chiefs swap agents in Berlin.",
		[]contract.Creator{{Name: "John le Carré", Role: contract.Author}, {Name: "Ian Fleming", Role: contract.Author}}},
	// A second spelling of Dostoevsky, with a book like his.
	{"9903", "Demons", "Brothers in provincial Russia lose their faith.",
		[]contract.Creator{{Name: "Fyodor Dostoyevsky", Role: contract.Author}}},
	// Nobody: a placeholder author and a title credited only to narrators.
	{"9904", "Spy Stories", "Spy agents in Berlin, from the Circus.",
		[]contract.Creator{{Name: "Anonymous", Role: contract.Author}}},
	{"9905", "Smiley Interview", "A spy talks about Berlin.",
		[]contract.Creator{{Name: "Professional Book Nerds", Role: contract.Narrator}}},
}

// setup loads the demo shelves and catalog plus extra, runs the build
// steps authors needs, and embeds with demo.ThemeEmbedder.
func setup(t *testing.T) *store.Store {
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
	for i := range titles {
		titles[i].Description = demo.Blurbs[titles[i].ID]
	}
	for _, e := range extra {
		titles = append(titles, contract.CatalogTitle{
			ID: e.id, Title: e.title, Description: e.blurb, Creators: e.creators,
			Format: contract.Audiobook, Language: "en", DurationMin: 600,
		})
		avail = append(avail, contract.Availability{TitleID: e.id, Available: true, CopiesOwned: 1, CopiesAvailable: 1})
	}
	if err := st.SaveCatalog(ctx, demo.Library, titles, avail, time.Now()); err != nil {
		t.Fatal(err)
	}

	if _, err := resolve.Run(ctx, st, resolve.Options{}); err != nil {
		t.Fatal(err)
	}
	if err := signals.Run(ctx, st, signals.Options{}); err != nil {
		t.Fatal(err)
	}
	if err := library.Build(ctx, st); err != nil {
		t.Fatal(err)
	}
	if _, err := embed.Run(ctx, st, demo.ThemeEmbedder{}, embed.Options{}); err != nil {
		t.Fatal(err)
	}
	if err := Build(ctx, st); err != nil {
		t.Fatal(err)
	}
	return st
}

func find(t *testing.T, st *store.Store, name string) Author {
	t.Helper()
	found, err := Find(context.Background(), st, name)
	if err != nil || len(found) == 0 {
		t.Fatalf("Find(%q) = %v, %v", name, found, err)
	}
	return found[0]
}

func titles(books []Book) []string {
	var out []string
	for _, b := range books {
		out = append(out, b.Title)
	}
	return out
}

func TestIdentity(t *testing.T) {
	st := setup(t)

	// The catalog says "John le Carré", the owned-books shelf "le Carré,
	// John": one person, named the catalog's way.
	got := find(t, st, "LE CARRE")
	want := Author{ID: "john le carre", Name: "John le Carré", Works: 4, InCatalog: 4, OnShelf: 2, Finished: 1}
	if got != want {
		t.Errorf("le Carré = %+v, want %+v", got, want)
	}

	// Two of Dostoevsky's books are only on my shelf, as "Dostoevsky,
	// Fyodor"; they still count as his.
	got = find(t, st, "Dostoevsky, Fyodor")
	want = Author{ID: "fyodor dostoevsky", Name: "Fyodor Dostoevsky", Works: 3, InCatalog: 1, OnShelf: 3, Finished: 1}
	if got != want {
		t.Errorf("Dostoevsky = %+v, want %+v", got, want)
	}

	for _, nobody := range []string{"anonymous", "professional book nerds", "michael jayston"} {
		if found, _ := Find(context.Background(), st, nobody); len(found) > 0 {
			t.Errorf("%q is a placeholder or a narrator, not an author: %+v", nobody, found)
		}
	}
}

// mean averages the vectors a query returns.
func mean(t *testing.T, st *store.Store, query string) ([]float64, int) {
	t.Helper()
	rows, err := st.DB().QueryContext(context.Background(), query)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var sum []float64
	n := 0
	for rows.Next() {
		var v []any
		if err := rows.Scan(&v); err != nil {
			t.Fatal(err)
		}
		if sum == nil {
			sum = make([]float64, len(v))
		}
		for i, x := range v {
			sum[i] += float64(x.(float32))
		}
		n++
	}
	for i := range sum {
		sum[i] /= float64(n)
	}
	return sum, n
}

func TestVectorIsTheCentredMeanAtUnitLength(t *testing.T) {
	ctx := context.Background()
	st := setup(t)

	// le Carré's mean less every work's mean, computed here.
	all, _ := mean(t, st, `
		SELECT e.vector FROM (SELECT DISTINCT text_hash FROM work_text) t
		JOIN embedding e USING (text_hash)`)
	his, n := mean(t, st, `
		SELECT e.vector FROM author_work aw
		JOIN work_text t USING (work_id)
		JOIN embedding e ON e.text_hash = t.text_hash
		WHERE aw.author_id = 'john le carre'`)
	var norm float64
	for i := range his {
		his[i] -= all[i]
		norm += his[i] * his[i]
	}

	var works int
	var vector []any
	err := st.DB().QueryRowContext(ctx,
		`SELECT works, vector FROM author_vector WHERE author_id = 'john le carre'`).Scan(&works, &vector)
	if err != nil {
		t.Fatal(err)
	}
	if works != n || n != 4 {
		t.Errorf("works = %d, want %d (and 4)", works, n)
	}
	for i, x := range vector {
		if want := his[i] / math.Sqrt(norm); math.Abs(float64(x.(float32))-want) > 1e-5 {
			t.Errorf("vector[%d] = %v, want %v", i, x, want)
		}
	}
}

func TestLookup(t *testing.T) {
	ctx := context.Background()
	st := setup(t)
	leCarre := find(t, st, "le carre")

	res, err := Lookup(ctx, st, leCarre.ID, Options{MaxWaitDays: -1, MinWorks: 1})
	if err != nil {
		t.Fatal(err)
	}
	if res.Embedded != 4 {
		t.Errorf("Embedded = %d, want 4", res.Embedded)
	}

	// More by him: not Tinker Tailor, which I finished.
	if got, want := titles(res.More), []string{"A Joint Effort", "Smiley's People", "The Spy Who Came in from the Cold"}; !slices.Equal(got, want) {
		t.Errorf("more by le Carré = %q, want %q", got, want)
	}

	// The nearest author is the other spy writer, shown with his spy novel.
	// Fleming wrote nothing without le Carré, so he isn't there at all.
	if len(res.Similar) == 0 || res.Similar[0].Name != "Alan Furst" ||
		!slices.Equal(titles(res.Similar[0].Books), []string{"The Polish Officer"}) {
		t.Fatalf("similar = %+v", res.Similar)
	}
	for i, n := range res.Similar {
		if n.ID == "ian fleming" || n.ID == leCarre.ID {
			t.Errorf("%s shouldn't be similar to le Carré", n.Name)
		}
		if i > 0 && n.Similarity > res.Similar[i-1].Similarity {
			t.Errorf("similar authors out of order: %+v", res.Similar)
		}
		if len(n.Books) == 0 || len(n.Books) > 3 {
			t.Errorf("%s has %d books, want 1 to 3", n.Name, len(n.Books))
		}
		for _, b := range n.Books {
			if b.Author == "John le Carré" {
				t.Errorf("%q is his own book, not a similar author's", b.Title)
			}
		}
	}

	// With the default minimum, authors of one or two books don't count.
	res, err = Lookup(ctx, st, leCarre.ID, Options{MaxWaitDays: -1})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Similar) != 0 {
		t.Errorf("no other demo author has 3 embedded books: %+v", res.Similar)
	}
}

func TestLookupSetsAsideOtherSpellings(t *testing.T) {
	st := setup(t)
	res, err := Lookup(context.Background(), st, "fyodor dostoevsky", Options{MaxWaitDays: -1, MinWorks: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Spellings) != 1 || res.Spellings[0].Name != "Fyodor Dostoyevsky" {
		t.Errorf("spellings = %+v", res.Spellings)
	}
	for _, n := range res.Similar {
		if n.ID == "fyodor dostoyevsky" {
			t.Error("another spelling of him isn't a similar author")
		}
	}
}
