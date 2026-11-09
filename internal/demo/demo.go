// Package demo is a small, made-up reading history and library, for tests
// and for trying recbook before any real data is loaded (`recbook demo`).
//
// Three shelves, in the JSONL contract format, play the roles of the real
// sources: a reading app (finishes and to-read), an owned-books catalog,
// and a library's loan history. Between them they cover every signal tier,
// a batch-entered date, and each kind of match. The catalog (catalog.go)
// holds audiobooks of most of those books plus a few unread ones. The
// ISBNs are valid but invented.
package demo

import (
	"context"
	"embed"
	"fmt"
	"time"

	"github.com/KevinNavarreteParra/recbook/internal/sources/jsonl"
	"github.com/KevinNavarreteParra/recbook/internal/store"
)

// Library is the demo catalog's library key.
const Library = "demo"

// Shelves are the demo shelf sources, in load order.
var Shelves = []string{"reading_app", "owned_books", "library_loans"}

//go:embed *.jsonl
var shelfFiles embed.FS

// Load ingests the demo shelves and catalog into st.
func Load(ctx context.Context, st *store.Store) error {
	for _, name := range Shelves {
		f, err := shelfFiles.Open(name + ".jsonl")
		if err != nil {
			return err
		}
		shelf, err := jsonl.Read(f)
		f.Close()
		if err != nil {
			return fmt.Errorf("demo %s: %w", name, err)
		}
		if err := st.ReplaceShelf(ctx, name, "demo/"+name+".jsonl", shelf); err != nil {
			return fmt.Errorf("demo %s: %w", name, err)
		}
	}
	titles, avail := Catalog()
	return st.SaveCatalog(ctx, Library, titles, avail, time.Now().UTC())
}
