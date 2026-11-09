// Package embed builds the content view (spec §6): one vector per work,
// from its title and cleaned blurb, computed by a local model.
//
// A run rebuilds work_text (the text chosen for each work, work_text.sql)
// and then embeds whatever text the embedding cache doesn't hold yet,
// committing after every batch. Stopping it partway (the context is
// cancelled, or Between returns an error) loses at most one batch, and the
// next run carries on from there.
package embed

import (
	"context"
	"crypto/sha256"
	"database/sql"
	_ "embed"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/KevinNavarreteParra/recbook/internal/blurb"
	"github.com/KevinNavarreteParra/recbook/internal/store"
)

var (
	//go:embed describe.sql
	describeSQL string
	//go:embed work_text.sql
	workTextSQL string
)

// Embedder turns texts into unit-length vectors; *ollama.Client is one.
type Embedder interface {
	Embed(ctx context.Context, model string, inputs []string) ([][]float32, error)
}

// Options tunes a run.
type Options struct {
	// Model is the embedding model's name. Default nomic-embed-text.
	Model string

	// Prefix is put in front of every text, for models trained with task
	// prefixes. Default "clustering: ", which is what nomic-embed-text
	// expects when documents are compared with each other.
	Prefix string

	// BatchSize is how many texts go to the model at once. Default 32.
	BatchSize int

	// Limit stops the run after this many new embeddings; 0 means no limit.
	Limit int

	// Between, if set, runs before each batch. An error stops the run
	// cleanly; the nightly job uses it to back off when the GPU gets busy.
	Between func(ctx context.Context) error

	// Progress, if set, is called after each batch with how many texts this
	// run has embedded and how many it set out to.
	Progress func(done, total int)
}

// Report summarizes a run.
type Report struct {
	Works    int   // works with a description
	Thin     int   // ...whose blurb is too short to say much
	Cached   int   // distinct texts already embedded before this run
	Embedded int   // distinct texts embedded by this run
	Left     int   // distinct texts still to embed
	Stopped  error // why the run ended early, if it did
}

// Run rebuilds work_text and embeds any text not yet in the cache.
func Run(ctx context.Context, st *store.Store, e Embedder, opts Options) (Report, error) {
	if opts.Model == "" {
		opts.Model, opts.Prefix = "nomic-embed-text", "clustering: "
	}
	if opts.BatchSize == 0 {
		opts.BatchSize = 32
	}

	var rep Report
	todo, err := writeWorkTexts(ctx, st, opts.Model, &rep)
	if err != nil {
		if ctx.Err() != nil { // the window closed before there was anything to embed
			return Report{Stopped: ctx.Err()}, nil
		}
		return rep, err
	}

	for start := 0; start < len(todo); start += opts.BatchSize {
		if opts.Limit > 0 && rep.Embedded >= opts.Limit {
			break
		}
		if err := ctx.Err(); err != nil {
			rep.Stopped = err
			break
		}
		if opts.Between != nil {
			if err := opts.Between(ctx); err != nil {
				rep.Stopped = err
				break
			}
		}

		batch := todo[start:min(start+opts.BatchSize, len(todo))]
		inputs := make([]string, len(batch))
		for i, t := range batch {
			inputs[i] = opts.Prefix + t.text
		}
		vectors, err := e.Embed(ctx, opts.Model, inputs)
		if err != nil {
			if ctx.Err() != nil { // cancelled mid-request: a clean stop, not a failure
				rep.Stopped = ctx.Err()
				break
			}
			return rep, err
		}

		now := time.Now().UTC()
		err = st.AppendRows(ctx, "embedding", func(add func(...any) error) error {
			for i, t := range batch {
				if err := add(opts.Model, t.hash, vectors[i], now); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			return rep, fmt.Errorf("saving embeddings: %w", err)
		}
		rep.Embedded += len(batch)
		if opts.Progress != nil {
			opts.Progress(rep.Embedded, len(todo))
		}
	}
	rep.Left = len(todo) - rep.Embedded
	return rep, nil
}

// pending is a text the cache doesn't hold yet.
type pending struct{ text, hash string }

// writeWorkTexts chooses and cleans each work's text, rebuilds work_text,
// and returns the distinct texts still to embed, seeds first.
func writeWorkTexts(ctx context.Context, st *store.Store, model string, rep *Report) ([]pending, error) {
	type row struct {
		workID     int64
		side, text string
		hash       string
		thin       bool
	}
	var rows []row

	q, err := st.DB().QueryContext(ctx, describeSQL)
	if err != nil {
		return nil, err
	}
	for q.Next() {
		var r row
		var title, description string
		var onShelf bool
		if err := q.Scan(&r.workID, &title, &description, &r.side, &onShelf); err != nil {
			q.Close()
			return nil, err
		}
		cleaned := blurb.Clean(description)
		r.text = title + ". " + cleaned
		r.hash = hash(r.text)
		r.thin = blurb.IsThin(cleaned)
		rows = append(rows, r)
	}
	q.Close()
	if err := q.Err(); err != nil {
		return nil, err
	}

	if err := st.RunScript(ctx, workTextSQL, nil); err != nil {
		return nil, err
	}
	err = st.AppendRows(ctx, "work_text", func(add func(...any) error) error {
		for _, r := range rows {
			if err := add(r.workID, r.side, r.text, r.hash, r.thin); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	cached, err := cachedHashes(ctx, st.DB(), model)
	if err != nil {
		return nil, err
	}
	var todo []pending
	seen := map[string]bool{}
	for _, r := range rows {
		rep.Works++
		if r.thin {
			rep.Thin++
		}
		if seen[r.hash] {
			continue
		}
		seen[r.hash] = true
		if cached[r.hash] {
			rep.Cached++
			continue
		}
		todo = append(todo, pending{text: r.text, hash: r.hash})
	}
	return todo, nil
}

func cachedHashes(ctx context.Context, db *sql.DB, model string) (map[string]bool, error) {
	rows, err := db.QueryContext(ctx, `SELECT text_hash FROM embedding WHERE model = ?`, model)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var h string
		if err := rows.Scan(&h); err != nil {
			return nil, err
		}
		out[h] = true
	}
	return out, rows.Err()
}

// hash identifies a text: the first 16 bytes of its SHA-256, in hex.
func hash(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:16])
}
