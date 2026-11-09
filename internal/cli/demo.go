package cli

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/KevinNavarreteParra/recbook/internal/demo"
)

func newDemoCmd(g *globals) *cobra.Command {
	return &cobra.Command{
		Use:   "demo",
		Short: "Load made-up data into data/demo.duckdb, to try recbook without real exports",
		Long: "Load a small made-up reading history and library catalog, then build.\n" +
			"Unless --db is given it uses data/demo.duckdb, so real data is never mixed\n" +
			"with demo data. Afterwards, pass the same --db to other commands.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !cmd.Flags().Changed("db") {
				g.dbPath = filepath.Join("data", "demo.duckdb")
			}
			ctx := cmd.Context()
			st, err := g.openStore(ctx)
			if err != nil {
				return err
			}
			defer st.Close()

			if err := demo.Load(ctx, st); err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "loaded the demo into %s\n\n", g.dbPath)
			if err := build(ctx, st, g.dataDir(), out); err != nil {
				return err
			}
			fmt.Fprintf(out, "\nnext: recbook --db %s backlog\n", g.dbPath)
			return nil
		},
	}
}
