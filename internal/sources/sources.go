// Package sources lists the adapters that turn each data source into
// contract records, one subpackage per source.
//
// A shelf source reads an export file (ReadingList, Libby, LibraryThing,
// Goodreads, ...) into a contract.Shelf. Adding one takes a subpackage with
// a ShelfReader and an entry in ShelfSources below. The catalog source,
// OverDrive, talks to an API, so it has its own package (overdrive) and
// command (`recbook crawl`).
package sources

import (
	"io"
	"slices"

	"github.com/KevinNavarreteParra/recbook/internal/contract"
	"github.com/KevinNavarreteParra/recbook/internal/sources/jsonl"
	"github.com/KevinNavarreteParra/recbook/internal/sources/libby"
	"github.com/KevinNavarreteParra/recbook/internal/sources/librarything"
	"github.com/KevinNavarreteParra/recbook/internal/sources/readinglist"
)

// ShelfReader reads one export file into a shelf. It only translates: it
// should keep every fact the file has and leave all judgment to later
// stages. The store validates the result before saving it.
type ShelfReader func(r io.Reader) (*contract.Shelf, error)

// ShelfSource is a shelf source that `recbook ingest` can load.
type ShelfSource struct {
	Name  string // stored with every record; also the ingest argument
	About string // one line for --help
	Read  ShelfReader
}

// ShelfSources is every shelf source recbook knows.
var ShelfSources = []ShelfSource{
	{Name: "readinglist", About: "Reading List (iOS) CSV export: finished, started, to-read", Read: readinglist.Read},
	{Name: "libby", About: "Libby timeline JSON export: borrows", Read: libby.Read},
	{Name: "librarything", About: "LibraryThing JSON export: books owned", Read: librarything.Read},
	{Name: "jsonl", About: "the contract itself, one JSON record per line (see internal/sources/jsonl)", Read: jsonl.Read},
}

// LookupShelf returns the shelf source with the given name.
func LookupShelf(name string) (ShelfSource, bool) {
	i := slices.IndexFunc(ShelfSources, func(s ShelfSource) bool { return s.Name == name })
	if i < 0 {
		return ShelfSource{}, false
	}
	return ShelfSources[i], true
}
