package enrich

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// serve answers requests by path (and query, for searches) from bodies;
// anything else is a 404. It records what was asked.
func serve(t *testing.T, bodies map[string]string) (*httptest.Server, *[]string) {
	t.Helper()
	var asked []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = append(asked, r.URL.RequestURI())
		for prefix, body := range bodies {
			if strings.HasPrefix(r.URL.RequestURI(), prefix) {
				w.Write([]byte(body))
				return
			}
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv, &asked
}

func TestGoogleBooks(t *testing.T) {
	srv, asked := serve(t, map[string]string{
		"/books/v1/volumes?fields=items%28id%2CvolumeInfo%28title%2Cauthors%2Cdescription%29%29&maxResults=5&printType=books&q=isbn%3A9780679722762": `{
			"items": [{"id": "g1", "volumeInfo": {"title": "Politics Among Nations", "authors": ["Hans J. Morgenthau"], "description": "The classic."}}]}`,
		"/books/v1/volumes?fields=items%28id%2CvolumeInfo%28title%2Cauthors%2Cdescription%29%29&maxResults=5&printType=books&q=intitle": `{
			"items": [
				{"id": "wrong", "volumeInfo": {"title": "Politics Among Nations Today", "authors": ["Someone Else"], "description": "Not it."}},
				{"id": "bare", "volumeInfo": {"title": "Politics Among Nations", "authors": ["Hans Morgenthau"]}},
				{"id": "g2", "volumeInfo": {"title": "Politics Among Nations", "subtitle": "The Struggle", "authors": ["Hans Morgenthau"], "description": "Found by title."}}
			]}`,
	})
	g := &GoogleBooks{HTTP: srv.Client(), BaseURL: srv.URL}
	ctx := context.Background()

	b, ok, err := g.Lookup(ctx, Query{ISBN: "9780679722762"})
	if err != nil || !ok || b.ID != "g1" || b.Description != "The classic." {
		t.Errorf("by ISBN: %+v, %v, %v", b, ok, err)
	}

	// By title, the first volume with a description that is the right book.
	b, ok, err = g.Lookup(ctx, Query{Title: "Politics among nations: The struggle for power and peace", Author: "Morgenthau, Hans Joachim"})
	if err != nil || !ok || b.ID != "g2" {
		t.Errorf("by title: %+v, %v, %v", b, ok, err)
	}
	if last := (*asked)[len(*asked)-1]; !strings.Contains(last, "intitle%3A%22Politics+among+nations%22+inauthor%3A%22morgenthau%22") {
		t.Errorf("searched %s, want the main title and the surname", last)
	}

	// Nothing found is not an error.
	if _, ok, err := g.Lookup(ctx, Query{ISBN: "9780000000002"}); ok || err != nil {
		t.Errorf("unknown ISBN: %v, %v", ok, err)
	}
}

func TestOpenLibrary(t *testing.T) {
	srv, _ := serve(t, map[string]string{
		"/isbn/9780140449136.json": `{"title": "Crime and Punishment", "works": [{"key": "/works/OL1W"}]}`,
		"/works/OL1W.json":         `{"title": "Crime and Punishment", "description": {"type": "/type/text", "value": "A typed description."}}`,
		"/search.json":             `{"docs": [{"key": "/works/OL2W", "title": "Emile, or On Education", "author_name": ["Jean-Jacques Rousseau"]}]}`,
		"/works/OL2W.json":         `{"title": "Emile", "description": "A plain description."}`,
		"/isbn/9780000000019.json": `{"title": "Bad key", "works": [{"key": "/works/../../etc"}]}`,
		"/isbn/9780000000026.json": `{"title": "No description", "works": [{"key": "/works/OL3W"}]}`,
		"/works/OL3W.json":         `{"title": "No description"}`,
	})
	o := &OpenLibrary{HTTP: srv.Client(), BaseURL: srv.URL}
	ctx := context.Background()

	b, ok, err := o.Lookup(ctx, Query{ISBN: "9780140449136"})
	if err != nil || !ok || b.ID != "OL1W" || b.Description != "A typed description." {
		t.Errorf("by ISBN: %+v, %v, %v", b, ok, err)
	}
	b, ok, err = o.Lookup(ctx, Query{Title: "Emile: Or On Education", Author: "Rousseau, Jean-Jacques"})
	if err != nil || !ok || b.ID != "OL2W" || b.Description != "A plain description." {
		t.Errorf("by title: %+v, %v, %v", b, ok, err)
	}
	for _, isbn := range []string{"9780000000002", "9780000000019", "9780000000026"} {
		if b, ok, err := o.Lookup(ctx, Query{ISBN: isbn}); ok || err != nil {
			t.Errorf("ISBN %s: %+v, %v, %v; want nothing found", isbn, b, ok, err)
		}
	}
}

func TestSearchTitle(t *testing.T) {
	for in, want := range map[string]string{
		"Second Treatise of Government (Hackett Classics)":        "Second Treatise of Government",
		"Tyrants: History's 100 Most Evil Despots & Dictators":    "Tyrants",
		"The American Revolution: A History (Modern Library ...)": "The American Revolution",
		"Emile, or On Education":                                  "Emile, or On Education",
	} {
		if got := searchTitle(in); got != want {
			t.Errorf("searchTitle(%q) = %q, want %q", in, got, want)
		}
	}
}
