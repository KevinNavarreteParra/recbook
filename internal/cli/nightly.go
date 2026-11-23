package cli

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"

	"github.com/KevinNavarreteParra/recbook/internal/embed"
	"github.com/KevinNavarreteParra/recbook/internal/graph"
	"github.com/KevinNavarreteParra/recbook/internal/nightly"
	"github.com/KevinNavarreteParra/recbook/internal/recommend"
	"github.com/KevinNavarreteParra/recbook/internal/sources/overdrive"
	"github.com/KevinNavarreteParra/recbook/internal/store"
)

func newNightlyCmd(g *globals) *cobra.Command {
	var (
		window     string
		library    string
		crawlEvery time.Duration
		emb        embedOptions
	)
	cmd := &cobra.Command{
		Use:   "nightly",
		Short: "The scheduled job: refresh the catalog, rebuild, embed, within a night window",
		Long: "Run the nightly upkeep, but only inside --window, stopping when it closes:\n\n" +
			"  1. crawl the catalog again, if the last full crawl is older than --crawl-every;\n" +
			"     otherwise refresh availability for the backlog alone (`recbook refresh`)\n" +
			"  2. look up descriptions for finished books that have none (`recbook enrich`)\n" +
			"  3. rebuild the derived tables, folding in the web app's tracker marks\n" +
			"  4. embed whatever isn't embedded yet, on the GPU, if it's free\n" +
			"  5. if it embedded anything, find each book's nearest books again for the\n" +
			"     graph view (`recbook neighbours`, on the CPU)\n" +
			"  6. publish the result for the web app\n\n" +
			"A failed refresh or enrich is reported and the night goes on.\n" +
			"Every step saves as it goes, so whatever a night doesn't finish, the next\n" +
			"night continues. Outside the window it does nothing. A systemd timer runs\n" +
			"it (deploy/systemd).",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			w, err := nightly.ParseWindow(window)
			if err != nil {
				return err
			}
			end, inside := w.End(time.Now())
			if !inside {
				fmt.Fprintf(out, "nightly: outside the %s window; nothing to do\n", w)
				return nil
			}
			ctx, cancel := context.WithDeadline(cmd.Context(), end)
			defer cancel()
			fmt.Fprintf(out, "nightly: running until %s\n", end.Format("15:04"))

			st, err := g.openStore(ctx)
			if err != nil {
				return err
			}
			defer st.Close()

			// A service being down shouldn't cost the night its build and embed.
			goOn := func(name string, err error) error {
				if err != nil && ctx.Err() == nil {
					fmt.Fprintf(out, "nightly: %s failed, going on: %v\n", name, err)
				}
				return nil
			}
			crawled, embedded := false, 0
			steps := []struct {
				name string
				run  func() error
			}{
				{"crawl", func() (err error) {
					crawled, err = crawlIfDue(ctx, st, library, crawlEvery, out)
					return err
				}},
				{"refresh", func() error {
					if crawled {
						return nil // the crawl just read every title's availability
					}
					return goOn("refresh", refreshWatched(ctx, st, library, out))
				}},
				{"enrich", func() error { return goOn("enrich", enrichSeeds(ctx, st, out)) }},
				{"build", func() error { return build(ctx, st, g, io.Discard) }},
				{"embed", func() error {
					rep, err := embedWorks(ctx, st, g.languages(), emb, out)
					embedded = rep.Embedded
					return err
				}},
				{"neighbours", func() error { return neighboursIfDue(ctx, st, g, embedded, out) }},
			}
			// Whatever happens, publish what was done for the web app. The
			// window may have closed, so this gets a little time of its own.
			defer func() {
				publish, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
				defer cancel()
				if err := st.Snapshot(publish, g.servePath()); err != nil {
					fmt.Fprintln(out, "nightly: publishing for the web app:", err)
				}
			}()
			for _, step := range steps {
				err := step.run()
				if ctx.Err() != nil {
					fmt.Fprintf(out, "nightly: window closed during %s; the next night continues\n", step.name)
					return nil
				}
				if err != nil {
					return fmt.Errorf("nightly %s: %w", step.name, err)
				}
			}
			fmt.Fprintln(out, "nightly: done")
			return nil
		},
	}
	cmd.Flags().StringVar(&window, "window", "02:00-06:00", "hours the job may run, HH:MM-HH:MM in local time")
	cmd.Flags().StringVar(&library, "library", envOr("RECBOOK_LIBRARY", ""), "library key (env RECBOOK_LIBRARY)")
	cmd.Flags().DurationVar(&crawlEvery, "crawl-every", 7*24*time.Hour, "how old the last full crawl may get")
	addEmbedFlags(cmd, &emb)
	return cmd
}

// neighboursIfDue rebuilds the graph's nearest-text edges, and the for-you
// list that may use them, when the night embedded anything, or when they
// were never built.
func neighboursIfDue(ctx context.Context, st *store.Store, g *globals, embedded int, out io.Writer) error {
	var built bool
	err := st.DB().QueryRowContext(ctx,
		`SELECT count(*) > 0 FROM duckdb_tables() WHERE table_name = 'text_neighbour'`).Scan(&built)
	if err != nil {
		return err
	}
	if built && embedded == 0 {
		return nil
	}
	fmt.Fprintln(out, "nightly: finding each book's nearest books")
	if err := graph.BuildNeighbours(ctx, st, embed.DefaultModel); err != nil {
		return err
	}
	return recommend.BuildForYou(ctx, st, g.languages())
}

// crawlIfDue runs a crawl when the last finished one is older than every
// (or there is none), and reports whether it crawled.
func crawlIfDue(ctx context.Context, st *store.Store, library string, every time.Duration, out io.Writer) (bool, error) {
	if library == "" {
		return false, errors.New("--library is required")
	}
	var last sql.NullTime
	err := st.DB().QueryRowContext(ctx,
		`SELECT max(finished_at) FROM crawl WHERE library = ? AND finished_at IS NOT NULL`, library).Scan(&last)
	if err != nil {
		return false, err
	}
	if last.Valid && time.Since(last.Time) < every {
		fmt.Fprintf(out, "nightly: catalog crawled %s ago; not due\n", time.Since(last.Time).Truncate(time.Minute))
		return false, nil
	}
	fmt.Fprintln(out, "nightly: crawling the catalog")
	progress := func(p overdrive.Progress) {
		if p.Page == p.Pages {
			fmt.Fprintf(out, "nightly: crawled partition %d/%d, %d titles so far\n", p.Partition, p.Partitions, p.Saved)
		}
	}
	return true, crawl(ctx, st, crawlOptions{library: library}, progress, out)
}
