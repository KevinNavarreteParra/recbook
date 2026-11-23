package cli

import (
	"context"
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/KevinNavarreteParra/recbook/internal/recommend"
	"github.com/KevinNavarreteParra/recbook/internal/search"
	"github.com/KevinNavarreteParra/recbook/internal/store"
)

// similarQuery is what a `similar` command line asks for. The query suite
// runs saved ones too (suite.go), so the flags and running are kept apart
// from printing.
type similarQuery struct {
	seeds, avoid []string
	view         string
	opts         recommend.Options
}

func (q *similarQuery) flags(fs *pflag.FlagSet) {
	fs.StringArrayVar(&q.seeds, "seed", nil, "a book I want more like (repeatable)")
	fs.StringArrayVar(&q.avoid, "not", nil, "a book I want less like (repeatable)")
	fs.StringVar(&q.view, "view", "blend", "compare by content, genre, or a blend of both")
	fs.BoolVar(&q.opts.AvailableNow, "available-now", false, "only titles borrowable right now")
	fs.IntVar(&q.opts.MaxWaitDays, "max-wait", -1, "only titles available within this many days")
	fs.BoolVar(&q.opts.IncludeRead, "include-read", false, "also recommend books I've finished")
	fs.BoolVar(&q.opts.Unweighted, "unweighted", false, "count every seed equally")
	fs.IntVar(&q.opts.PerAuthor, "per-author", 3, "at most this many results by one author (0 for no cap)")
	fs.Float64Var(&q.opts.Diversity, "diversity", recommend.DefaultDiversity,
		"trade closeness for variety, 0..1: 0 for the closest matches, higher to range wider")
	fs.IntVar(&q.opts.Limit, "limit", 20, "show at most this many")
}

// run finds the seeds by name and recommends. It fills in q.opts.Seeds
// and q.opts.Avoid, so the caller can tell the used seeds apart.
func (q *similarQuery) run(ctx context.Context, st *store.Store, languages []string) ([]recommend.Rec, []recommend.Seed, error) {
	if len(q.seeds) == 0 {
		return nil, nil, fmt.Errorf("give at least one --seed")
	}
	blend, err := recommend.ParseView(q.view)
	if err != nil {
		return nil, nil, err
	}
	q.opts.Views, q.opts.Languages = blend, languages
	if q.opts.Seeds, err = resolveSeeds(ctx, st, q.seeds, languages); err != nil {
		return nil, nil, err
	}
	if q.opts.Avoid, err = resolveSeeds(ctx, st, q.avoid, languages); err != nil {
		return nil, nil, err
	}
	return recommend.Similar(ctx, st, q.opts)
}

func newSimilarCmd(g *globals) *cobra.Command {
	var q similarQuery
	cmd := &cobra.Command{
		Use:   "similar --seed <book> [--seed <book>...]",
		Short: "Recommend borrowable audiobooks like the books given as seeds",
		Long: "Recommend audiobooks the library can lend that read like the seed books.\n" +
			"Name each seed by a few words of its title and author; books on my shelf\n" +
			"are matched first. Seeds count by their tier (a book I own and finished\n" +
			"counts more), unless --unweighted. --view compares descriptions (content),\n" +
			"the library's subjects (genre), or both, fused (blend). Only recordings in\n" +
			"the --lang languages are recommended. Each seed says where its description\n" +
			"came from, and each result which seed it's closest to. Needs `recbook\n" +
			"build` and `recbook embed`.",
		Example: `  recbook similar --seed "tinker tailor" --seed "red october" --available-now
  recbook similar --seed "brothers karamazov" --not "da vinci code" --per-author 1`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			st, err := g.openStore(ctx)
			if err != nil {
				return err
			}
			defer st.Close()

			recs, used, err := q.run(ctx, st, g.languages())
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			for i, s := range used {
				role := "seed"
				if i >= len(q.opts.Seeds) {
					role = "avoid"
				}
				note := fmt.Sprintf("weight %.2f", s.Weight)
				switch {
				case s.Embedded:
					note += "; described by " + described(s)
				case s.TextSource != "":
					note += "; description by " + described(s) + " not embedded yet"
				}
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
	q.flags(cmd.Flags())
	return cmd
}

// described says where a seed's description came from, and its language:
// "the catalog (es)". A seed described in another language than mine finds
// books in that language.
func described(s recommend.Seed) string {
	from := s.TextSource
	if from == "catalog" {
		from = "the catalog"
	}
	if s.TextLanguage != "" {
		from += " (" + s.TextLanguage + ")"
	}
	return from
}

// resolveSeeds turns each seed's name into a work, using the first match
// (search.Find: books on my shelf, then in my languages).
func resolveSeeds(ctx context.Context, st *store.Store, names []string, languages []string) ([]int64, error) {
	var ids []int64
	for _, name := range names {
		works, err := search.Find(ctx, st, name, languages)
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
