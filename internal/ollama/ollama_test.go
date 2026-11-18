package ollama

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"slices"
	"strings"
	"testing"
)

func fakeServer(t *testing.T) *Client {
	t.Helper()
	pulled := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		json.NewDecoder(r.Body).Decode(&req)
		switch r.URL.Path {
		case "/api/version":
			json.NewEncoder(w).Encode(map[string]string{"version": "0.0.0-test"})
		case "/api/embed":
			var out [][]float32
			for _, in := range req["input"].([]any) {
				out = append(out, []float32{float32(len(in.(string))), 0, 3})
			}
			json.NewEncoder(w).Encode(map[string]any{"embeddings": out})
		case "/api/show":
			if !pulled {
				http.Error(w, `{"error":"model not found"}`, http.StatusNotFound)
				return
			}
			w.Write([]byte(`{}`))
		case "/api/pull":
			pulled = true
			w.Write([]byte(`{"status":"success"}`))
		case "/api/ps":
			w.Write([]byte(`{"models":[{"size_vram":100},{"size_vram":23}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return &Client{BaseURL: srv.URL, HTTP: srv.Client()}
}

func TestClient(t *testing.T) {
	ctx := context.Background()
	c := fakeServer(t)

	if !c.Ready(ctx) {
		t.Fatal("server should be ready")
	}
	vecs, err := c.Embed(ctx, "m", []string{"abcd", ""})
	if err != nil {
		t.Fatal(err)
	}
	// [4, 0, 3] has length 5, so it normalizes to [0.8, 0, 0.6].
	if math.Abs(float64(vecs[0][0])-0.8) > 1e-6 || math.Abs(float64(vecs[0][2])-0.6) > 1e-6 {
		t.Errorf("vectors should be unit length: %v", vecs[0])
	}
	if err := c.Pull(ctx, "m"); err != nil {
		t.Fatal(err)
	}
	if err := c.Pull(ctx, "m"); err != nil {
		t.Errorf("a second pull should be a no-op: %v", err)
	}
	if n, err := c.VRAMBytes(ctx); err != nil || n != 123 {
		t.Errorf("VRAMBytes = %d, %v", n, err)
	}
}

func TestServeUsesARunningServer(t *testing.T) {
	c := fakeServer(t)
	host := strings.TrimPrefix(c.BaseURL, "http://")
	s, err := Serve(context.Background(), "/nonexistent/ollama", host)
	if err != nil {
		t.Fatalf("with a server already up, Serve shouldn't start one: %v", err)
	}
	s.Stop() // must not touch a server it didn't start
	if !c.Ready(context.Background()) {
		t.Error("Stop shut down a server Serve didn't start")
	}
}

func TestServeCPURefusesARunningServer(t *testing.T) {
	c := fakeServer(t)
	host := strings.TrimPrefix(c.BaseURL, "http://")
	if _, err := ServeCPU(context.Background(), "/nonexistent/ollama", host); err == nil {
		t.Error("ServeCPU should refuse a server it didn't start: it may be on the GPU")
	}
}

func TestWithoutGPUHidesTheGPU(t *testing.T) {
	if _, err := exec.LookPath("bwrap"); err != nil {
		t.Skip("no bubblewrap here")
	}
	out, err := withoutGPU("sh", "-c", "ls /dev; nice").Output()
	if err != nil {
		t.Skipf("bubblewrap can't make a sandbox here (no user namespaces?): %v", err)
	}
	lines := strings.Fields(string(out))
	for _, name := range lines {
		if strings.HasPrefix(name, "nvidia") || name == "dri" || name == "kfd" {
			t.Errorf("the sandbox's /dev has %s: %q", name, lines)
		}
	}
	if !slices.Contains(lines, "null") {
		t.Errorf("the sandbox's /dev should still have the basics: %q", lines)
	}
	if niceness := lines[len(lines)-1]; niceness != "19" {
		t.Errorf("niceness %s, want 19", niceness)
	}
}
