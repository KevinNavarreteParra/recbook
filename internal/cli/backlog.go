package cli

import (
	"database/sql"
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
					e.Tier, e.Title, e.Author, e.Narrator, hoursMinutes(e.DurationMin, e.Abridged),
					availabilityText(e.Available, e.WaitDays, e.Holds), e.URL)
			}
			return tw.Flush()
		},
	}
	cmd.Flags().BoolVar(&opts.AvailableNow, "available-now", false, "only titles borrowable right now")
	cmd.Flags().IntVar(&opts.MaxWaitDays, "max-wait", -1, "only titles available within this many days")
	cmd.Flags().IntVar(&opts.Limit, "limit", 50, "show at most this many")
	return cmd
}

// hoursMinutes formats a running time, flagging versions that aren't the
// full text.
func hoursMinutes(minutes sql.NullInt64, abridged bool) string {
	if !minutes.Valid {
		return ""
	}
	s := fmt.Sprintf("%dh%02d", minutes.Int64/60, minutes.Int64%60)
	if abridged {
		s += " (abridged)"
	}
	return s
}

// availabilityText says when a title can be borrowed.
func availabilityText(available bool, waitDays, holds sql.NullInt64) string {
	switch {
	case available:
		return "now"
	case waitDays.Valid:
		return fmt.Sprintf("~%d days (%d holds)", waitDays.Int64, holds.Int64)
	default:
		return "unknown"
	}
}
