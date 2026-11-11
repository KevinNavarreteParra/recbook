package cli

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/KevinNavarreteParra/recbook/internal/sources/overdrive"
	"github.com/KevinNavarreteParra/recbook/internal/store"
)

func newCrawlCmd(g *globals) *cobra.Command {
	var (
		library string
		formats []string
		sample  int
		full    bool
	)
	cmd := &cobra.Command{
		Use:   "crawl",
		Short: "Fetch the library's audiobook catalog and current availability",
		Long: "Fetch every audiobook in the library's OverDrive collection, with full\n" +
			"descriptions and current availability. Titles are saved a page at a time,\n" +
			"so an interrupted crawl keeps what it fetched. A collection of 100k titles\n" +
			"takes about half an hour the first time.\n\n" +
			"After that, only new titles are fetched in full: for titles already saved,\n" +
			"the listing gives everything a re-crawl needs (that they're still there,\n" +
			"and their availability), which halves the requests. --full fetches every\n" +
			"record again, to pick up changed descriptions or series.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if library == "" {
				return errors.New("--library is required (the key in libbyapp.com/library/{key})")
			}
			st, err := g.openStore(cmd.Context())
			if err != nil {
				return err
			}
			defer st.Close()

			progress := func(p overdrive.Progress) {
				fmt.Fprintf(cmd.ErrOrStderr(), "\rpartition %d/%d, page %d/%d: %d titles saved   ",
					p.Partition, p.Partitions, p.Page, p.Pages, p.Saved)
			}
			err = crawl(cmd.Context(), st, crawlOptions{library: library, formats: formats, sample: sample, full: full},
				progress, cmd.OutOrStdout())
			fmt.Fprintln(cmd.ErrOrStderr())
			return err
		},
	}
	cmd.Flags().StringVar(&library, "library", envOr("RECBOOK_LIBRARY", ""), "library key (env RECBOOK_LIBRARY)")
	cmd.Flags().StringSliceVar(&formats, "formats", nil,
		"OverDrive format ids to crawl (default audiobook-overdrive,audiobook-mp3)")
	cmd.Flags().IntVar(&sample, "sample", 0, "stop after saving about this many titles, for a quick look")
	cmd.Flags().BoolVar(&full, "full", false, "fetch every title's full record, not only new titles'")
	return cmd
}

type crawlOptions struct {
	library string
	formats []string
	sample  int  // stop after about this many titles; 0 for a full crawl
	full    bool // fetch full records for titles already saved too
}

// crawl fetches the catalog into st. Only a complete crawl is recorded as
// finished, because the dropped-title diff trusts finished crawls alone.
func crawl(ctx context.Context, st *store.Store, opts crawlOptions, progress func(overdrive.Progress), out io.Writer) error {
	started, err := st.StartCrawl(ctx, opts.library)
	if err != nil {
		return err
	}
	var known map[string]bool
	if !opts.full {
		if known, err = st.KnownTitles(ctx, opts.library); err != nil {
			return err
		}
	}
	client := overdrive.NewClient(overdrive.Config{Library: opts.library, Formats: opts.formats})
	errSampled := errors.New("sample complete")
	saved, seen := 0, 0

	save := func(b overdrive.Batch) error {
		if err := st.SaveCatalog(ctx, opts.library, b.Titles, b.Availability, started); err != nil {
			return err
		}
		if err := st.SaveSeen(ctx, opts.library, b.Seen, started); err != nil {
			return err
		}
		saved, seen = saved+len(b.Titles), seen+len(b.Seen)
		if opts.sample > 0 && saved+seen >= opts.sample {
			return errSampled
		}
		return nil
	}

	res, err := client.Crawl(ctx, known, save, progress)
	switch {
	case errors.Is(err, errSampled):
		fmt.Fprintf(out, "%s: saved a sample of %d titles\n", opts.library, saved+seen)
		return nil
	case err != nil:
		return err
	}
	if err := st.FinishCrawl(ctx, opts.library, started, res.Saved); err != nil {
		return err
	}
	fmt.Fprintf(out, "%s: %d titles listed, %d saved (%d new, fetched in full)\n",
		opts.library, res.Listed, res.Saved, saved)
	if res.Warning != "" {
		fmt.Fprintln(out, "warning:", res.Warning)
	}
	return nil
}
