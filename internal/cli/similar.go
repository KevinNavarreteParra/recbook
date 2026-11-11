package cli

import (
	"context"
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/KevinNavarreteParra/recbook/internal/recommend"
	"github.com/KevinNavarreteParra/recbook/internal/search"
	"github.com/KevinNavarreteParra/recbook/internal/store"
)

func newSimilarCmd(g *globals) *cobra.Command {
	var (
		seeds, avoid []string
		view         string
		opts         recommend.Options
	)
	cmd := &cobra.Command{
		Use:   "similar --seed <book> [--seed <book>...]",
		Short: "Recommend borrowable audiobooks like the books given as seeds",
		Long: "Recommend audiobooks the library can lend that read like the seed books.\n" +
			"Name each seed by a few words of its title and author; books on my shelf\n" +
			"are matched first. Seeds count by their tier (a book I own and finished\n" +
			"counts more), unless --unweighted. --view compares descriptions (content),\n" +
			"the library's subjects (genre), or both, fused (blend). Each result says\n" +
			"which seed it's closest to. Needs `recbook build` and `recbook embed`.",
		Example: `  recbook similar --seed "tinker tailor" --seed "red october" --available-now
  recbook similar --seed "brothers karamazov" --not "da vinci code" --per-author 1`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(seeds) == 0 {
				return fmt.Errorf("give at least one --seed")
			}
			blend, err := recommend.ParseView(view)
			if err != nil {
				return err
			}
			opts.Views = blend
			ctx := cmd.Context()
			st, err := g.openStore(ctx)
			if err != nil {
				return err
			}
			defer st.Close()

			if opts.Seeds, err = resolveSeeds(ctx, st, seeds); err != nil {
				return err
			}
			if opts.Avoid, err = resolveSeeds(ctx, st, avoid); err != nil {
				return err
			}
			recs, used, err := recommend.Similar(ctx, st, opts)
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			for i, s := range used {
				role := "seed"
				if i >= len(opts.Seeds) {
					role = "avoid"
				}
				note := fmt.Sprintf("weight %.2f", s.Weight)
				if s.Skipped {
					note = "nothing to compare in this view, skipped"
				}
				fmt.Fprintf(out, "%-5s  %s, %s (%s)\n", role, s.Title, s.Author, note)
			}
			fmt.Fprintln(out)
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
	cmd.Flags().StringArrayVar(&seeds, "seed", nil, "a book I want more like (repeatable)")
	cmd.Flags().StringArrayVar(&avoid, "not", nil, "a book I want less like (repeatable)")
	cmd.Flags().StringVar(&view, "view", "blend", "compare by content, genre, or a blend of both")
	cmd.Flags().BoolVar(&opts.AvailableNow, "available-now", false, "only titles borrowable right now")
	cmd.Flags().IntVar(&opts.MaxWaitDays, "max-wait", -1, "only titles available within this many days")
	cmd.Flags().BoolVar(&opts.IncludeRead, "include-read", false, "also recommend books I've finished")
	cmd.Flags().BoolVar(&opts.Unweighted, "unweighted", false, "count every seed equally")
	cmd.Flags().IntVar(&opts.PerAuthor, "per-author", 3, "at most this many results by one author (0 for no cap)")
	cmd.Flags().IntVar(&opts.Limit, "limit", 20, "show at most this many")
	return cmd
}

// resolveSeeds turns each seed's name into a work, using the first match.
func resolveSeeds(ctx context.Context, st *store.Store, names []string) ([]int64, error) {
	var ids []int64
	for _, name := range names {
		works, err := search.Find(ctx, st, name)
		if err != nil {
			return nil, err
		}
		if len(works) == 0 {
			return nil, fmt.Errorf("no book matches %q; try fewer or different words", name)
		}
		ids = append(ids, works[0].ID)
	}
	return ids, nil
}
