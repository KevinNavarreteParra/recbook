package cli

import (
	"encoding/csv"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/KevinNavarreteParra/recbook/internal/eval"
	"github.com/KevinNavarreteParra/recbook/internal/recommend"
)

func newEvalCmd(g *globals) *cobra.Command {
	var (
		opts        eval.Options
		genreWeight float64
	)
	cmd := &cobra.Command{
		Use:   "eval",
		Short: "Measure recommendations by leave-one-out over the books I've finished",
		Long: "Hold out each finished book the library lends, as if I hadn't read it,\n" +
			"and see where recommendations rank it: from every other finished book at\n" +
			"once (history), and from the best single one of them (one seed). The\n" +
			"content and genre views, alone and blended, are compared with two\n" +
			"baselines (books by authors I've finished; books on the shelves I read\n" +
			"most from) and with chance.\n\n" +
			"hit@N is the share of held-out books ranked in the top N; MRR is the mean\n" +
			"of 1/rank. Baselines tie a lot, so theirs are expected values. Each book's\n" +
			"ranks go to <data dir>/review/loo.csv, to compare runs. Needs `recbook\n" +
			"build` and `recbook embed`; it reads the database only.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := g.openStore(cmd.Context())
			if err != nil {
				return err
			}
			defer st.Close()

			fmt.Fprintln(cmd.ErrOrStderr(), "ranking the catalog against every finished book; this takes a few minutes")
			opts.Blend = recommend.Blend{recommend.Content: 1, recommend.Genre: genreWeight}
			rep, err := eval.Run(cmd.Context(), st, opts)
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "held out %d finished books the library lends, one at a time\n", len(rep.Targets))
			fmt.Fprintf(out, "seeds    the other finished books with a description or subjects (%d of %d)\n", rep.Seeds, rep.Finished)
			fmt.Fprintf(out, "pool     %d unread borrowable works\n\n", rep.Pool)

			for _, protocol := range eval.Protocols {
				fmt.Fprintln(out, protocolText[protocol])
				tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', tabwriter.AlignRight)
				header := []string{"method"}
				for _, k := range eval.Cutoffs {
					header = append(header, fmt.Sprintf("hit@%d", k))
				}
				fmt.Fprintln(tw, strings.Join(append(header, "MRR", "median rank"), "\t")+"\t")
				for _, r := range rep.Results {
					if r.Protocol != protocol {
						continue
					}
					cells := []string{r.Method}
					for _, h := range r.Hits {
						cells = append(cells, fmt.Sprintf("%.1f%%", 100*h))
					}
					cells = append(cells, fmt.Sprintf("%.3f", r.MRR), rankText(r.MedianRank))
					fmt.Fprintln(tw, strings.Join(cells, "\t")+"\t")
				}
				if err := tw.Flush(); err != nil {
					return err
				}
				fmt.Fprintln(out)
			}

			file := filepath.Join(g.dataDir(), "review", "loo.csv")
			if err := writeRanks(file, rep); err != nil {
				return err
			}
			fmt.Fprintf(out, "ranks per book in %s\n", file)
			return nil
		},
	}
	cmd.Flags().Float64Var(&genreWeight, "genre-weight", recommend.DefaultBlend[recommend.Genre],
		"the blend's weight for genre, with content at 1")
	cmd.Flags().BoolVar(&opts.Unweighted, "unweighted", false, "count every seed equally")
	cmd.Flags().IntVar(&opts.PerAuthor, "per-author", 3, "at most this many results by one author, as in similar (0 for no cap)")
	cmd.Flags().IntVar(&opts.MaxRank, "max-rank", 1000, "count a book ranked below this as missed")
	return cmd
}

// protocolText introduces each protocol's table.
var protocolText = map[string]string{
	"history":  "history: every other finished book as a seed at once (a \"for you\" list)",
	"one seed": "one seed: the best rank any single finished book gives it as the only seed",
}

// rankText formats an expected rank: whole, or "missed".
func rankText(r float64) string {
	if math.IsInf(r, 1) {
		return "missed"
	}
	return strconv.FormatFloat(math.Round(r), 'f', 0, 64)
}

// writeRanks writes one row per held-out book: who it is, and each
// method's rank for it. Works are named rather than numbered, since work
// ids change with every build.
func writeRanks(file string, rep *eval.Report) error {
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	f, err := os.Create(file)
	if err != nil {
		return err
	}
	w := csv.NewWriter(f)
	header := []string{"title", "author", "tier"}
	for _, r := range rep.Results {
		header = append(header, r.Protocol+": "+r.Method)
	}
	w.Write(header)
	for _, t := range rep.Targets {
		row := []string{t.Title, t.Author, t.Tier}
		for _, r := range t.Ranks {
			row = append(row, rankText(r))
		}
		w.Write(row)
	}
	w.Flush()
	if err := w.Error(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
