package graph

import (
	"cmp"
	"container/heap"
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"runtime"
	"slices"

	"golang.org/x/sync/errgroup"

	"github.com/KevinNavarreteParra/recbook/internal/store"
)

var (
	//go:embed texts.sql
	textsSQL string
	//go:embed text_neighbour.sql
	textNeighbourSQL string
)

// Neighbours is how many nearest texts each text keeps (spec §7: k≈15).
const Neighbours = 15

// BuildNeighbours rebuilds text_neighbour: for each text in work_text that
// has a vector in model, the Neighbours other texts nearest it (highest
// cosine). Rows are keyed by text hash, as the embedding cache is, so they
// survive builds, which renumber works; embed runs it after adding vectors.
//
// It compares every text with every other, a hundred thousand squared,
// which is the whole cost: several minutes on every core. So only the
// nightly job runs it (and `recbook neighbours`), never a daytime embed.
// If ctx is cancelled first, it stops and leaves the table as it was.
func BuildNeighbours(ctx context.Context, st *store.Store, model string) error {
	hashes, vectors, dim, err := loadTextVectors(ctx, st, model)
	if err != nil {
		return err
	}
	near, err := nearest(ctx, vectors, dim, Neighbours)
	if err != nil {
		return err
	}

	if err := st.RunScript(ctx, textNeighbourSQL, nil); err != nil {
		return fmt.Errorf("text neighbours: %w", err)
	}
	return st.AppendRows(ctx, "text_neighbour", func(add func(...any) error) error {
		for i, list := range near {
			for _, n := range list {
				if err := add(model, hashes[i], hashes[n.index], float64(n.similarity)); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// loadTextVectors returns the hashes of work_text's texts that have a
// vector in model, and their vectors, end to end in one slice.
func loadTextVectors(ctx context.Context, st *store.Store, model string) ([]string, []float32, int, error) {
	rows, err := st.DB().QueryContext(ctx, textsSQL, sql.Named("model", model))
	if err != nil {
		return nil, nil, 0, err
	}
	defer rows.Close()
	var hashes []string
	var vectors []float32
	dim := 0
	for rows.Next() {
		var hash string
		var list any // a FLOAT[] comes as []any
		if err := rows.Scan(&hash, &list); err != nil {
			return nil, nil, 0, err
		}
		vector, _ := list.([]any)
		if dim == 0 {
			dim = len(vector)
		}
		if len(vector) != dim {
			return nil, nil, 0, fmt.Errorf("text %s has %d dimensions, not %d", hash, len(vector), dim)
		}
		hashes = append(hashes, hash)
		for _, x := range vector {
			vectors = append(vectors, x.(float32))
		}
	}
	return hashes, vectors, dim, rows.Err()
}

// neighbour is one of a text's nearest texts: its index and cosine.
type neighbour struct {
	index      int
	similarity float32
}

// nearest returns, for each of the vectors (unit length, dim long, end to
// end), the k others with the highest inner product, best first.
//
// Rows are taken a block at a time, each block against every vector, so
// that a block's vectors stay in the CPU's cache while the rest stream
// past: done a row at a time, the whole set would be read from memory once
// per row.
func nearest(ctx context.Context, vectors []float32, dim, k int) ([][]neighbour, error) {
	n := len(vectors) / dim
	out := make([][]neighbour, n)
	const block = 64
	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(runtime.GOMAXPROCS(0))
	for start := 0; start < n; start += block {
		g.Go(func() error {
			if err := ctx.Err(); err != nil {
				return err
			}
			end := min(start+block, n)
			heaps := make([]topK, end-start)
			for j := range n {
				vj := vectors[j*dim : (j+1)*dim]
				for i := start; i < end; i++ {
					if i == j {
						continue
					}
					heaps[i-start].offer(neighbour{j, dot(vectors[i*dim:(i+1)*dim], vj)}, k)
				}
			}
			for i := start; i < end; i++ {
				out[i] = heaps[i-start].sorted()
			}
			return nil
		})
	}
	return out, g.Wait()
}

// dot is the inner product of two vectors of one length, with four
// running sums so the additions needn't wait on one another.
func dot(a, b []float32) float32 {
	b = b[:len(a)]
	var s0, s1, s2, s3 float32
	i := 0
	for ; i+4 <= len(a); i += 4 {
		s0 += a[i] * b[i]
		s1 += a[i+1] * b[i+1]
		s2 += a[i+2] * b[i+2]
		s3 += a[i+3] * b[i+3]
	}
	for ; i < len(a); i++ {
		s0 += a[i] * b[i]
	}
	return s0 + s1 + s2 + s3
}

// topK keeps the k most similar neighbours offered: a min-heap, so the
// least similar of those kept is the one a better offer replaces.
type topK []neighbour

func (h topK) Len() int           { return len(h) }
func (h topK) Less(i, j int) bool { return h[i].similarity < h[j].similarity }
func (h topK) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *topK) Push(x any)        { *h = append(*h, x.(neighbour)) }
func (h *topK) Pop() any {
	old := *h
	x := old[len(old)-1]
	*h = old[:len(old)-1]
	return x
}

func (h *topK) offer(n neighbour, k int) {
	if len(*h) < k {
		heap.Push(h, n)
	} else if n.similarity > (*h)[0].similarity {
		(*h)[0] = n
		heap.Fix(h, 0)
	}
}

// sorted returns the neighbours kept, most similar first.
func (h topK) sorted() []neighbour {
	out := slices.Clone([]neighbour(h))
	slices.SortFunc(out, func(a, b neighbour) int { return cmp.Compare(b.similarity, a.similarity) })
	return out
}
