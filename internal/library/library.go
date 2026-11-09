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

// Build rebuilds the borrowable table. It needs resolve to have run.
func Build(ctx context.Context, st *store.Store) error {
	if err := st.RunScript(ctx, borrowableSQL, nil); err != nil {
		return fmt.Errorf("library: %w", err)
	}
	return nil
}
