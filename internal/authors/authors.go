// Package authors is the author view and author queries (spec §6 and §8):
// who wrote what, each author's typical territory in the content view, and
// for one author, more of their books and the authors nearest them.
//
// An author is a person, not a spelling. Sources write the same name
// several ways ("John le Carré", "John Le Carre", "le Carré, John"), so
// every credit is reduced to a textkey.Person key and the key is the
// author's id. Credits come from the catalog's creator roles where the
// library has the book, so narrators are never mistaken for authors.
//
// Build rebuilds these tables, in order:
//
//	author_credit  every credited author name of every work   (credits.sql)
//	author_name    the person each name stands for            (Go: textkey.Person)
//	author_work    each author's works                        (authors.sql)
//	author         one row per author, with a display name    (authors.sql)
//	author_vector  each author's mean content vector          (vectors.sql)
//
// author_vector depends on the embeddings as well as the works, and the
// nightly job embeds after it builds, so embedding refreshes it too
// (BuildVectors).
package authors

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"slices"

	"github.com/KevinNavarreteParra/recbook/internal/embed"
	"github.com/KevinNavarreteParra/recbook/internal/store"
	"github.com/KevinNavarreteParra/recbook/internal/textkey"
)

var (
	//go:embed credits.sql
	creditsSQL string
	//go:embed authors.sql
	authorsSQL string
	//go:embed vectors.sql
	vectorsSQL string
	//go:embed find.sql
	findSQL string
	//go:embed get.sql
	getSQL string
	//go:embed more.sql
	moreSQL string
	//go:embed similar.sql
	similarSQL string
)

// Build rebuilds the author tables. It needs resolve and signals to have
// run, and the work texts to have been written (embed.WriteTexts).
func Build(ctx context.Context, st *store.Store) error {
	if err := st.RunScript(ctx, creditsSQL, nil); err != nil {
		return fmt.Errorf("authors: credits: %w", err)
	}
	if err := writeNames(ctx, st); err != nil {
		return fmt.Errorf("authors: names: %w", err)
	}
	if err := st.RunScript(ctx, authorsSQL, nil); err != nil {
		return fmt.Errorf("authors: %w", err)
	}
	return BuildVectors(ctx, st)
}

// BuildVectors rebuilds author_vector from the embeddings there are now.
// Embedding runs it after adding vectors, so authors don't wait a night for
// the next build to see their new books.
func BuildVectors(ctx context.Context, st *store.Store) error {
	if err := st.RunScript(ctx, vectorsSQL, nil); err != nil {
		return fmt.Errorf("authors: vectors: %w", err)
	}
	return nil
}

// writeNames keys every credited name in Go, where the rules are readable
// and unit-tested (textkey.Person), and loads the keys into author_name.
func writeNames(ctx context.Context, st *store.Store) error {
	rows, err := st.DB().QueryContext(ctx, `SELECT DISTINCT name FROM author_credit`)
	if err != nil {
		return err
	}
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return err
		}
		names = append(names, name)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	return st.AppendRows(ctx, "author_name", func(add func(...any) error) error {
		for _, name := range names {
			if key := textkey.Person(name); key != "" {
				if err := add(name, key); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// Author is one person and how much of theirs there is.
type Author struct {
	ID        string // their textkey.Person key, "john le carre"
	Name      string // for display, "John le Carré"
	Works     int    // works credited to them, on my shelf or in the catalog
	InCatalog int    // ...of which the library has
	OnShelf   int    // ...of which are on my shelf
	Finished  int    // ...of which I finished
}

// Find returns up to five authors whose name contains every word of name:
// the one with exactly that name first, if there is one, then those I've
// read.
func Find(ctx context.Context, st *store.Store, name string) ([]Author, error) {
	key := textkey.Person(name)
	if key == "" {
		return nil, nil
	}
	rows, err := st.DB().QueryContext(ctx, findSQL, sql.Named("q", key))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Author
	for rows.Next() {
		var a Author
		if err := rows.Scan(&a.ID, &a.Name, &a.Works, &a.InCatalog, &a.OnShelf, &a.Finished); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// DefaultMinWorks is how many embedded works a similar author needs. Six
// in ten authors in the real catalog have one, and with no minimum the
// nearest of them fill the list: for Plato, writers of a single book about
// Plato. From three on, the list is authors with a body of work like the
// queried author's.
const DefaultMinWorks = 3

// Options are an author query.
type Options struct {
	Model        string // embedding model; default embed.DefaultModel
	AvailableNow bool   // only what can be borrowed right now
	MaxWaitDays  int    // only what's available within this many days; <0 for no limit
	IncludeRead  bool   // also show books I've finished
	MinWorks     int    // similar authors need this many embedded works; 0 means DefaultMinWorks
	Authors      int    // how many similar authors; 0 means 10
	PerAuthor    int    // books shown for each similar author; 0 means 3
	Limit        int    // books in "more by"; 0 means 50
}

// Book is one borrowable book, in the recording the library would lend.
type Book struct {
	WorkID      int64
	TitleID     string // the recording's catalog id
	Title       string
	Author      string // as the recording credits them
	Narrator    string
	DurationMin sql.NullInt64
	Abridged    bool
	Available   bool
	WaitDays    sql.NullInt64
	Holds       sql.NullInt64
	URL         string
	Tier        string  // my relation to it, if any
	Similarity  float64 // for a similar author's book: its cosine with the queried author
}

// Neighbour is a similar author and their books nearest the queried one.
type Neighbour struct {
	Author
	Embedded   int     // works their vector averages
	Similarity float64 // cosine of the two authors' vectors
	Books      []Book
}

// Result is the answer to an author query: the two sections of spec §8.
type Result struct {
	Author   Author
	Embedded int         // works the author's vector averages; 0 means no similar authors
	More     []Book      // more by this author, most borrowable first
	Similar  []Neighbour // nearest authors first

	// Spellings are near authors whose name differs from this one's by a
	// letter or two in one word ("Fyodor Dostoyevsky", "Fyodor Dostoevsky"):
	// most likely the same person, credited differently. They're kept out
	// of Similar and listed here instead, since merging them for sure
	// would take more than a name.
	Spellings []Author
}

// Lookup answers an author query for the author with this id (see Find).
// It needs `recbook build` to have run.
func Lookup(ctx context.Context, st *store.Store, id string, opts Options) (*Result, error) {
	if opts.Model == "" {
		opts.Model = embed.DefaultModel
	}
	if opts.MinWorks == 0 {
		opts.MinWorks = DefaultMinWorks
	}
	if opts.Authors == 0 {
		opts.Authors = 10
	}
	if opts.PerAuthor == 0 {
		opts.PerAuthor = 3
	}
	if opts.Limit == 0 {
		opts.Limit = 50
	}

	res := &Result{}
	a := &res.Author
	err := st.DB().QueryRowContext(ctx, getSQL, sql.Named("author", id), sql.Named("model", opts.Model)).
		Scan(&a.ID, &a.Name, &a.Works, &a.InCatalog, &a.OnShelf, &a.Finished, &res.Embedded)
	if err != nil {
		return nil, fmt.Errorf("author %q: %w", id, err)
	}

	filters := []any{
		sql.Named("author", id),
		sql.Named("include_read", opts.IncludeRead),
		sql.Named("available_now", opts.AvailableNow),
		sql.Named("max_wait", opts.MaxWaitDays),
	}
	res.More, err = moreBy(ctx, st.DB(), slices.Concat(filters, []any{sql.Named("limit", opts.Limit)}))
	if err != nil {
		return nil, fmt.Errorf("more by %s: %w", a.Name, err)
	}
	if res.Embedded == 0 {
		return res, nil
	}

	// Ask for a few extra neighbours, in case some are other spellings.
	near, err := similar(ctx, st.DB(), slices.Concat(filters, []any{
		sql.Named("model", opts.Model),
		sql.Named("min_works", opts.MinWorks),
		sql.Named("per_author", opts.PerAuthor),
		sql.Named("authors", opts.Authors+extraNeighbours),
		sql.Named("pool", poolFactor*(opts.Authors+extraNeighbours)),
	}))
	if err != nil {
		return nil, fmt.Errorf("authors like %s: %w", a.Name, err)
	}
	for _, n := range near {
		switch {
		case textkey.LikelySamePerson(id, n.ID):
			res.Spellings = append(res.Spellings, n.Author)
		case len(res.Similar) < opts.Authors:
			res.Similar = append(res.Similar, n)
		}
	}
	return res, nil
}

// extraNeighbours is how many more similar authors Lookup asks for than it
// shows, to make up for any that turn out to be other spellings.
const extraNeighbours = 3

// poolFactor is how many times more authors similar.sql looks at than it
// returns, to make up for those with no books that pass the filters.
const poolFactor = 5

func moreBy(ctx context.Context, db *sql.DB, args []any) ([]Book, error) {
	rows, err := db.QueryContext(ctx, moreSQL, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Book
	for rows.Next() {
		var b Book
		if err := rows.Scan(b.fields()...); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// similar reads similar.sql's rows, one per book, into one Neighbour per
// author. The rows come grouped by author, nearest author first.
func similar(ctx context.Context, db *sql.DB, args []any) ([]Neighbour, error) {
	rows, err := db.QueryContext(ctx, similarSQL, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Neighbour
	for rows.Next() {
		var n Neighbour
		var b Book
		dest := slices.Concat(
			[]any{&n.ID, &n.Name, &n.Works, &n.InCatalog, &n.OnShelf, &n.Finished, &n.Embedded, &n.Similarity},
			b.fields(),
			[]any{&b.Similarity})
		if err := rows.Scan(dest...); err != nil {
			return nil, err
		}
		if len(out) == 0 || out[len(out)-1].ID != n.ID {
			out = append(out, n)
		}
		last := &out[len(out)-1]
		last.Books = append(last.Books, b)
	}
	return out, rows.Err()
}

// fields are where a book's columns scan to, in the order more.sql and
// similar.sql select them.
func (b *Book) fields() []any {
	return []any{&b.WorkID, &b.TitleID, &b.Title, &b.Author, &b.Narrator, &b.DurationMin, &b.Abridged,
		&b.Available, &b.WaitDays, &b.Holds, &b.URL, &b.Tier}
}
