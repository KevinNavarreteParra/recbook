package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/KevinNavarreteParra/recbook/internal/sources"
)

func newIngestCmd(g *globals) *cobra.Command {
	var as string
	cmd := &cobra.Command{
		Use:   "ingest <source> <file>",
		Short: "Load a reading-history export, replacing that source's previous one",
		Long: "Load a reading-history export into the database. Exports are snapshots, so\n" +
			"this replaces whatever the source had before.\n\nSources:\n" + listShelfSources(),
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			src, ok := sources.LookupShelf(args[0])
			if !ok {
				return fmt.Errorf("unknown source %q; known sources:\n%s", args[0], listShelfSources())
			}
			name := src.Name
			if as != "" {
				name = as
			}

			f, err := os.Open(args[1])
			if err != nil {
				return err
			}
			defer f.Close()
			shelf, err := src.Read(f)
			if err != nil {
				return fmt.Errorf("reading %s: %w", args[1], err)
			}

			st, err := g.openStore(cmd.Context())
			if err != nil {
				return err
			}
			defer st.Close()
			if err := st.ReplaceShelf(cmd.Context(), name, args[1], shelf); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s: %d items, %d events\n", name, len(shelf.Items), len(shelf.Events))
			return nil
		},
	}
	cmd.Flags().StringVar(&as, "as", "", "store the records under this source name instead (e.g. a jsonl file from a Goodreads script)")
	return cmd
}

func listShelfSources() string {
	var b strings.Builder
	for _, s := range sources.ShelfSources {
		fmt.Fprintf(&b, "  %-12s %s\n", s.Name, s.About)
	}
	return b.String()
}
