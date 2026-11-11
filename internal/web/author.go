package web

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/KevinNavarreteParra/recbook/internal/authors"
)

// authorView is the author page: the author, then the two sections of an
// author query (spec §8), each a results table.
type authorView struct {
	Author    authors.Author
	More      *rowsView
	Similar   []neighbourView
	Spellings []authors.Author
	Note      string // why there are no similar authors, when there are none
}

type neighbourView struct {
	authors.Neighbour
	Books *rowsView
}

// authorPage answers /author?name=John+le+Carré. Result rows link their
// author's name here. Any spelling finds the author (see authors.Find),
// and so does part of a name.
func (s *Server) authorPage(w http.ResponseWriter, r *http.Request) {
	st, done := s.store()
	defer done()
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	f := readFilters(r.URL.Query())
	p := page{Nav: "author", Query: name, Filters: f}

	found, err := authors.Find(r.Context(), st, name)
	if err != nil {
		s.fail(w, err)
		return
	}
	if len(found) == 0 {
		http.Error(w, fmt.Sprintf("No author matches %q.", name), http.StatusNotFound)
		return
	}

	res, err := authors.Lookup(r.Context(), st, found[0].ID, authors.Options{
		AvailableNow: f.AvailableNow, MaxWaitDays: f.maxWaitDays(), IncludeRead: f.IncludeRead,
	})
	if err != nil {
		s.fail(w, err)
		return
	}
	view := authorView{
		Author:    res.Author,
		More:      &rowsView{Rows: s.authorRows(res.More, false), Empty: "Nothing else of theirs to borrow with these filters."},
		Spellings: res.Spellings,
	}
	for _, n := range res.Similar {
		view.Similar = append(view.Similar, neighbourView{Neighbour: n, Books: &rowsView{Rows: s.authorRows(n.Books, true)}})
	}
	switch {
	case res.Embedded == 0:
		view.Note = "None of their books has been embedded yet."
	case len(res.Similar) == 0:
		view.Note = "No similar authors have books that match these filters."
	}
	p.Extra = view
	s.render(w, r, "author", p)
}

func (s *Server) authorRows(books []authors.Book, similar bool) []row {
	marks := s.tracker.Statuses()
	var out []row
	for _, b := range books {
		rw := row{
			WorkID: b.WorkID, TitleID: b.TitleID, Title: b.Title, Author: b.Author, Narrator: b.Narrator,
			Length: hoursMinutes(b.DurationMin, b.Abridged), Available: b.Available,
			Availability: availability(b.Available, b.WaitDays, b.Holds), Mine: tierLabel(b.Tier), URL: b.URL,
		}
		if similar {
			rw.Why = fmt.Sprintf("%.2f like this author", b.Similarity)
		}
		out = append(out, s.withMark(marks, rw))
	}
	return out
}
