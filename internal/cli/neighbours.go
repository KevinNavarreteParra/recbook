package cli

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/KevinNavarreteParra/recbook/internal/embed"
	"github.com/KevinNavarreteParra/recbook/internal/graph"
	"github.com/KevinNavarreteParra/recbook/internal/recommend"
)

func newNeighboursCmd(g *globals) *cobra.Command {
	return &cobra.Command{
		Use:   "neighbours",
		Short: "Find each book's nearest books, for the graph view (minutes, on every core)",
		Long: "Find the 15 nearest texts to every embedded text, the graph view's\n" +
			"work-to-work edges, then remake the for-you list and publish for the web\n" +
			"app. It compares every text with every other, which keeps every core busy\n" +
			"for several minutes, so the nightly job does it after it embeds anything,\n" +
			"and a daytime embed doesn't. Run it by hand after a first embed.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			st, err := g.openStore(ctx)
			if err != nil {
				return err
			}
			defer st.Close()
			out := cmd.OutOrStdout()
			start := time.Now()
			if err := graph.BuildNeighbours(ctx, st, embed.DefaultModel); err != nil {
				return err
			}
			fmt.Fprintf(out, "neighbours: found in %s\n", time.Since(start).Round(time.Second))
			if err := recommend.BuildForYou(ctx, st, g.languages()); err != nil {
				return err
			}
			return st.Snapshot(ctx, g.servePath())
		},
	}
}
