// Package search is keyword search over everything the library can lend:
// a BM25 full-text index over each work's titles, people, series,
// subjects and blurbs (index.sql). Unlike Libby's search, any word can
// match, word forms and accents don't matter, and the results can be
// filtered by what I've read and what I can borrow now.
//
// It also finds works by name (Find), which recommendations use to turn
// "Tinker Tailor" into a seed.
package search

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"

	"github.com/KevinNavarreteParra/recbook/internal/store"
)

var (
	//go:embed index.sql
	indexSQL string
	//go:embed query.sql
	querySQL string
	//go:embed find.sql
	findSQL string
)

// Build rebuilds the search index. It needs resolve, signals and library
// to have run.
func Build(ctx context.Context, st *store.Store) error {
	if err := st.RunScript(ctx, indexSQL, nil); err != nil {
		return fmt.Errorf("search index: %w", err)
	}
	return nil
}

// Query is a search and its filters.
type Query struct {
	Text         string
	AvailableNow bool // only what can be borrowed right now
	MaxWaitDays  int  // only what's available within this many days; <0 for no limit
	MinMinutes   int  // running time at least this long; 0 for no limit
	MaxMinutes   int  // running time at most this long; 0 for no limit
	IncludeRead  bool // also show books I've finished
	Limit        int  // at most this many; 0 means 20
}

// Result is one work found, in the recording the library would lend.
type Result struct {
	WorkID      int64
	TitleID     string // the recording's catalog id
	Title       string
	Subtitle    string
	Author      string
	Narrator    string
	DurationMin sql.NullInt64
	Abridged    bool
	Available   bool
	WaitDays    sql.NullInt64
	Holds       sql.NullInt64
	URL         string
	Tier        string // my relation to it ("T4" owned, "T5" wanted, ...), or ""
	Score       float64
}

// Search runs a keyword query, best matches first.
func Search(ctx context.Context, st *store.Store, q Query) ([]Result, error) {
	if q.Limit == 0 {
		q.Limit = 20
	}
	rows, err := st.DB().QueryContext(ctx, querySQL,
		sql.Named("q", q.Text),
		sql.Named("include_read", q.IncludeRead),
		sql.Named("available_now", q.AvailableNow),
		sql.Named("max_wait", q.MaxWaitDays),
		sql.Named("min_minutes", q.MinMinutes),
		sql.Named("max_minutes", q.MaxMinutes),
		sql.Named("limit", q.Limit))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Result
	for rows.Next() {
		var r Result
		var subtitle, author, narrator, url, tier sql.NullString
		err := rows.Scan(&r.WorkID, &r.TitleID, &r.Title, &subtitle, &author, &narrator, &r.DurationMin, &r.Abridged,
			&r.Available, &r.WaitDays, &r.Holds, &url, &tier, &r.Score)
		if err != nil {
			return nil, err
		}
		r.Subtitle, r.Author, r.Narrator, r.URL, r.Tier = subtitle.String, author.String, narrator.String, url.String, tier.String
		out = append(out, r)
	}
	return out, rows.Err()
}

// Work is a work found by name.
type Work struct {
	ID      int64
	Title   string
	Author  string
	OnShelf bool
}

// Find returns up to five works whose title and author contain every word
// of name, those on my shelf first.
func Find(ctx context.Context, st *store.Store, name string) ([]Work, error) {
	rows, err := st.DB().QueryContext(ctx, findSQL, sql.Named("q", name))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Work
	for rows.Next() {
		var w Work
		if err := rows.Scan(&w.ID, &w.Title, &w.Author, &w.OnShelf); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}
