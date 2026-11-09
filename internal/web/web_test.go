package web

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/KevinNavarreteParra/recbook/internal/contract"
	"github.com/KevinNavarreteParra/recbook/internal/demo"
	"github.com/KevinNavarreteParra/recbook/internal/embed"
	"github.com/KevinNavarreteParra/recbook/internal/library"
	"github.com/KevinNavarreteParra/recbook/internal/resolve"
	"github.com/KevinNavarreteParra/recbook/internal/search"
	"github.com/KevinNavarreteParra/recbook/internal/signals"
	"github.com/KevinNavarreteParra/recbook/internal/sources/jsonl"
	"github.com/KevinNavarreteParra/recbook/internal/store"
)

// flatEmbedder gives every text the same vector, which is enough to
// exercise the recommendation page.
type flatEmbedder struct{}

func (flatEmbedder) Embed(_ context.Context, _ string, inputs []string) ([][]float32, error) {
	out := make([][]float32, len(inputs))
	for i := range out {
		out[i] = []float32{1, 0}
	}
	return out, nil
}

// app builds the demo into a database, publishes it, and serves it.
func app(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	st, err := store.Open(ctx, filepath.Join(dir, "live.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := demo.Load(ctx, st); err != nil {
		t.Fatal(err)
	}
	titles, avail := demo.Catalog()
	for i := range titles {
		titles[i].Description = "A story about " + titles[i].Title + "."
	}
	// A hostile title, to check that everything is escaped.
	titles = append(titles, contract.CatalogTitle{ID: "666", Title: `<script>alert(1)</script> Spy Story`,
		Format: contract.Audiobook, Creators: []contract.Creator{{Name: "Mallory", Role: contract.Author}}})
	avail = append(avail, contract.Availability{TitleID: "666", Available: true, CopiesOwned: 1, CopiesAvailable: 1})
	if err := st.SaveCatalog(ctx, demo.Library, titles, avail, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := resolve.Run(ctx, st, resolve.Options{}); err != nil {
		t.Fatal(err)
	}
	for _, step := range []func(context.Context, *store.Store) error{
		func(ctx context.Context, st *store.Store) error { return signals.Run(ctx, st, signals.Options{}) },
		library.Build, search.Build,
	} {
		if err := step(ctx, st); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := embed.Run(ctx, st, flatEmbedder{}, embed.Options{}); err != nil {
		t.Fatal(err)
	}
	snapshot := filepath.Join(dir, "serve.duckdb")
	if err := st.Snapshot(ctx, snapshot); err != nil {
		t.Fatal(err)
	}

	trackerPath := filepath.Join(dir, "tracker.jsonl")
	srv, err := New(ctx, snapshot, trackerPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { srv.Close() })
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts, trackerPath
}

func get(t *testing.T, ts *httptest.Server, path string, headers ...string) string {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, ts.URL+path, nil)
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: %s\n%s", path, resp.Status, body)
	}
	return string(body)
}

func post(t *testing.T, ts *httptest.Server, path string, form url.Values, fromApp bool) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, ts.URL+path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if fromApp {
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		req.Header.Set("HX-Request", "true")
	}
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body)
}

func TestPages(t *testing.T) {
	ts, _ := app(t)

	home := get(t, ts, "/")
	if !strings.Contains(home, "From your backlog") || !strings.Contains(home, "The Brothers Karamazov") {
		t.Errorf("the landing page should show the backlog available now:\n%s", home)
	}

	page := get(t, ts, "/search?q=carre")
	if !strings.Contains(page, "<html") || !strings.Contains(page, "Smiley") {
		t.Errorf("a direct search should render the whole page with results")
	}
	partial := get(t, ts, "/search?q=carre", "HX-Request", "true", "HX-Target", "results")
	if strings.Contains(partial, "<html") || !strings.Contains(partial, "Smiley") {
		t.Errorf("an htmx search should render only the results")
	}

	hostile := get(t, ts, "/search?q=spy+story")
	if strings.Contains(hostile, "<script>alert") || !strings.Contains(hostile, "&lt;script&gt;") {
		t.Error("catalog text must be HTML-escaped")
	}

	for _, path := range []string{"/recommend", "/backlog", "/listened", "/static/htmx.min.js", "/seeds?q=tinker"} {
		get(t, ts, path)
	}
	if opts := get(t, ts, "/seeds?q=tinker"); !strings.Contains(opts, "on my shelf") {
		t.Errorf("seed suggestions should come from my shelf first:\n%s", opts)
	}
}

func TestRecommendPage(t *testing.T) {
	ts, _ := app(t)
	opts := get(t, ts, "/seeds?q=tinker+tailor")
	i := strings.Index(opts, "/seed-chip?id=")
	if i < 0 {
		t.Fatalf("no seed option:\n%s", opts)
	}
	id := strings.TrimSpace(strings.Split(opts[i+len("/seed-chip?id="):], `"`)[0])
	page := get(t, ts, "/similar?seed="+id)
	if !strings.Contains(page, "like Tinker Tailor Soldier Spy") || !strings.Contains(page, `name="seed" value="`+id+`"`) {
		t.Errorf("recommendations should say why, and keep the seed chosen:\n%s", page)
	}
}

func TestTracker(t *testing.T) {
	ts, trackerPath := app(t)
	form := url.Values{"title_id": {"9011"}, "kind": {"finished"}}

	if code, _ := post(t, ts, "/mark", form, false); code != http.StatusForbidden {
		t.Errorf("a POST not sent by the app should be refused, got %d", code)
	}

	code, body := post(t, ts, "/mark", form, true)
	if code != http.StatusOK || !strings.Contains(body, "finished") || !strings.Contains(body, "undo") {
		t.Fatalf("mark: %d\n%s", code, body)
	}
	if got := get(t, ts, "/listened"); !strings.Contains(got, "A Gentleman in Moscow") {
		t.Error("the history page should list the mark")
	}

	// The tracker file is a valid shelf in the contract's format.
	f, err := os.Open(trackerPath)
	if err != nil {
		t.Fatal(err)
	}
	shelf, err := jsonl.Read(f)
	f.Close()
	if err != nil || shelf.Validate() != nil || len(shelf.Events) != 1 || shelf.Items[0].Refs[contract.RefOverDrive] != "9011" {
		t.Fatalf("tracker file: %+v, %v", shelf, err)
	}

	if code, body := post(t, ts, "/undo", url.Values{"title_id": {"9011"}}, true); code != http.StatusOK || !strings.Contains(body, "want") {
		t.Errorf("undo should bring the buttons back: %d\n%s", code, body)
	}
	if code, _ := post(t, ts, "/mark", url.Values{"title_id": {"9011"}, "kind": {"loved"}}, true); code != http.StatusBadRequest {
		t.Errorf("an unknown mark should be refused, got %d", code)
	}
}
