package overdrive

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/KevinNavarreteParra/recbook/internal/contract"
)

func TestToContract(t *testing.T) {
	raw, err := os.ReadFile("testdata/media.json")
	if err != nil {
		t.Fatal(err)
	}
	var m media
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	c := NewClient(Config{Library: "testlib"})
	title, avail := c.toContract(m)

	if err := contract.ValidateCatalog([]contract.CatalogTitle{title}, []contract.Availability{avail}); err != nil {
		t.Fatal(err)
	}
	check := func(field string, got, want any) {
		t.Helper()
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Errorf("%s = %v, want %v", field, got, want)
		}
	}
	check("title", title.Title, "The Lighthouse Keeper's Ledger")
	check("subtitle", title.Subtitle, "A Novel of the Northern Coast")
	check("format", title.Format, contract.Audiobook)
	check("creators", title.Creators, []contract.Creator{
		{Name: "Ada Fenwick", Role: contract.Author},
		{Name: "Tomas Reyes", Role: contract.Translator},
		{Name: "June Okafor", Role: contract.Narrator},
	})
	check("isbns", title.ISBNs, []string{"9798999900029"}) // the malformed mp3 ISBN is dropped
	check("duration", title.DurationMin, 9*60+42)          // 09:41:45 rounds up
	check("language", title.Language, "en")
	check("published", title.PublishedOn, contract.NewDay(2021, 3, 9))
	check("series", title.Series+" #"+title.SeriesIndex, "Northern Coast #2")
	check("subjects", len(title.Subjects), 4)
	check("bisac", title.Subjects[2], contract.Subject{Scheme: "bisac", Code: "FIC019000", Name: "Fiction / Literary"})
	check("refs", title.Refs[contract.RefOverDrive], "424242")
	check("url", title.URL, "https://libbyapp.com/library/testlib/everything/page-1/424242")
	check("availability", fmt.Sprint(avail.Available, avail.CopiesOwned, avail.Holds, *avail.WaitDays), "false 2 7 42")
}

func TestMinutes(t *testing.T) {
	for in, want := range map[string]int{"03:21:16": 201, "00:00:30": 1, "120:00:00": 7200, "": 0, "3:21": 0, "aa:bb:cc": 0} {
		if got := minutes(in); got != want {
			t.Errorf("minutes(%q) = %d, want %d", in, got, want)
		}
	}
}

// TestCrawlSplitsOversizedQueries crawls a fake library that is too big for
// one query, and checks that partitioning still reaches every title once.
func TestCrawlSplitsOversizedQueries(t *testing.T) {
	lib := newFakeLibrary(t, 20)
	c := NewClient(Config{Library: "test", BaseURL: lib.server.URL, PageSize: 2, MaxPages: 3, RequestsPerSecond: 1000, RetryDelay: time.Millisecond})

	var got []string
	res, err := c.Crawl(context.Background(), func(b Batch) error {
		for _, title := range b.Titles {
			got = append(got, title.ID)
			if !strings.HasPrefix(title.Description, "Full description") {
				t.Errorf("title %s has the truncated list description", title.ID)
			}
		}
		return contract.ValidateCatalog(b.Titles, b.Availability)
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	slices.Sort(got)
	if len(got) != 20 || len(slices.Compact(slices.Clone(got))) != 20 {
		t.Errorf("saved %d titles (%d distinct), want all 20 once each", len(got), len(slices.Compact(got)))
	}
	if res.Listed != 20 || res.Saved != 20 || res.Warning != "" {
		t.Errorf("result = %+v", res)
	}
	if lib.throttled.Load() == 0 {
		t.Error("the fake should have answered 429 at least once, to exercise retries")
	}
}

// fakeLibrary imitates the parts of Thunder the crawler uses: filters,
// facet counts, a page cap, bulk lookups, and a 429 on the first request.
type fakeLibrary struct {
	server    *httptest.Server
	titles    []fakeTitle
	requests  atomic.Int64
	throttled atomic.Int64
}

type fakeTitle struct {
	id, duration, maturity string
	subjects               []string
}

func newFakeLibrary(t *testing.T, n int) *fakeLibrary {
	lib := &fakeLibrary{}
	for i := range n {
		ft := fakeTitle{id: strconv.Itoa(1000 + i), maturity: "general", subjects: []string{"fiction"}}
		switch {
		case i < 4:
			ft.duration = "0-1"
		case i < 13: // 9 titles: too many for one query, but maturity splits them
			ft.duration = "1-2"
			if i%2 == 0 {
				ft.maturity = "teen"
			}
		default: // 7 titles, all the same maturity, so only subjects split them
			ft.duration = "2-3"
			ft.subjects = []string{"fiction", []string{"mystery", "romance"}[i%2]}
		}
		lib.titles = append(lib.titles, ft)
	}
	lib.server = httptest.NewServer(http.HandlerFunc(lib.serve))
	t.Cleanup(lib.server.Close)
	return lib
}

func (lib *fakeLibrary) serve(w http.ResponseWriter, r *http.Request) {
	if lib.requests.Add(1) == 1 {
		lib.throttled.Add(1)
		w.Header().Set("Retry-After", "0")
		http.Error(w, "slow down", http.StatusTooManyRequests)
		return
	}
	q := r.URL.Query()
	switch r.URL.Path {
	case "/v2/libraries/test/media/bulk":
		var items []map[string]any
		for _, id := range strings.Split(q.Get("titleIds"), ",") {
			items = append(items, record(id, "Full description of "+id))
		}
		json.NewEncoder(w).Encode(map[string]any{"items": items})

	case "/v2/libraries/test/media":
		var match []fakeTitle
		for _, ft := range lib.titles {
			if want := q.Get("audiobookDuration"); want != "" && ft.duration != want {
				continue
			}
			if want := q.Get("maturityLevel"); want != "" && ft.maturity != want {
				continue
			}
			if want := q.Get("subject"); want != "" && !slices.Contains(ft.subjects, want) {
				continue
			}
			match = append(match, ft)
		}
		perPage, _ := strconv.Atoi(q.Get("perPage"))
		page, _ := strconv.Atoi(q.Get("page"))
		page = min(page, 3) // like Thunder, never serve past the last page

		var items []map[string]any
		for i := (page - 1) * perPage; i < min(page*perPage, len(match)); i++ {
			items = append(items, record(match[i].id, "Full desc..."))
		}
		resp := map[string]any{"items": items, "totalItems": len(match)}
		if q.Get("includeFacets") == "true" {
			resp["facets"] = map[string]any{
				"appliedCount":      0, // not a facet, as in the real API
				"audiobookDuration": facetCounts(match, func(ft fakeTitle) []string { return []string{ft.duration} }),
				"maturityLevels":    facetCounts(match, func(ft fakeTitle) []string { return []string{ft.maturity} }),
				"subjects":          facetCounts(match, func(ft fakeTitle) []string { return ft.subjects }),
			}
		}
		json.NewEncoder(w).Encode(resp)

	default:
		http.NotFound(w, r)
	}
}

func record(id, description string) map[string]any {
	return map[string]any{"id": id, "title": "Title " + id, "description": description, "type": map[string]any{"id": "audiobook"}}
}

func facetCounts(titles []fakeTitle, values func(fakeTitle) []string) map[string]any {
	counts := map[string]int{}
	var order []string
	for _, ft := range titles {
		for _, v := range values(ft) {
			if counts[v] == 0 {
				order = append(order, v)
			}
			counts[v]++
		}
	}
	var items []map[string]any
	for _, v := range order {
		items = append(items, map[string]any{"id": v, "name": v, "totalItems": counts[v]})
	}
	return map[string]any{"items": items}
}
