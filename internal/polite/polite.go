// Package polite makes HTTP clients that behave well toward the free
// services recbook reads from (OverDrive's Thunder API, Google Books, Open
// Library): every request names the client, waits its turn under a rate
// limit, and backs off when the server is overloaded or failing.
//
// The behaviour lives in an http.RoundTripper, rather than in each call,
// so that every request a client makes gets it.
package polite

import (
	"io"
	"net/http"
	"strconv"
	"time"

	"golang.org/x/time/rate"
)

// Config tunes a client. Zero values get the defaults noted.
type Config struct {
	UserAgent         string            // required: who is asking
	RequestsPerSecond float64           // default 1
	RetryDelay        time.Duration     // first retry waits this long, then doubles; default 1s
	MaxRetries        int               // default 4
	Timeout           time.Duration     // per request, retries included; default 60s
	Base              http.RoundTripper // what actually sends requests; default http.DefaultTransport
}

// NewClient returns an http.Client that sends every request politely.
func NewClient(cfg Config) *http.Client {
	if cfg.RequestsPerSecond == 0 {
		cfg.RequestsPerSecond = 1
	}
	if cfg.RetryDelay == 0 {
		cfg.RetryDelay = time.Second
	}
	if cfg.MaxRetries == 0 {
		cfg.MaxRetries = 4
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 60 * time.Second
	}
	if cfg.Base == nil {
		cfg.Base = http.DefaultTransport
	}
	return &http.Client{
		Timeout: cfg.Timeout,
		Transport: &transport{
			next:       cfg.Base,
			limiter:    rate.NewLimiter(rate.Limit(cfg.RequestsPerSecond), 1),
			userAgent:  cfg.UserAgent,
			maxRetries: cfg.MaxRetries,
			retryDelay: cfg.RetryDelay,
		},
	}
}

// transport wraps an http.RoundTripper so every request identifies itself,
// waits its turn under the rate limit, and is retried with backoff when the
// server is overloaded (429) or failing (5xx).
type transport struct {
	next       http.RoundTripper
	limiter    *rate.Limiter
	userAgent  string
	maxRetries int
	retryDelay time.Duration
}

func (t *transport) RoundTrip(req *http.Request) (*http.Response, error) {
	// RoundTrip must not modify the caller's request.
	req = req.Clone(req.Context())
	req.Header.Set("User-Agent", t.userAgent)
	req.Header.Set("Accept", "application/json")

	for attempt := 0; ; attempt++ {
		if err := t.limiter.Wait(req.Context()); err != nil {
			return nil, err
		}
		resp, err := t.next.RoundTrip(req)
		if attempt == t.maxRetries || !shouldRetry(resp, err) {
			return resp, err
		}

		wait := t.backoff(attempt, resp)
		if resp != nil {
			io.Copy(io.Discard, resp.Body) // so the connection can be reused
			resp.Body.Close()
		}
		select {
		case <-time.After(wait):
		case <-req.Context().Done():
			return nil, req.Context().Err()
		}
	}
}

// shouldRetry reports whether a failed request is worth trying again.
// These are all GETs, so repeating one is safe.
func shouldRetry(resp *http.Response, err error) bool {
	if err != nil {
		return true // network error
	}
	return resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500
}

// backoff is how long to wait before retry number attempt+1: whatever the
// server asked for in Retry-After, otherwise retryDelay doubled each time.
func (t *transport) backoff(attempt int, resp *http.Response) time.Duration {
	if resp != nil {
		if secs, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && secs > 0 {
			return time.Duration(secs) * time.Second
		}
	}
	return t.retryDelay << attempt
}
