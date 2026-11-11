// Package cli defines recbook's commands. Each command is a thin wrapper:
// it parses flags, opens the store, and calls into the package that does the
// work, so the logic stays testable without a terminal.
package cli

import (
	"context"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/KevinNavarreteParra/recbook/internal/store"
)

// Execute runs the command named on the command line. Ctrl-C or a SIGTERM
// (which is how systemd stops a job) cancels the command's context, so
// long-running commands save their current batch and exit cleanly.
func Execute() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return newRootCmd().ExecuteContext(ctx)
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
	root.CompletionOptions.DisableDefaultCmd = true
	root.PersistentFlags().StringVar(&g.dbPath, "db", envOr("RECBOOK_DB", "data/recbook.duckdb"),
		"DuckDB database file (env RECBOOK_DB)")

	root.AddCommand(
		newIngestCmd(g),
		newCrawlCmd(g),
		newRefreshCmd(g),
		newBuildCmd(g),
		newEnrichCmd(g),
		newBacklogCmd(g),
		newSearchCmd(g),
		newSimilarCmd(g),
		newAuthorCmd(g),
		newEvalCmd(g),
		newServeCmd(g),
		newEmbedCmd(g),
		newNightlyCmd(g),
		newStatusCmd(g),
		newDemoCmd(g),
	)
	return root
}

// openStore opens the database named by --db.
func (g *globals) openStore(ctx context.Context) (*store.Store, error) {
	return store.Open(ctx, g.dbPath)
}

// dataDir is the folder holding the database. Hand-kept files (match
// overrides) and generated ones (the review file) live beside it.
func (g *globals) dataDir() string {
	return filepath.Dir(g.dbPath)
}

// servePath is where the read-only copy for the web app is published:
// data/recbook.duckdb → data/recbook.serve.duckdb.
func (g *globals) servePath() string {
	return strings.TrimSuffix(g.dbPath, ".duckdb") + ".serve.duckdb"
}

// trackerPath is the web app's tracker file, a shelf in JSONL.
func (g *globals) trackerPath() string {
	return filepath.Join(g.dataDir(), "tracker.jsonl")
}

// envOr returns the environment variable key, or fallback when it is unset.
func envOr(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return fallback
}
