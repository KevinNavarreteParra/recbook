package cli

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/KevinNavarreteParra/recbook/internal/search"
)

func newSearchCmd(g *globals) *cobra.Command {
	q := search.Query{}
	var minHours, maxHours float64
	cmd := &cobra.Command{
		Use:   "search <words...>",
		Short: "Search the library's audiobooks by title, author, narrator, series, subject or blurb",
		Long: "Keyword search over every audiobook the library has. Any word may match,\n" +
			"and word forms and accents don't matter. Books I've finished are hidden\n" +
			"unless --include-read. Run `recbook build` first.",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			q.Text = strings.Join(args, " ")
			q.MinMinutes, q.MaxMinutes = int(minHours*60), int(maxHours*60)
			st, err := g.openStore(cmd.Context())
			if err != nil {
				return err
			}
			defer st.Close()
			results, err := search.Search(cmd.Context(), st, q)
			if err != nil {
				return err
			}
			printResults(cmd.OutOrStdout(), results)
			return nil
		},
	}
	cmd.Flags().BoolVar(&q.AvailableNow, "available-now", false, "only titles borrowable right now")
	cmd.Flags().IntVar(&q.MaxWaitDays, "max-wait", -1, "only titles available within this many days")
	cmd.Flags().Float64Var(&minHours, "min-hours", 0, "at least this long")
	cmd.Flags().Float64Var(&maxHours, "max-hours", 0, "at most this long")
	cmd.Flags().BoolVar(&q.IncludeRead, "include-read", false, "also show books I've finished")
	cmd.Flags().IntVar(&q.Limit, "limit", 20, "show at most this many")
	return cmd
}

func printResults(out io.Writer, results []search.Result) {
	if len(results) == 0 {
		fmt.Fprintln(out, "no matches")
		return
	}
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "title\tauthor\tnarrator\tlength\tavailability\tmine\tlink")
	for _, r := range results {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", r.Title, r.Author, r.Narrator,
			hoursMinutes(r.DurationMin, r.Abridged), availabilityText(r.Available, r.WaitDays, r.Holds),
			mine(r.Tier), r.URL)
	}
	tw.Flush()
}

// mine says how a work relates to my shelf, from its tier.
func mine(tier string) string {
	switch tier {
	case "T2":
		return "read, own"
	case "T3":
		return "read"
	case "T4":
		return "own"
	case "T5":
		return "want"
	case "N":
		return "gave up"
	}
	return ""
}
