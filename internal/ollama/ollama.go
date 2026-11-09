// Package ollama talks to a local Ollama server, which runs the embedding
// model on the GPU. It covers the few API calls recbook needs, and can
// start a server for the length of a job and stop it afterwards (Serve), so
// nothing holds the GPU between jobs.
package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"time"
)

// DefaultHost is where Serve listens and Client connects: this machine only.
const DefaultHost = "127.0.0.1:11434"

// Client calls one Ollama server.
type Client struct {
	BaseURL string // "http://127.0.0.1:11434"
	HTTP    *http.Client
}

// NewClient returns a client for the server at host ("127.0.0.1:11434").
func NewClient(host string) *Client {
	return &Client{BaseURL: "http://" + host, HTTP: &http.Client{Timeout: 10 * time.Minute}}
}

// Ready reports whether a server is answering.
func (c *Client) Ready(ctx context.Context) bool {
	var v struct {
		Version string `json:"version"`
	}
	return c.call(ctx, http.MethodGet, "/api/version", nil, &v) == nil && v.Version != ""
}

// Embed returns one unit-length vector per input, in order. The server
// truncates inputs longer than the model's context instead of failing.
func (c *Client) Embed(ctx context.Context, model string, inputs []string) ([][]float32, error) {
	req := map[string]any{"model": model, "input": inputs, "truncate": true}
	var resp struct {
		Embeddings [][]float32 `json:"embeddings"`
	}
	if err := c.call(ctx, http.MethodPost, "/api/embed", req, &resp); err != nil {
		return nil, err
	}
	if len(resp.Embeddings) != len(inputs) {
		return nil, fmt.Errorf("embed: sent %d inputs, got %d vectors", len(inputs), len(resp.Embeddings))
	}
	for _, v := range resp.Embeddings {
		normalize(v)
	}
	return resp.Embeddings, nil
}

// Pull downloads model if the server doesn't have it yet. It blocks until
// the download finishes.
func (c *Client) Pull(ctx context.Context, model string) error {
	if c.call(ctx, http.MethodPost, "/api/show", map[string]any{"model": model}, &struct{}{}) == nil {
		return nil // already there
	}
	var resp struct {
		Status string `json:"status"`
	}
	if err := c.call(ctx, http.MethodPost, "/api/pull", map[string]any{"model": model, "stream": false}, &resp); err != nil {
		return fmt.Errorf("pulling %s: %w", model, err)
	}
	if resp.Status != "success" {
		return fmt.Errorf("pulling %s: status %q", model, resp.Status)
	}
	return nil
}

// VRAMBytes is how much GPU memory the server's loaded models hold.
func (c *Client) VRAMBytes(ctx context.Context) (int64, error) {
	var resp struct {
		Models []struct {
			SizeVRAM int64 `json:"size_vram"`
		} `json:"models"`
	}
	if err := c.call(ctx, http.MethodGet, "/api/ps", nil, &resp); err != nil {
		return 0, err
	}
	var total int64
	for _, m := range resp.Models {
		total += m.SizeVRAM
	}
	return total, nil
}

func (c *Client) call(ctx context.Context, method, path string, body, into any) error {
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, r)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 500))
		return fmt.Errorf("%s %s: %s: %s", method, path, resp.Status, bytes.TrimSpace(msg))
	}
	return json.NewDecoder(resp.Body).Decode(into)
}

// normalize scales v to unit length, so a dot product is a cosine.
func normalize(v []float32) {
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	if sum == 0 {
		return
	}
	n := float32(1 / math.Sqrt(sum))
	for i := range v {
		v[i] *= n
	}
}
