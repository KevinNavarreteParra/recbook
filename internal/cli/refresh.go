package cli

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/KevinNavarreteParra/recbook/internal/backlog"
	"github.com/KevinNavarreteParra/recbook/internal/sources/overdrive"
	"github.com/KevinNavarreteParra/recbook/internal/store"
)

func newRefreshCmd(g *globals) *cobra.Command {
	var library string
	cmd := &cobra.Command{
		Use:   "refresh",
		Short: "Refresh availability for the books on my backlog, between crawls",
		Long: "Fetch current availability for every recording of a backlog book (owned or\n" +
			"wanted, not finished), a hundred to a request: a couple of requests rather\n" +
			"than a crawl's thousand. The nightly job runs this on the nights it doesn't\n" +
			"crawl, so the backlog is never more than a day out of date. Run `recbook\n" +
			"build` afterwards to use it.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := g.openStore(cmd.Context())
			if err != nil {
				return err
			}
			defer st.Close()
			return refreshWatched(cmd.Context(), st, library, cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&library, "library", envOr("RECBOOK_LIBRARY", ""), "library key (env RECBOOK_LIBRARY)")
	return cmd
}

// refreshWatched saves fresh availability for the backlog's recordings.
func refreshWatched(ctx context.Context, st *store.Store, library string, out io.Writer) error {
	if library == "" {
		return errors.New("--library is required (the key in libbyapp.com/library/{key})")
	}
	ids, err := backlog.Watch(ctx, st, library)
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		fmt.Fprintln(out, "refresh: no backlog books in the catalog; nothing to check")
		return nil
	}
	client := overdrive.NewClient(overdrive.Config{Library: library})
	avail, fetchErr := client.Availability(ctx, ids)
	// Keep whatever came back before an error.
	if err := st.SaveAvailability(ctx, library, avail); err != nil {
		return err
	}
	if fetchErr != nil {
		return fetchErr
	}
	requests := (len(ids) + overdrive.BulkMax - 1) / overdrive.BulkMax
	fmt.Fprintf(out, "refresh: availability for %d of %d backlog recordings, in %d requests\n",
		len(avail), len(ids), requests)
	return nil
}
