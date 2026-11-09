// Package backlog lists the books I already meant to read that I could
// borrow as audiobooks right now, or soon (spec §8, phase 0.5). It needs no
// model: it joins the signal tiers to the catalog.
package backlog

import (
	"context"
	"database/sql"
	_ "embed"

	"github.com/KevinNavarreteParra/recbook/internal/store"
)

//go:embed backlog.sql
var backlogSQL string

// Options filters the list.
type Options struct {
	AvailableNow bool // only titles borrowable right now
	MaxWaitDays  int  // only titles available within this many days; <0 for no limit
	Limit        int  // at most this many; 0 means 50
}

// Entry is one backlog book, in its most borrowable edition.
type Entry struct {
	WorkID      int64
	TitleID     string // the recording's catalog id
	Tier        string // T4 (owned) or T5 (wanted)
	Title       string
	Author      string
	Narrator    string
	Available   bool
	WaitDays    sql.NullInt64 // the library's estimate, when not available now
	Holds       sql.NullInt64
	DurationMin sql.NullInt64
	Abridged    bool // abridged, dramatized or adapted; shown only when no full recording exists
	URL         string
}

// List returns the backlog, most borrowable first. It needs `recbook build`
// to have run (signals and library).
func List(ctx context.Context, st *store.Store, opts Options) ([]Entry, error) {
	if opts.Limit == 0 {
		opts.Limit = 50
	}
	rows, err := st.DB().QueryContext(ctx, backlogSQL,
		sql.Named("available_now", opts.AvailableNow),
		sql.Named("max_wait", opts.MaxWaitDays),
		sql.Named("limit", opts.Limit))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Entry
	for rows.Next() {
		var e Entry
		var author, narrator, url sql.NullString
		err := rows.Scan(&e.WorkID, &e.TitleID, &e.Tier, &e.Title, &author, &narrator, &e.Available,
			&e.WaitDays, &e.Holds, &e.DurationMin, &e.Abridged, &url)
		if err != nil {
			return nil, err
		}
		e.Author, e.Narrator, e.URL = author.String, narrator.String, url.String
		out = append(out, e)
	}
	return out, rows.Err()
}
