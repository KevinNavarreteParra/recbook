package cli

import (
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/KevinNavarreteParra/recbook/internal/backlog"
)

func newBacklogCmd(g *globals) *cobra.Command {
	opts := backlog.Options{}
	cmd := &cobra.Command{
		Use:   "backlog",
		Short: "List books I own or want, unfinished, that the library has as audiobooks",
		Long: "List the books I already meant to read (owned but unfinished, or on a\n" +
			"to-read list) that the library has as audiobooks, borrowable-now first.\n" +
			"Availability is as of the last crawl. Run `recbook build` first.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := g.openStore(cmd.Context())
			if err != nil {
				return err
			}
			defer st.Close()
			entries, err := backlog.List(cmd.Context(), st, opts)
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			if len(entries) == 0 {
				fmt.Fprintln(out, "nothing on the backlog matches")
				return nil
			}
			tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "tier\ttitle\tauthor\tnarrator\tlength\tavailability\tlink")
			for _, e := range entries {
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
					e.Tier, e.Title, e.Author, e.Narrator, length(e), availability(e), e.URL)
			}
			return tw.Flush()
		},
	}
	cmd.Flags().BoolVar(&opts.AvailableNow, "available-now", false, "only titles borrowable right now")
	cmd.Flags().IntVar(&opts.MaxWaitDays, "max-wait", -1, "only titles available within this many days")
	cmd.Flags().IntVar(&opts.Limit, "limit", 50, "show at most this many")
	return cmd
}

func length(e backlog.Entry) string {
	if !e.DurationMin.Valid {
		return ""
	}
	return fmt.Sprintf("%dh%02d", e.DurationMin.Int64/60, e.DurationMin.Int64%60)
}

func availability(e backlog.Entry) string {
	switch {
	case e.Available:
		return "now"
	case e.WaitDays.Valid:
		return fmt.Sprintf("~%d days (%d holds)", e.WaitDays.Int64, e.Holds.Int64)
	default:
		return "unknown"
	}
}
