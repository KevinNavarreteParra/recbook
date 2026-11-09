package cli

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/KevinNavarreteParra/recbook/internal/sources/overdrive"
)

func newCrawlCmd(g *globals) *cobra.Command {
	var (
		library string
		formats []string
		sample  int
	)
	cmd := &cobra.Command{
		Use:   "crawl",
		Short: "Fetch the library's audiobook catalog and current availability",
		Long: "Fetch every audiobook in the library's OverDrive collection, with full\n" +
			"descriptions and current availability. Titles are saved a page at a time,\n" +
			"so an interrupted crawl keeps what it fetched. At two requests a second, a\n" +
			"collection of 100k titles takes about half an hour.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if library == "" {
				return errors.New("--library is required (the key in libbyapp.com/library/{key})")
			}
			ctx := cmd.Context()
			st, err := g.openStore(ctx)
			if err != nil {
				return err
			}
			defer st.Close()

			started, err := st.StartCrawl(ctx, library)
			if err != nil {
				return err
			}
			client := overdrive.NewClient(overdrive.Config{Library: library, Formats: formats})
			errSampled := errors.New("sample complete")
			saved := 0

			save := func(b overdrive.Batch) error {
				if err := st.SaveCatalog(ctx, library, b.Titles, b.Availability, started); err != nil {
					return err
				}
				saved += len(b.Titles)
				if sample > 0 && saved >= sample {
					return errSampled
				}
				return nil
			}
			progress := func(p overdrive.Progress) {
				fmt.Fprintf(cmd.ErrOrStderr(), "\rpartition %d/%d, page %d/%d: %d titles saved   ",
					p.Partition, p.Partitions, p.Page, p.Pages, p.Saved)
			}

			res, err := client.Crawl(ctx, save, progress)
			fmt.Fprintln(cmd.ErrOrStderr())
			switch {
			case errors.Is(err, errSampled):
				// A sample isn't a full crawl, so it isn't marked finished:
				// the dropped-title diff only trusts complete crawls.
				fmt.Fprintf(cmd.OutOrStdout(), "%s: saved a sample of %d titles\n", library, saved)
				return nil
			case err != nil:
				return err
			}
			if err := st.FinishCrawl(ctx, library, started, res.Saved); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s: %d titles listed, %d saved\n", library, res.Listed, res.Saved)
			if res.Warning != "" {
				fmt.Fprintln(cmd.ErrOrStderr(), "warning:", res.Warning)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&library, "library", envOr("RECBOOK_LIBRARY", ""), "library key (env RECBOOK_LIBRARY)")
	cmd.Flags().StringSliceVar(&formats, "formats", nil,
		"OverDrive format ids to crawl (default audiobook-overdrive,audiobook-mp3)")
	cmd.Flags().IntVar(&sample, "sample", 0, "stop after saving about this many titles, for a quick look")
	return cmd
}
