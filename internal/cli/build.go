package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/KevinNavarreteParra/recbook/internal/authors"
	"github.com/KevinNavarreteParra/recbook/internal/embed"
	"github.com/KevinNavarreteParra/recbook/internal/library"
	"github.com/KevinNavarreteParra/recbook/internal/resolve"
	"github.com/KevinNavarreteParra/recbook/internal/search"
	"github.com/KevinNavarreteParra/recbook/internal/signals"
	"github.com/KevinNavarreteParra/recbook/internal/sources/jsonl"
	"github.com/KevinNavarreteParra/recbook/internal/store"
)

func newBuildCmd(g *globals) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "build",
		Short: "Rebuild works, labels, tiers and the search index from everything ingested",
		Long: "Rebuild every derived table from the contract tables: match records into\n" +
			"works, derive dates, labels and tiers, pick the recording to offer for each\n" +
			"work, and index everything for search. Safe to rerun; it never changes\n" +
			"ingested data.\n\n" +
			"Reads <data dir>/match_overrides.csv if present and writes the matches\n" +
			"worth checking to <data dir>/review/matches.csv, where <data dir> is the\n" +
			"folder holding the database.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := g.openStore(cmd.Context())
			if err != nil {
				return err
			}
			defer st.Close()
			return build(cmd.Context(), st, g, cmd.OutOrStdout())
		},
	}
	return cmd
}

// build folds in the web app's tracker marks, runs every derived-table
// stage in order, publishes the copy the web app reads, and prints what it
// found.
func build(ctx context.Context, st *store.Store, g *globals, out io.Writer) error {
	dataDir := g.dataDir()
	if err := ingestTracker(ctx, st, g.trackerPath()); err != nil {
		return err
	}
	reviewFile := filepath.Join(dataDir, "review", "matches.csv")
	rep, err := resolve.Run(ctx, st, resolve.Options{
		OverridesFile: filepath.Join(dataDir, "match_overrides.csv"),
		ReviewFile:    reviewFile,
	})
	if err != nil {
		return err
	}
	if err := signals.Run(ctx, st, signals.Options{}); err != nil {
		return err
	}
	if err := library.Build(ctx, st); err != nil {
		return err
	}
	if err := search.Build(ctx, st); err != nil {
		return err
	}
	// Works were renumbered, so the embedded texts must follow them.
	if _, err := embed.WriteTexts(ctx, st); err != nil {
		return err
	}
	if err := authors.Build(ctx, st); err != nil {
		return err
	}
	if err := st.Snapshot(ctx, g.servePath()); err != nil {
		return fmt.Errorf("publishing for the web app: %w", err)
	}

	methods := make([]string, 0, len(rep.EdgesByMethod))
	for m, n := range rep.EdgesByMethod {
		methods = append(methods, fmt.Sprintf("%s %d", m, n))
	}
	slices.Sort(methods)

	fmt.Fprintf(out, "records  %d shelf, %d catalog\n", rep.ShelfRecords, rep.CatalogRecords)
	fmt.Fprintf(out, "matches  %s\n", strings.Join(methods, ", "))
	if rep.OverridesApplied > 0 {
		fmt.Fprintf(out, "         plus %d overrides\n", rep.OverridesApplied)
	}
	fmt.Fprintf(out, "works    %d, of which %d are on my shelf and %d of those in the catalog\n",
		rep.Works, rep.ShelfWorks, rep.ShelfWorksInCatalog)
	if rep.ReviewRows > 0 {
		fmt.Fprintf(out, "review   %d matches to check in %s\n", rep.ReviewRows, reviewFile)
	}
	fmt.Fprintln(out)
	return printSignals(ctx, st, out)
}

func printSignals(ctx context.Context, st *store.Store, out io.Writer) error {
	tiers, err := signals.Tiers(ctx, st)
	if err != nil {
		return err
	}
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "tier\tworks\tweight\tmeaning")
	for _, t := range tiers {
		fmt.Fprintf(tw, "%s\t%d\t%.2f\t%s\n", t.Tier, t.Works, t.SeedWeight, t.Meaning)
	}
	tw.Flush()

	loans, err := signals.Loans(ctx, st)
	if err != nil || loans.Borrowed == 0 {
		return err
	}
	fmt.Fprintf(out, "\nlibrary loans: %d works borrowed, %d with a known outcome", loans.Borrowed, loans.Labeled)
	if loans.Labeled > 0 {
		fmt.Fprintf(out, ", %d finished (%.0f%%)", loans.Finished, 100*float64(loans.Finished)/float64(loans.Labeled))
	}
	fmt.Fprintln(out)
	return nil
}

// ingestTracker loads the web app's tracker file, if there is one, as the
// shelf source "tracker". It's the contract's JSONL, like any other source.
func ingestTracker(ctx context.Context, st *store.Store, path string) error {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	shelf, err := jsonl.Read(f)
	if err != nil {
		return fmt.Errorf("tracker %s: %w", path, err)
	}
	return st.ReplaceShelf(ctx, "tracker", path, shelf)
}
