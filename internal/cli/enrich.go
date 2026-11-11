package cli

import (
	"context"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/KevinNavarreteParra/recbook/internal/enrich"
	"github.com/KevinNavarreteParra/recbook/internal/store"
)

func newEnrichCmd(g *globals) *cobra.Command {
	return &cobra.Command{
		Use:   "enrich",
		Short: "Look up descriptions for finished books that no source describes",
		Long: "Find the finished books with no description to compare (mostly print books\n" +
			"the library doesn't carry), and look each up in Google Books, then Open\n" +
			"Library, by ISBN and then by title and author, at one request a second.\n" +
			"Every answer is kept, so each book is asked about once; the nightly job\n" +
			"runs this too. What's found is saved as the shelf \"enrich\": run `recbook\n" +
			"build` and `recbook embed` afterwards to use it.\n\n" +
			"Google Books often refuses requests without an API key; set\n" +
			"RECBOOK_GOOGLE_BOOKS_KEY to use one. A service that fails is skipped for\n" +
			"the rest of the run and asked again next time.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := g.openStore(cmd.Context())
			if err != nil {
				return err
			}
			defer st.Close()
			return enrichSeeds(cmd.Context(), st, cmd.OutOrStdout())
		},
	}
}

// enrichSeeds runs enrich with the real services and reports. A Google
// Books API key, if there is one, comes from RECBOOK_GOOGLE_BOOKS_KEY.
func enrichSeeds(ctx context.Context, st *store.Store, out io.Writer) error {
	rep, err := enrich.Run(ctx, st, enrich.Services(os.Getenv("RECBOOK_GOOGLE_BOOKS_KEY")))
	if err != nil {
		return err
	}
	var parts []string
	total := 0
	for _, ref := range slices.Sorted(maps.Keys(rep.Described)) {
		parts = append(parts, fmt.Sprintf("%s %d", ref, rep.Described[ref]))
		total += rep.Described[ref]
	}
	fmt.Fprintf(out, "enrich: %d finished books had no description; found %d (%s), %d of them thin\n",
		rep.Wanting, total, strings.Join(parts, ", "), rep.Thin)
	fmt.Fprintf(out, "enrich: asked %d questions; the rest were answered from earlier runs\n", rep.Asked)
	for _, ref := range slices.Sorted(maps.Keys(rep.Failed)) {
		fmt.Fprintf(out, "enrich: gave up on %s for this run; a later run asks again: %v\n", ref, rep.Failed[ref])
	}
	return nil
}
