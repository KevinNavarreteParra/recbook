package cli

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/KevinNavarreteParra/recbook/internal/web"
)

func newServeCmd(g *globals) *cobra.Command {
	var addr string
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the web app: search, recommendations, backlog and a listening tracker",
		Long: "Serve the web app on --addr (this machine only, by default; reach it over\n" +
			"Tailscale with `tailscale serve`). It reads the copy of the database that\n" +
			"`build`, `embed` and the nightly job publish, switching to each new copy\n" +
			"as it appears, and writes only the tracker file (<data dir>/tracker.jsonl).",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if _, err := os.Stat(g.servePath()); err != nil {
				return fmt.Errorf("no published database at %s; run `recbook build` first", g.servePath())
			}
			srv, err := web.New(ctx, g.servePath(), g.trackerPath())
			if err != nil {
				return err
			}
			defer srv.Close()
			go srv.Watch(ctx)

			httpServer := &http.Server{Addr: addr, Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second}
			go func() {
				<-ctx.Done() // Ctrl-C or systemd stop
				shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				httpServer.Shutdown(shutdown)
			}()
			log.Printf("serving %s on http://%s", g.servePath(), addr)
			if err := httpServer.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
				return err
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&addr, "addr", "127.0.0.1:8484", "address to listen on")
	return cmd
}
