package enrich

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/KevinNavarreteParra/recbook/internal/contract"
)

// GoogleBooks looks books up in the Google Books API. It works without an
// API key, but keyless requests share one daily quota with everyone else's
// and are often refused; a free key from a Google Cloud project has its
// own (1,000 a day).
type GoogleBooks struct {
	HTTP    *http.Client // see polite.NewClient
	Key     string       // API key; optional
	BaseURL string       // default https://www.googleapis.com
}

// Ref names Google Books by the contract's ref key for its volume ids.
func (g *GoogleBooks) Ref() string { return contract.RefGoogleBooks }

// Lookup asks for up to five volumes and takes the first with a
// description, which for a title search must also be the right book.
func (g *GoogleBooks) Lookup(ctx context.Context, q Query) (Book, bool, error) {
	search := "isbn:" + q.ISBN
	if q.ISBN == "" {
		search = fmt.Sprintf(`intitle:"%s" inauthor:"%s"`, searchTitle(q.Title), surname(q.Author))
	}
	params := url.Values{
		"q":          {search},
		"maxResults": {"5"},
		"printType":  {"books"},
		"fields":     {"items(id,volumeInfo(title,authors,description))"},
	}
	if g.Key != "" {
		params.Set("key", g.Key)
	}
	var res struct {
		Items []struct {
			ID         string `json:"id"`
			VolumeInfo struct {
				Title       string   `json:"title"`
				Authors     []string `json:"authors"`
				Description string   `json:"description"`
			} `json:"volumeInfo"`
		} `json:"items"`
	}
	err := getJSON(ctx, g.HTTP, base(g.BaseURL, "https://www.googleapis.com")+"/books/v1/volumes?"+params.Encode(), &res)
	if errors.Is(err, errNotFound) {
		return Book{}, false, nil // Google answers no match with an empty list, but just in case
	}
	if err != nil {
		return Book{}, false, err
	}
	for _, it := range res.Items {
		v := it.VolumeInfo
		if v.Description == "" || (q.ISBN == "" && !sameBook(q, v.Title, v.Authors)) {
			continue
		}
		return Book{ID: it.ID, Title: v.Title, Description: v.Description}, true, nil
	}
	return Book{}, false, nil
}

// OpenLibrary looks books up in Open Library. Descriptions belong to its
// works rather than its editions, so each lookup is two requests: find the
// work, then fetch it.
type OpenLibrary struct {
	HTTP    *http.Client // see polite.NewClient
	BaseURL string       // default https://openlibrary.org
}

// Ref names Open Library by the contract's ref key for its work ids.
func (o *OpenLibrary) Ref() string { return contract.RefOpenLibraryWork }

// Lookup finds the work by ISBN (an edition names its work) or by a title
// search checked against title and author, then reads its description.
func (o *OpenLibrary) Lookup(ctx context.Context, q Query) (Book, bool, error) {
	root := base(o.BaseURL, "https://openlibrary.org")

	var key string // "/works/OL45883W"
	if q.ISBN != "" {
		var edition struct {
			Works []struct {
				Key string `json:"key"`
			} `json:"works"`
		}
		err := getJSON(ctx, o.HTTP, root+"/isbn/"+url.PathEscape(q.ISBN)+".json", &edition)
		if errors.Is(err, errNotFound) {
			return Book{}, false, nil
		}
		if err != nil {
			return Book{}, false, err
		}
		if len(edition.Works) > 0 {
			key = edition.Works[0].Key
		}
	} else {
		params := url.Values{
			"title":  {searchTitle(q.Title)},
			"author": {surname(q.Author)},
			"limit":  {"5"},
			"fields": {"key,title,author_name"},
		}
		var res struct {
			Docs []struct {
				Key     string   `json:"key"`
				Title   string   `json:"title"`
				Authors []string `json:"author_name"`
			} `json:"docs"`
		}
		if err := getJSON(ctx, o.HTTP, root+"/search.json?"+params.Encode(), &res); err != nil {
			return Book{}, false, err
		}
		for _, d := range res.Docs {
			if sameBook(q, d.Title, d.Authors) {
				key = d.Key
				break
			}
		}
	}
	// The key goes into a URL path, so take only the shape expected.
	id, ok := strings.CutPrefix(key, "/works/")
	if !ok || id == "" || strings.ContainsAny(id, "/.?#") {
		return Book{}, false, nil
	}

	var work struct {
		Title       string `json:"title"`
		Description text   `json:"description"`
	}
	err := getJSON(ctx, o.HTTP, root+key+".json", &work)
	if errors.Is(err, errNotFound) {
		return Book{}, false, nil
	}
	if err != nil {
		return Book{}, false, err
	}
	if work.Description == "" {
		return Book{}, false, nil
	}
	return Book{ID: id, Title: work.Title, Description: string(work.Description)}, true, nil
}

// text is an Open Library text field, which comes either as a string or as
// {"type": "/type/text", "value": "..."}.
type text string

func (t *text) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		*t = text(s)
		return nil
	}
	var typed struct {
		Value string `json:"value"`
	}
	if err := json.Unmarshal(b, &typed); err != nil {
		return err
	}
	*t = text(typed.Value)
	return nil
}

// errNotFound is a 404: the service doesn't have what was asked for.
var errNotFound = errors.New("not found")

func getJSON(ctx context.Context, client *http.Client, u string, into any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return errNotFound
	case resp.StatusCode != http.StatusOK:
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		return fmt.Errorf("GET %s: %s: %s", u, resp.Status, body)
	}
	if err := json.NewDecoder(resp.Body).Decode(into); err != nil {
		return fmt.Errorf("GET %s: %w", u, err)
	}
	return nil
}

func base(configured, fallback string) string {
	if configured != "" {
		return strings.TrimSuffix(configured, "/")
	}
	return fallback
}
