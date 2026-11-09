package resolve

import (
	"context"
	"fmt"
	"slices"

	"github.com/KevinNavarreteParra/recbook/internal/store"
)

// writeClusters groups records connected by match edges (after overrides)
// into works, and loads the result into work_record. Records with no edges
// become works of their own.
func writeClusters(ctx context.Context, st *store.Store, overrides []override) error {
	refs, err := queryStrings(ctx, st, `SELECT ref FROM record ORDER BY ref`)
	if err != nil {
		return err
	}
	index := make(map[string]int, len(refs))
	for i, ref := range refs {
		index[ref] = i
	}

	edges, err := queryPairs(ctx, st, `SELECT a, b FROM match_edge`)
	if err != nil {
		return err
	}
	edges, err = applyOverrides(edges, overrides, index)
	if err != nil {
		return err
	}

	uf := newUnionFind(len(refs))
	for _, e := range edges {
		uf.union(index[e[0]], index[e[1]])
	}

	// Number works in ref order so ids are the same from run to run.
	workID := map[int]int64{}
	return st.AppendRows(ctx, "work_record", func(add func(...any) error) error {
		for i, ref := range refs {
			root := uf.find(i)
			id, ok := workID[root]
			if !ok {
				id = int64(len(workID) + 1)
				workID[root] = id
			}
			if err := add(id, ref); err != nil {
				return err
			}
		}
		return nil
	})
}

// applyOverrides drops every edge touching a record marked "none" and adds
// an edge for every "link". It fails on refs that name no record, which
// usually means a typo or an id that changed between exports.
func applyOverrides(edges [][2]string, overrides []override, index map[string]int) ([][2]string, error) {
	isolated := map[string]bool{}
	for _, o := range overrides {
		if _, ok := index[o.ref]; !ok {
			return nil, fmt.Errorf("%s: no record has ref %q", o.where, o.ref)
		}
		if o.linkTo == "" {
			isolated[o.ref] = true
		} else if _, ok := index[o.linkTo]; !ok {
			return nil, fmt.Errorf("%s: no record has ref %q", o.where, o.linkTo)
		}
	}

	edges = slices.DeleteFunc(edges, func(e [2]string) bool { return isolated[e[0]] || isolated[e[1]] })
	for _, o := range overrides {
		if o.linkTo != "" {
			edges = append(edges, [2]string{o.ref, o.linkTo})
		}
	}
	return edges, nil
}

// unionFind tracks which items have been joined into the same group. find
// returns a group's representative; union merges two groups. The
// representative is always the group's smallest index, which keeps the
// numbering above deterministic.
type unionFind struct {
	parent []int
}

func newUnionFind(n int) *unionFind {
	uf := &unionFind{parent: make([]int, n)}
	for i := range uf.parent {
		uf.parent[i] = i
	}
	return uf
}

func (uf *unionFind) find(x int) int {
	for uf.parent[x] != x {
		uf.parent[x] = uf.parent[uf.parent[x]] // path halving keeps the trees shallow
		x = uf.parent[x]
	}
	return x
}

func (uf *unionFind) union(a, b int) {
	ra, rb := uf.find(a), uf.find(b)
	switch {
	case ra < rb:
		uf.parent[rb] = ra
	case rb < ra:
		uf.parent[ra] = rb
	}
}

func queryStrings(ctx context.Context, st *store.Store, query string) ([]string, error) {
	rows, err := st.DB().QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func queryPairs(ctx context.Context, st *store.Store, query string) ([][2]string, error) {
	rows, err := st.DB().QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out [][2]string
	for rows.Next() {
		var p [2]string
		if err := rows.Scan(&p[0], &p[1]); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
