package cli

import (
	"database/sql"
	"fmt"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
)

func newStatusCmd(g *globals) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show what has been ingested, crawled and built",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			st, err := g.openStore(ctx)
			if err != nil {
				return err
			}
			defer st.Close()
			db, out := st.DB(), cmd.OutOrStdout()

			fmt.Fprintf(out, "database  %s\n\n", g.dbPath)
			tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "shelf source\titems\tevents\tfile\tingested")
			rows, err := db.QueryContext(ctx, `SELECT source, items, events, file, imported_at FROM shelf_import ORDER BY source`)
			if err != nil {
				return err
			}
			for rows.Next() {
				var source, file string
				var items, events int
				var at time.Time
				if err := rows.Scan(&source, &items, &events, &file, &at); err != nil {
					rows.Close()
					return err
				}
				fmt.Fprintf(tw, "%s\t%d\t%d\t%s\t%s\n", source, items, events, file, at.Local().Format(time.DateTime))
			}
			rows.Close()
			tw.Flush()

			fmt.Fprintln(out)
			tw = tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "library\ttitles\tlast full crawl")
			rows, err = db.QueryContext(ctx, `
				SELECT t.library, count(*),
				       (SELECT max(finished_at) FROM crawl c WHERE c.library = t.library)
				FROM catalog_title t GROUP BY t.library ORDER BY t.library`)
			if err != nil {
				return err
			}
			for rows.Next() {
				var library string
				var titles int
				var finished sql.NullTime
				if err := rows.Scan(&library, &titles, &finished); err != nil {
					rows.Close()
					return err
				}
				last := "none (sample or demo only)"
				if finished.Valid {
					last = finished.Time.Local().Format(time.DateTime)
				}
				fmt.Fprintf(tw, "%s\t%d\t%s\n", library, titles, last)
			}
			rows.Close()
			tw.Flush()

			var built bool
			if err := db.QueryRowContext(ctx,
				`SELECT count(*) > 0 FROM information_schema.tables WHERE table_name = 'work_signal'`).Scan(&built); err != nil {
				return err
			}
			fmt.Fprintln(out)
			if !built {
				fmt.Fprintln(out, "not built yet: run `recbook build`")
				return nil
			}
			return printSignals(ctx, st, out)
		},
	}
}
