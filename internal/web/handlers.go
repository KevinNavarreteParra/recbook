package web

import (
	"bytes"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/KevinNavarreteParra/recbook/internal/backlog"
	"github.com/KevinNavarreteParra/recbook/internal/contract"
	"github.com/KevinNavarreteParra/recbook/internal/recommend"
	"github.com/KevinNavarreteParra/recbook/internal/search"
)

// filters are the options every list shares, read from the query string.
type filters struct {
	AvailableNow bool   // "now=1"
	MaxWait      string // "wait": "", "7", "14", "30"
	Length       string // "len": "", "short", "medium", "long"
	IncludeRead  bool   // "read=1"
}

func readFilters(q url.Values) filters {
	return filters{
		AvailableNow: q.Get("now") == "1",
		MaxWait:      q.Get("wait"),
		Length:       q.Get("len"),
		IncludeRead:  q.Get("read") == "1",
	}
}

func (f filters) maxWaitDays() int {
	if days, err := strconv.Atoi(f.MaxWait); err == nil {
		return days
	}
	return -1
}

// minutes turns the length choice into a range; 0 means no bound.
func (f filters) minutes() (min, max int) {
	switch f.Length {
	case "short":
		return 0, 6 * 60
	case "medium":
		return 6 * 60, 12 * 60
	case "long":
		return 12 * 60, 0
	}
	return 0, 0
}

// row is one book in any results table.
type row struct {
	WorkID       int64
	TitleID      string
	Title        string
	Subtitle     string
	Author       string
	Narrator     string
	Length       string
	Available    bool
	Availability string
	Mine         string // my relation to it: "read", "own", "want", ...
	Why          string // for recommendations: the closest seed
	URL          string
	Mark         *Status // my latest mark in the tracker, if any
}

// rowsView is a results table and what produced it.
type rowsView struct {
	Rows    []row
	Empty   string // shown when there are no rows
	Caption string
}

// page is what every page template gets.
type page struct {
	Nav     string // which tab is active
	Query   string
	Filters filters
	Results *rowsView
	Seeds   []seedChip
	Extra   any // page-specific data
}

type seedChip struct {
	ID     int64
	Title  string
	Author string
}

func (s *Server) searchPage(w http.ResponseWriter, r *http.Request) {
	st, done := s.store()
	defer done()
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	f := readFilters(r.URL.Query())
	p := page{Nav: "search", Query: q, Filters: f}

	if q == "" {
		// The landing page: what's on my backlog and borrowable now.
		entries, err := backlog.List(r.Context(), st, backlog.Options{AvailableNow: true, MaxWaitDays: -1, Limit: 10})
		if err != nil {
			s.fail(w, err)
			return
		}
		p.Results = &rowsView{Rows: s.backlogRows(entries), Caption: "From your backlog, available now",
			Empty: "Nothing on your backlog is available right now."}
	} else {
		min, max := f.minutes()
		results, err := search.Search(r.Context(), st, search.Query{
			Text: q, AvailableNow: f.AvailableNow, MaxWaitDays: f.maxWaitDays(),
			MinMinutes: min, MaxMinutes: max, IncludeRead: f.IncludeRead, Limit: 30,
		})
		if err != nil {
			s.fail(w, err)
			return
		}
		p.Results = &rowsView{Rows: s.searchRows(results), Empty: "No matches. Try fewer words, or include books you've read."}
	}
	s.render(w, r, "search", p)
}

func (s *Server) recommendPage(w http.ResponseWriter, r *http.Request) {
	st, done := s.store()
	defer done()
	f := readFilters(r.URL.Query())
	p := page{Nav: "recommend", Filters: f}

	var ids []int64
	for _, v := range r.URL.Query()["seed"] {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil {
			ids = append(ids, id)
		}
	}
	if len(ids) > 0 {
		recs, seeds, err := recommend.Similar(r.Context(), st, recommend.Options{
			Seeds: ids, AvailableNow: f.AvailableNow, MaxWaitDays: f.maxWaitDays(),
			IncludeRead: f.IncludeRead, PerAuthor: 3, Limit: 30,
		})
		if err != nil {
			s.fail(w, err)
			return
		}
		var skipped []string
		for _, sd := range seeds {
			p.Seeds = append(p.Seeds, seedChip{sd.WorkID, sd.Title, sd.Author})
			if sd.Skipped {
				skipped = append(skipped, sd.Title)
			}
		}
		view := &rowsView{Rows: s.recommendRows(recs), Empty: "No recommendations with these filters."}
		if len(skipped) > 0 {
			view.Caption = "Skipped (nothing to compare): " + strings.Join(skipped, ", ")
		}
		p.Results = view
	}
	s.render(w, r, "recommend", p)
}

// seedOptions suggests seeds as I type, books on my shelf first.
func (s *Server) seedOptions(w http.ResponseWriter, r *http.Request) {
	st, done := s.store()
	defer done()
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	var works []search.Work
	if q != "" {
		var err error
		if works, err = search.Find(r.Context(), st, q); err != nil {
			s.fail(w, err)
			return
		}
	}
	s.renderPartial(w, "seed-options", works)
}

// seedChip renders one chosen seed, to add to the seed list.
func (s *Server) seedChip(w http.ResponseWriter, r *http.Request) {
	st, done := s.store()
	defer done()
	id, err := strconv.ParseInt(r.URL.Query().Get("id"), 10, 64)
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	chip := seedChip{ID: id}
	err = st.DB().QueryRowContext(r.Context(), `SELECT title, coalesce(author, '') FROM work WHERE work_id = ?`, id).
		Scan(&chip.Title, &chip.Author)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.renderPartial(w, "seed-chip", chip)
}

func (s *Server) backlogPage(w http.ResponseWriter, r *http.Request) {
	st, done := s.store()
	defer done()
	f := readFilters(r.URL.Query())
	entries, err := backlog.List(r.Context(), st, backlog.Options{
		AvailableNow: f.AvailableNow, MaxWaitDays: f.maxWaitDays(), Limit: 300,
	})
	if err != nil {
		s.fail(w, err)
		return
	}
	s.render(w, r, "backlog", page{Nav: "backlog", Filters: f, Results: &rowsView{
		Rows: s.backlogRows(entries), Empty: "Nothing on your backlog matches."}})
}

// listenedView is the tracker history page.
type listenedView struct {
	Entries  []historyRow
	ThisYear map[string]int // marks this year, by kind label
}

type historyRow struct {
	Entry
	Label  string
	Latest bool // the item's most recent mark, which Undo removes
}

func (s *Server) listenedPage(w http.ResponseWriter, r *http.Request) {
	view := listenedView{ThisYear: map[string]int{}}
	seen := map[string]bool{}
	year := time.Now().Year()
	for _, e := range s.tracker.History() {
		view.Entries = append(view.Entries, historyRow{Entry: e, Label: markLabel(e.Kind), Latest: !seen[e.Item.ID]})
		seen[e.Item.ID] = true
		if e.On.Time().Year() == year {
			view.ThisYear[markLabel(e.Kind)]++
		}
	}
	s.render(w, r, "listened", page{Nav: "listened", Extra: view})
}

// mark records a mark for a catalog recording and returns its new actions.
func (s *Server) mark(w http.ResponseWriter, r *http.Request) {
	titleID, kind := r.FormValue("title_id"), contract.EventKind(r.FormValue("kind"))
	if !isMark(kind) {
		http.Error(w, "unknown mark", http.StatusBadRequest)
		return
	}
	st, done := s.store()
	item, err := catalogItem(r, st.DB(), titleID)
	done()
	if err != nil {
		s.fail(w, err)
		return
	}
	if err := s.tracker.Mark(item, kind); err != nil {
		s.fail(w, err)
		return
	}
	s.renderActions(w, titleID)
}

func (s *Server) undo(w http.ResponseWriter, r *http.Request) {
	titleID := r.FormValue("title_id")
	if err := s.tracker.Undo(titleID); err != nil {
		s.fail(w, err)
		return
	}
	if r.FormValue("from") == "listened" {
		w.Header().Set("HX-Refresh", "true") // the history page is simplest to redraw whole
		return
	}
	s.renderActions(w, titleID)
}

func (s *Server) renderActions(w http.ResponseWriter, titleID string) {
	rw := row{TitleID: titleID}
	if st, ok := s.tracker.Statuses()[titleID]; ok {
		rw.Mark = &st
	}
	s.renderPartial(w, "actions", rw)
}

// catalogItem describes a catalog recording as a shelf item, for the
// tracker. Its OverDrive ref ties it to the catalog record exactly.
func catalogItem(r *http.Request, db *sql.DB, titleID string) (contract.ShelfItem, error) {
	item := contract.ShelfItem{ID: titleID, Format: contract.Audiobook, Refs: map[string]string{contract.RefOverDrive: titleID}}
	var subtitle sql.NullString
	var authors, isbns []any
	err := db.QueryRowContext(r.Context(), `
		SELECT title, subtitle,
		       list_transform(list_filter(creators, lambda c: c.role = 'author'), lambda c: c.name),
		       isbns
		FROM catalog_title WHERE title_id = ? LIMIT 1`, titleID).Scan(&item.Title, &subtitle, &authors, &isbns)
	if err != nil {
		return item, fmt.Errorf("catalog title %s: %w", titleID, err)
	}
	item.Subtitle = subtitle.String
	for _, a := range authors {
		item.Authors = append(item.Authors, a.(string))
	}
	if len(item.Authors) == 0 {
		item.Authors = []string{"Unknown"}
	}
	for _, i := range isbns {
		item.ISBNs = append(item.ISBNs, i.(string))
	}
	return item, nil
}

// Rows from each kind of result.

func (s *Server) searchRows(results []search.Result) []row {
	marks := s.tracker.Statuses()
	var out []row
	for _, r := range results {
		out = append(out, s.withMark(marks, row{
			WorkID: r.WorkID, TitleID: r.TitleID, Title: r.Title, Subtitle: r.Subtitle, Author: r.Author,
			Narrator: r.Narrator, Length: hoursMinutes(r.DurationMin, r.Abridged), Available: r.Available,
			Availability: availability(r.Available, r.WaitDays, r.Holds), Mine: tierLabel(r.Tier), URL: r.URL,
		}))
	}
	return out
}

func (s *Server) recommendRows(recs []recommend.Rec) []row {
	marks := s.tracker.Statuses()
	var out []row
	for _, r := range recs {
		out = append(out, s.withMark(marks, row{
			WorkID: r.WorkID, TitleID: r.TitleID, Title: r.Title, Author: r.Author, Narrator: r.Narrator,
			Length: hoursMinutes(r.DurationMin, r.Abridged), Available: r.Available,
			Availability: availability(r.Available, r.WaitDays, r.Holds), Mine: tierLabel(r.Tier),
			Why: fmt.Sprintf("like %s (%s)", r.Like, r.SimilarityText()), URL: r.URL,
		}))
	}
	return out
}

func (s *Server) backlogRows(entries []backlog.Entry) []row {
	marks := s.tracker.Statuses()
	var out []row
	for _, e := range entries {
		out = append(out, s.withMark(marks, row{
			WorkID: e.WorkID, TitleID: e.TitleID, Title: e.Title, Author: e.Author, Narrator: e.Narrator,
			Length: hoursMinutes(e.DurationMin, e.Abridged), Available: e.Available,
			Availability: availability(e.Available, e.WaitDays, e.Holds), Mine: tierLabel(e.Tier), URL: e.URL,
		}))
	}
	return out
}

func (s *Server) withMark(marks map[string]Status, r row) row {
	if m, ok := marks[r.TitleID]; ok {
		r.Mark = &m
	}
	return r
}

// Rendering.

// render writes a whole page, or for an htmx request only the results.
func (s *Server) render(w http.ResponseWriter, r *http.Request, name string, p page) {
	t := s.tmpl[name]
	target := "layout"
	if r.Header.Get("HX-Request") == "true" && r.Header.Get("HX-Target") == "results" {
		target = "results"
	}
	var buf bytes.Buffer
	data := any(p)
	if target == "results" {
		data = p.Results
	}
	if err := t.ExecuteTemplate(&buf, target, data); err != nil {
		s.fail(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	buf.WriteTo(w)
}

// renderPartial writes one fragment for htmx to swap in.
func (s *Server) renderPartial(w http.ResponseWriter, name string, data any) {
	var buf bytes.Buffer
	if err := s.tmpl["search"].ExecuteTemplate(&buf, name, data); err != nil {
		s.fail(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	buf.WriteTo(w)
}

func (s *Server) fail(w http.ResponseWriter, err error) {
	log.Printf("error: %v", err)
	http.Error(w, "Something went wrong: "+err.Error(), http.StatusInternalServerError)
}

// Labels.

func isMark(k contract.EventKind) bool {
	for _, m := range marks {
		if m == k {
			return true
		}
	}
	return false
}

func markLabel(k contract.EventKind) string {
	switch k {
	case contract.Started:
		return "listening"
	case contract.Finished:
		return "finished"
	case contract.Abandoned:
		return "gave up"
	case contract.Wanted:
		return "want"
	}
	return string(k)
}

func tierLabel(tier string) string {
	switch tier {
	case "T2":
		return "read, own"
	case "T3":
		return "read"
	case "T4":
		return "own"
	case "T5":
		return "want"
	case "N":
		return "gave up"
	}
	return ""
}

func hoursMinutes(minutes sql.NullInt64, abridged bool) string {
	if !minutes.Valid {
		return ""
	}
	s := fmt.Sprintf("%dh%02d", minutes.Int64/60, minutes.Int64%60)
	if abridged {
		s += " (abridged)"
	}
	return s
}

func availability(available bool, waitDays, holds sql.NullInt64) string {
	switch {
	case available:
		return "now"
	case waitDays.Valid:
		return fmt.Sprintf("~%d days, %s", waitDays.Int64, plural(holds.Int64, "hold"))
	default:
		return "unknown"
	}
}

// plural writes a count and a noun, adding an "s" unless the count is 1.
func plural(n int64, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
