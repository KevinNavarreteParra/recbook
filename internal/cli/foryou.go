package cli

import (
	"context"
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/KevinNavarreteParra/recbook/internal/recommend"
	"github.com/KevinNavarreteParra/recbook/internal/store"
)

// forYouQuery is what a `foryou` command line asks for; like similarQuery,
// the query suite runs saved ones too.
type forYouQuery struct {
	opts recommend.ListOptions
}

func (q *forYouQuery) flags(fs *pflag.FlagSet) {
	fs.BoolVar(&q.opts.AvailableNow, "available-now", false, "only titles borrowable right now")
	fs.IntVar(&q.opts.MaxWaitDays, "max-wait", -1, "only titles available within this many days")
	fs.IntVar(&q.opts.Limit, "limit", 30, "show at most this many")
}

func (q *forYouQuery) run(ctx context.Context, st *store.Store, languages []string) ([]recommend.Rec, []recommend.GroupSummary, error) {
	q.opts.Languages = languages
	return recommend.ListForYou(ctx, st, q.opts)
}

func newForYouCmd(g *globals) *cobra.Command {
	var (
		q       forYouQuery
		groups  int
		rebuild bool
	)
	cmd := &cobra.Command{
		Use:   "foryou",
		Short: "Recommend from everything I've finished",
		Long: "Recommend audiobooks the library can lend from my whole history: every\n" +
			"book I've finished is a seed, counted by its tier, and every book I tried\n" +
			"and gave up on pushes its likes down. The seeds are first put into groups\n" +
			"that read alike, each group's seeds are fused alone, and the groups take\n" +
			"turns at the list in proportion to their weight, so no one taste crowds\n" +
			"out the rest. Each result says which seed it's closest to.\n\n" +
			"Making the list ranks the catalog for every finished book, so `recbook\n" +
			"build` makes it (and `recbook embed`, when it adds vectors), and this reads\n" +
			"it, with the library's latest availability. --rebuild makes it afresh.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			st, err := g.openStore(ctx)
			if err != nil {
				return err
			}
			defer st.Close()

			if rebuild {
				if err := recommend.BuildForYou(ctx, st, g.languages()); err != nil {
					return err
				}
			}
			recs, gs, err := q.run(ctx, st, g.languages())
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			printGroups(out, gs, groups)
			if len(recs) == 0 {
				fmt.Fprintln(out, "no recommendations")
				return nil
			}
			tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "title\tauthor\tlength\tavailability\tmine\tclosest to\tlink")
			for _, r := range recs {
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s (%s)\t%s\n", r.Title, r.Author,
					hoursMinutes(r.DurationMin, r.Abridged), availabilityText(r.Available, r.WaitDays, r.Holds),
					mine(r.Tier), r.Like, r.SimilarityText(), r.URL)
			}
			return tw.Flush()
		},
	}
	q.flags(cmd.Flags())
	cmd.Flags().IntVar(&groups, "groups", 8, "list this many of the largest groups of seeds")
	cmd.Flags().BoolVar(&rebuild, "rebuild", false, "make the list afresh first (a minute or so)")
	return cmd
}

// printGroups says how many seeds and groups there are and lists the n
// heaviest groups, each by its leader and size.
func printGroups(out io.Writer, groups []recommend.GroupSummary, n int) {
	seeds := 0
	for _, g := range groups {
		seeds += g.Seeds
	}
	fmt.Fprintf(out, "%s in %s; the largest:\n", plural(int64(seeds), "finished book"), plural(int64(len(groups)), "group"))
	for _, g := range groups[:min(n, len(groups))] {
		fmt.Fprintf(out, "  %3d  %s, %s\n", g.Seeds, g.Title, g.Author)
	}
	fmt.Fprintln(out)
}
