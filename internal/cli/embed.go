package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/KevinNavarreteParra/recbook/internal/embed"
	"github.com/KevinNavarreteParra/recbook/internal/nightly"
	"github.com/KevinNavarreteParra/recbook/internal/ollama"
	"github.com/KevinNavarreteParra/recbook/internal/store"
)

func newEmbedCmd(g *globals) *cobra.Command {
	var opts embedOptions
	cmd := &cobra.Command{
		Use:   "embed",
		Short: "Embed work descriptions on the GPU, skipping any already done",
		Long: "Embed each work's title and cleaned blurb with a local model through\n" +
			"Ollama, for the content view. Vectors are cached, so a run only does what\n" +
			"earlier runs didn't. It refuses to start while the GPU is busy, stops if\n" +
			"something else starts using it, and starts and stops its own Ollama\n" +
			"server unless one is already running. Run `recbook build` first.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := g.openStore(cmd.Context())
			if err != nil {
				return err
			}
			defer st.Close()
			_, err = embedWorks(cmd.Context(), st, opts, cmd.OutOrStdout())
			return err
		},
	}
	addEmbedFlags(cmd, &opts)
	return cmd
}

type embedOptions struct {
	model, prefix string
	ollamaBinary  string
	limit         int
}

func addEmbedFlags(cmd *cobra.Command, opts *embedOptions) {
	cmd.Flags().StringVar(&opts.model, "model", "", "Ollama embedding model (default nomic-embed-text)")
	cmd.Flags().StringVar(&opts.prefix, "prefix", "", "task prefix the model expects (default \"clustering: \" for nomic-embed-text)")
	cmd.Flags().StringVar(&opts.ollamaBinary, "ollama", defaultOllama(), "ollama binary, used when no server is running")
	cmd.Flags().IntVar(&opts.limit, "limit", 0, "stop after embedding this many new texts")
}

// embedWorks runs the embedding step with the GPU guard: claim the GPU,
// bring up Ollama, make sure the model is there, embed, shut Ollama down.
// A busy GPU or a closing window ends it early without an error.
func embedWorks(ctx context.Context, st *store.Store, opts embedOptions, out io.Writer) (embed.Report, error) {
	guard, err := nightly.Claim(ctx)
	if err != nil {
		fmt.Fprintln(out, "embed:", err)
		return embed.Report{Stopped: err}, nil
	}

	server, err := ollama.Serve(ctx, opts.ollamaBinary, ollama.DefaultHost)
	if err != nil {
		return embed.Report{}, err
	}
	defer server.Stop()

	model := opts.model
	if model == "" {
		model = "nomic-embed-text"
		if opts.prefix == "" {
			opts.prefix = "clustering: "
		}
	}
	fmt.Fprintf(out, "embed: making sure %s is downloaded\n", model)
	if err := server.Pull(ctx, model); err != nil {
		return embed.Report{}, err
	}

	// Report every 30s, estimating from the rate since the last report: the
	// first batches are slow while the model loads.
	lastReport, lastDone := time.Now(), 0
	progress := func(done, total int) {
		elapsed := time.Since(lastReport)
		if elapsed < 30*time.Second {
			return
		}
		rate := float64(done-lastDone) / elapsed.Seconds()
		lastReport, lastDone = time.Now(), done
		left := time.Duration(float64(total-done)/rate) * time.Second
		fmt.Fprintf(out, "embed: %d of %d (%.0f/s, about %s left)\n", done, total, rate, left.Round(time.Minute))
	}

	rep, err := embed.Run(ctx, st, server, embed.Options{
		Model: model, Prefix: opts.prefix, Limit: opts.limit, Progress: progress,
		Between: func(ctx context.Context) error {
			vram, err := server.VRAMBytes(ctx)
			if err != nil {
				return err
			}
			return guard.Check(ctx, int(vram>>20))
		},
	})
	if err != nil {
		return rep, err
	}
	fmt.Fprintf(out, "embed: %d works described (%d thin); %d texts were already embedded, %d embedded now, %d left\n",
		rep.Works, rep.Thin, rep.Cached, rep.Embedded, rep.Left)
	if rep.Stopped != nil {
		fmt.Fprintln(out, "embed: stopped early:", rep.Stopped)
	}
	return rep, nil
}

// defaultOllama finds the ollama binary: on PATH, or where the release
// tarball was unpacked (~/.local/bin), which systemd's PATH doesn't include.
func defaultOllama() string {
	if path, err := exec.LookPath("ollama"); err == nil {
		return path
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "bin", "ollama")
}
