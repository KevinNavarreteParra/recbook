// Package cli defines recbook's commands. Each command is a thin wrapper:
// it parses flags, opens the store, and calls into the package that does the
// work, so the logic stays testable without a terminal.
package cli

import (
	"context"
	"os"

	"github.com/spf13/cobra"

	"github.com/KevinNavarreteParra/recbook/internal/store"
)

// Execute runs the command named on the command line.
func Execute() error {
	return newRootCmd().Execute()
}

// globals are the flags every command shares.
type globals struct {
	dbPath string
}

func newRootCmd() *cobra.Command {
	g := &globals{}
	root := &cobra.Command{
		Use:           "recbook",
		Short:         "Find borrowable library audiobooks similar to books you already liked",
		SilenceUsage:  true, // print usage for flag mistakes, not for runtime errors
		SilenceErrors: false,
	}
	root.PersistentFlags().StringVar(&g.dbPath, "db", envOr("RECBOOK_DB", "data/recbook.duckdb"),
		"DuckDB database file (env RECBOOK_DB)")

	root.AddCommand(
		newIngestCmd(g),
	)
	return root
}

// openStore opens the database named by --db.
func (g *globals) openStore(ctx context.Context) (*store.Store, error) {
	return store.Open(ctx, g.dbPath)
}

// envOr returns the environment variable key, or fallback when it is unset.
func envOr(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return fallback
}
