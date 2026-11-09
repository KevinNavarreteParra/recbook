package overdrive

import (
	"io"
	"net/http"
	"strconv"
	"time"

	"golang.org/x/time/rate"
)

// politeTransport wraps an http.RoundTripper so every request identifies
// itself, waits its turn under the rate limit, and is retried with backoff
// when the server is overloaded (429) or failing (5xx).
//
// Wrapping the transport, rather than adding the logic to each call, means
// every request the client makes gets it.
type politeTransport struct {
	next       http.RoundTripper
	limiter    *rate.Limiter
	userAgent  string
	maxRetries int
	retryDelay time.Duration
}

func (t *politeTransport) RoundTrip(req *http.Request) (*http.Response, error) {
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
func (t *politeTransport) backoff(attempt int, resp *http.Response) time.Duration {
	if resp != nil {
		if secs, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && secs > 0 {
			return time.Duration(secs) * time.Second
		}
	}
	return t.retryDelay << attempt
}
