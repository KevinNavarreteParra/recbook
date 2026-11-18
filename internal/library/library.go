// Package library answers "can I borrow it?" for each work: which of the
// library's recordings to offer, and how available it is. Backlog, search
// and recommendations all read the borrowable table it builds.
package library

import (
	"context"
	_ "embed"
	"fmt"

	"github.com/KevinNavarreteParra/recbook/internal/store"
)

//go:embed borrowable.sql
var borrowableSQL string

// Build rebuilds the borrowable table, offering each work in the first of
// languages (ISO 639-1 codes, "en") that the library has it in; with no
// languages, any. It needs resolve to have run.
func Build(ctx context.Context, st *store.Store, languages []string) error {
	if err := st.RunScript(ctx, borrowableSQL, store.Vars{"languages": languages}); err != nil {
		return fmt.Errorf("library: %w", err)
	}
	return nil
}
