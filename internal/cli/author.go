package cli

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/KevinNavarreteParra/recbook/internal/authors"
)

func newAuthorCmd(g *globals) *cobra.Command {
	var opts authors.Options
	cmd := &cobra.Command{
		Use:   "author <name>",
		Short: "More by an author, and the authors most like them",
		Long: "Show an author's books the library can lend that I haven't finished, most\n" +
			"borrowable first, and the authors whose books read most like theirs, each\n" +
			"with the few of their books nearest the author's. Name the author by a few\n" +
			"words of their name; authors I've read are matched first. Similar authors\n" +
			"need --min-works embedded books, since one or two say little about an\n" +
			"author. Needs `recbook build` and `recbook embed`.",
		Example: `  recbook author le carre
  recbook author feynman --available-now --authors 5`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			st, err := g.openStore(ctx)
			if err != nil {
				return err
			}
			defer st.Close()

			name := strings.Join(args, " ")
			found, err := authors.Find(ctx, st, name)
			if err != nil {
				return err
			}
			if len(found) == 0 {
				return fmt.Errorf("no author matches %q; try their surname alone", name)
			}
			res, err := authors.Lookup(ctx, st, found[0].ID, opts)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			printAuthor(out, res, found[1:])
			return nil
		},
	}
	cmd.Flags().BoolVar(&opts.AvailableNow, "available-now", false, "only titles borrowable right now")
	cmd.Flags().IntVar(&opts.MaxWaitDays, "max-wait", -1, "only titles available within this many days")
	cmd.Flags().BoolVar(&opts.IncludeRead, "include-read", false, "also show books I've finished")
	cmd.Flags().IntVar(&opts.Authors, "authors", 10, "how many similar authors")
	cmd.Flags().IntVar(&opts.PerAuthor, "books", 3, "books shown for each similar author")
	cmd.Flags().IntVar(&opts.MinWorks, "min-works", authors.DefaultMinWorks, "embedded books a similar author needs")
	cmd.Flags().IntVar(&opts.Limit, "limit", 50, "show at most this many of the author's own books")
	return cmd
}

// printAuthor writes the author, the two sections, and any other authors
// the name matched.
func printAuthor(out io.Writer, res *authors.Result, others []authors.Author) {
	a := res.Author
	fmt.Fprintf(out, "%s: %s in the catalog, %d finished\n", a.Name, plural(int64(a.InCatalog), "book"), a.Finished)
	if len(others) > 0 {
		var names []string
		for _, o := range others {
			names = append(names, o.Name)
		}
		fmt.Fprintf(out, "also matched: %s\n", strings.Join(names, ", "))
	}
	for _, s := range res.Spellings {
		fmt.Fprintf(out, "maybe also credited as %s (%s): try `recbook author %s`\n",
			s.Name, plural(int64(s.InCatalog), "book"), s.ID)
	}

	fmt.Fprintf(out, "\nMore by %s\n", a.Name)
	if len(res.More) == 0 {
		fmt.Fprintln(out, "  nothing else of theirs to borrow")
	} else {
		tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "  title\tnarrator\tlength\tavailability\tmine\tlink")
		for _, b := range res.More {
			fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\t%s\t%s\n", b.Title, b.Narrator, hoursMinutes(b.DurationMin, b.Abridged),
				availabilityText(b.Available, b.WaitDays, b.Holds), mine(b.Tier), b.URL)
		}
		tw.Flush()
	}

	fmt.Fprintln(out, "\nSimilar authors")
	switch {
	case res.Embedded == 0:
		fmt.Fprintln(out, "  none of their books has been embedded yet")
		return
	case len(res.Similar) == 0:
		fmt.Fprintln(out, "  none with books that match the filters")
		return
	}
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "    title\tlength\tavailability\tmine\tsimilarity\tlink")
	for _, n := range res.Similar {
		read := ""
		if n.Finished > 0 {
			read = fmt.Sprintf(", %d finished", n.Finished)
		}
		// No tab, so the author's line doesn't widen the title column.
		fmt.Fprintf(tw, "  %s (%.2f; %s%s)\n", n.Name, n.Similarity, plural(int64(n.Embedded), "book"), read)
		for _, b := range n.Books {
			fmt.Fprintf(tw, "    %s\t%s\t%s\t%s\t%.2f\t%s\n", b.Title, hoursMinutes(b.DurationMin, b.Abridged),
				availabilityText(b.Available, b.WaitDays, b.Holds), mine(b.Tier), b.Similarity, b.URL)
		}
	}
	tw.Flush()
}
