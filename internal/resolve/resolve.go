// Package resolve decides which records are the same book (spec §5).
//
// Every shelf item and catalog title becomes a record. SQL proposes pairs
// that look like the same work: a shared ISBN, a shared external id, equal
// title and author keys, or close titles by the same author. Go then joins
// the pairs into clusters with union-find, and each cluster becomes a work.
// A hand-kept overrides file wins over both.
//
// It rebuilds these tables, in order:
//
//	record       every shelf item and catalog title, in one shape  (records.sql)
//	record_key   their matching keys, computed in Go               (keys.go)
//	match_edge   pairs that look like the same work                (edges.sql)
//	work_record  which work each record belongs to                 (clusters.go)
//	work         one row per book                                  (works.sql)
//	match_review matches worth a human look                        (works.sql)
package resolve

import (
	"context"
	_ "embed"
	"fmt"

	"github.com/KevinNavarreteParra/recbook/internal/store"
)

var (
	//go:embed records.sql
	recordsSQL string
	//go:embed edges.sql
	edgesSQL string
	//go:embed works.sql
	worksSQL string
)

// Options tunes a run.
type Options struct {
	// OverridesFile is a CSV of hand corrections (see overrides.go). A
	// missing file means no overrides.
	OverridesFile string

	// ReviewFile, if set, receives the match_review table as CSV.
	ReviewFile string

	// FuzzyMin is the Jaro-Winkler similarity two title keys need for a
	// fuzzy match. Default 0.93.
	FuzzyMin float64
}

// Report summarizes a run.
type Report struct {
	ShelfRecords, CatalogRecords int
	EdgesByMethod                map[string]int
	OverridesApplied             int
	Works                        int
	ShelfWorks                   int // works with at least one shelf record
	ShelfWorksInCatalog          int // ...of which the library has a copy
	ReviewRows                   int
}

// Run rebuilds the work tables from the contract tables.
func Run(ctx context.Context, st *store.Store, opts Options) (*Report, error) {
	if opts.FuzzyMin == 0 {
		opts.FuzzyMin = 0.93
	}
	overrides, err := readOverrides(opts.OverridesFile)
	if err != nil {
		return nil, err
	}

	steps := []struct {
		name string
		run  func() error
	}{
		{"records", func() error { return st.RunScript(ctx, recordsSQL, nil) }},
		{"keys", func() error { return writeKeys(ctx, st) }},
		{"edges", func() error { return st.RunScript(ctx, edgesSQL, store.Vars{"fuzzy_min": opts.FuzzyMin}) }},
		{"clusters", func() error { return writeClusters(ctx, st, overrides) }},
		{"works", func() error { return st.RunScript(ctx, worksSQL, nil) }},
	}
	for _, step := range steps {
		if err := step.run(); err != nil {
			return nil, fmt.Errorf("resolve %s: %w", step.name, err)
		}
	}

	if opts.ReviewFile != "" {
		if err := writeReview(ctx, st, opts.ReviewFile); err != nil {
			return nil, err
		}
	}
	return report(ctx, st, len(overrides))
}

func report(ctx context.Context, st *store.Store, overrides int) (*Report, error) {
	r := &Report{EdgesByMethod: map[string]int{}, OverridesApplied: overrides}
	db := st.DB()

	err := db.QueryRowContext(ctx, `
		SELECT count(*) FILTER (side = 'shelf'), count(*) FILTER (side = 'catalog') FROM record`).
		Scan(&r.ShelfRecords, &r.CatalogRecords)
	if err != nil {
		return nil, err
	}
	err = db.QueryRowContext(ctx, `
		SELECT count(*), count(*) FILTER (on_shelf), count(*) FILTER (on_shelf AND in_catalog) FROM work`).
		Scan(&r.Works, &r.ShelfWorks, &r.ShelfWorksInCatalog)
	if err != nil {
		return nil, err
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM match_review`).Scan(&r.ReviewRows); err != nil {
		return nil, err
	}

	rows, err := db.QueryContext(ctx, `SELECT method, count(*) FROM match_edge GROUP BY method ORDER BY method`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var method string
		var n int
		if err := rows.Scan(&method, &n); err != nil {
			return nil, err
		}
		r.EdgesByMethod[method] = n
	}
	return r, rows.Err()
}
