package cli

import (
	"context"
	"fmt"
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

func (q *forYouQuery) run(ctx context.Context, st *store.Store, languages []string) ([]recommend.Rec, recommend.Walked, error) {
	q.opts.Languages = languages
	return recommend.ListForYou(ctx, st, q.opts)
}

func newForYouCmd(g *globals) *cobra.Command {
	var (
		q       forYouQuery
		rebuild bool
	)
	cmd := &cobra.Command{
		Use:   "foryou",
		Short: "Recommend from everything I've finished",
		Long: "Recommend audiobooks the library can lend from my whole history: one walk\n" +
			"of the graph view from every book I've finished, each counted by its tier,\n" +
			"less half a walk from the books I tried and gave up on. Each result says\n" +
			"which finished book it reads most like.\n\n" +
			"`recbook build` makes the list (and `embed` and `neighbours` remake it\n" +
			"when they change the graph), and this reads it, with the library's latest\n" +
			"availability. --rebuild makes it afresh first.",
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
			recs, walked, err := q.run(ctx, st, g.languages())
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "from %s, and away from the %d I gave up on\n\n",
				plural(int64(walked.Finished), "finished book"), walked.GaveUp)
			if len(recs) == 0 {
				fmt.Fprintln(out, "no recommendations")
				return nil
			}
			tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "title\tauthor\tlength\tavailability\tmine\tmost like\tlink")
			for _, r := range recs {
				like := ""
				if r.Like != "" {
					like = fmt.Sprintf("%s (%s)", r.Like, r.SimilarityText())
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", r.Title, r.Author,
					hoursMinutes(r.DurationMin, r.Abridged), availabilityText(r.Available, r.WaitDays, r.Holds),
					mine(r.Tier), like, r.URL)
			}
			return tw.Flush()
		},
	}
	q.flags(cmd.Flags())
	cmd.Flags().BoolVar(&rebuild, "rebuild", false, "make the list afresh first")
	return cmd
}
