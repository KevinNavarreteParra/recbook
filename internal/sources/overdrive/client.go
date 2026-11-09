package overdrive

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"golang.org/x/time/rate"
)

// Config says which library to read and how politely.
type Config struct {
	Library string // the key in libbyapp.com/library/{key}

	// Formats are the format ids to crawl.
	// Default: audiobook-overdrive and audiobook-mp3.
	Formats []string

	// The rest have sensible defaults and exist mostly for tests.
	BaseURL           string  // default: https://thunder.api.overdrive.com
	RequestsPerSecond float64 // default: 2
	UserAgent         string  // default: recbook/<version> plus the repo URL
	HTTPClient        *http.Client
	PageSize          int           // default and Thunder's maximum: 100
	MaxPages          int           // pages Thunder serves per query: 200
	RetryDelay        time.Duration // first retry waits this long, then doubles; default 1s
}

// Client talks to the Thunder API for one library.
type Client struct {
	cfg  Config
	http *http.Client
}

// NewClient returns a client for cfg.Library, filling in defaults.
func NewClient(cfg Config) *Client {
	if len(cfg.Formats) == 0 {
		cfg.Formats = []string{"audiobook-overdrive", "audiobook-mp3"}
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://thunder.api.overdrive.com"
	}
	if cfg.RequestsPerSecond == 0 {
		cfg.RequestsPerSecond = 2
	}
	if cfg.UserAgent == "" {
		cfg.UserAgent = "recbook/0.1 (personal catalog index; +https://github.com/KevinNavarreteParra/recbook)"
	}
	if cfg.PageSize == 0 {
		cfg.PageSize = 100
	}
	if cfg.MaxPages == 0 {
		cfg.MaxPages = 200
	}
	if cfg.RetryDelay == 0 {
		cfg.RetryDelay = time.Second
	}

	base := http.DefaultTransport
	if cfg.HTTPClient != nil && cfg.HTTPClient.Transport != nil {
		base = cfg.HTTPClient.Transport
	}
	return &Client{
		cfg: cfg,
		http: &http.Client{
			Timeout: 60 * time.Second,
			Transport: &politeTransport{
				next:       base,
				limiter:    rate.NewLimiter(rate.Limit(cfg.RequestsPerSecond), 1),
				userAgent:  cfg.UserAgent,
				maxRetries: 4,
				retryDelay: cfg.RetryDelay,
			},
		},
	}
}

// search fetches one page of the media list. filters narrow the query
// (facet id → value); withFacets asks for facet counts, which only the
// crawl planner needs.
func (c *Client) search(ctx context.Context, filters url.Values, page, perPage int, withFacets bool) (*searchResult, error) {
	q := url.Values{}
	for k, vs := range filters {
		q[k] = vs
	}
	q.Set("format", strings.Join(c.cfg.Formats, ","))
	q.Set("page", strconv.Itoa(page))
	q.Set("perPage", strconv.Itoa(perPage))
	q.Set("includeFacets", strconv.FormatBool(withFacets))
	q.Set("sortBy", "title") // a stable order, so titles added mid-crawl shift fewer pages

	var res searchResult
	if err := c.getJSON(ctx, "/media?"+q.Encode(), &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// bulk fetches full records, descriptions included, for up to 100 ids.
func (c *Client) bulk(ctx context.Context, ids []string) ([]media, error) {
	q := url.Values{"titleIds": {strings.Join(ids, ",")}}
	var res bulkResult
	if err := c.getJSON(ctx, "/media/bulk?"+q.Encode(), &res); err != nil {
		return nil, err
	}
	return res.Items, nil
}

func (c *Client) getJSON(ctx context.Context, path string, into any) error {
	u := c.cfg.BaseURL + "/v2/libraries/" + url.PathEscape(c.cfg.Library) + path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 500))
		return fmt.Errorf("GET %s: %s: %s", u, resp.Status, body)
	}
	if err := json.NewDecoder(resp.Body).Decode(into); err != nil {
		return fmt.Errorf("GET %s: decoding: %w", u, err)
	}
	return nil
}
