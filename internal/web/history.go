package web

import (
	"cmp"
	"context"
	_ "embed"
	"net/http"
	"slices"
	"time"

	"github.com/KevinNavarreteParra/recbook/internal/contract"
	"github.com/KevinNavarreteParra/recbook/internal/store"
)

//go:embed history.sql
var historySQL string

// historyView is the history page: what I've started, finished and given
// up on, from Reading List (or any shelf source) and the marks made here,
// newest first.
type historyView struct {
	Rows     []historyRow
	ThisYear map[string]int // books whose latest mark this year has each label
	Sources  []loaded       // the sources behind rows not marked here
}

type historyRow struct {
	On            contract.Day
	Label         string
	Title, Author string
	TitleID       string // a mark made here: the catalog title, for undo
	Latest        bool   // the book's latest mark from its source
}

// loaded says when a source's rows were last loaded.
type loaded struct {
	Name string
	On   string
}

// sourceNames are the names the page gives shelf sources.
var sourceNames = map[string]string{"readinglist": "Reading List", "libby": "Libby", "librarything": "LibraryThing"}

func (s *Server) historyPage(w http.ResponseWriter, r *http.Request) {
	st, done := s.store()
	rows, sources, err := shelfHistory(r.Context(), st)
	done()
	if err != nil {
		s.fail(w, err)
		return
	}
	seen := map[string]bool{}
	for _, e := range s.tracker.History() {
		rows = append(rows, historyRow{On: e.On, Label: markLabel(e.Kind), Title: e.Item.Title,
			Author: first(e.Item.Authors), TitleID: e.Item.ID, Latest: !seen[e.Item.ID]})
		seen[e.Item.ID] = true
	}
	slices.SortStableFunc(rows, func(a, b historyRow) int { return b.On.Time().Compare(a.On.Time()) })

	view := historyView{Rows: rows, ThisYear: map[string]int{}, Sources: sources}
	year := time.Now().Year()
	for _, row := range rows {
		if row.Latest && row.On.Time().Year() == year {
			view.ThisYear[row.Label]++
		}
	}
	s.render(w, r, "history", page{Nav: "history", Extra: view})
}

// shelfHistory reads the history of every shelf source but the tracker
// from the published copy (history.sql), and when each was last loaded.
func shelfHistory(ctx context.Context, st *store.Store) ([]historyRow, []loaded, error) {
	rs, err := st.DB().QueryContext(ctx, historySQL)
	if err != nil {
		return nil, nil, err
	}
	defer rs.Close()
	var rows []historyRow
	var sources []string
	for rs.Next() {
		var source, kind string
		var on time.Time
		row := historyRow{Latest: true}
		if err := rs.Scan(&source, &on, &kind, &row.Title, &row.Author); err != nil {
			return nil, nil, err
		}
		row.On, row.Label = contract.DayOf(on), markLabel(contract.EventKind(kind))
		rows = append(rows, row)
		if !slices.Contains(sources, source) {
			sources = append(sources, source)
		}
	}
	if err := rs.Err(); err != nil {
		return nil, nil, err
	}
	slices.Sort(sources)
	var out []loaded
	for _, source := range sources {
		at, err := st.ImportedAt(ctx, source)
		if err != nil {
			return nil, nil, err
		}
		out = append(out, loaded{Name: cmp.Or(sourceNames[source], source), On: at.Local().Format(time.DateOnly)})
	}
	return rows, out, nil
}

func first(names []string) string {
	if len(names) == 0 {
		return ""
	}
	return names[0]
}
