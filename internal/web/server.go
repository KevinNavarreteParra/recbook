// Package web is recbook's private web app: keyword search, "more like
// this", recommendations from seeds or from my whole history ("for you"),
// author pages, the backlog, and a tracker for what I'm listening to. Pages are server-rendered Go
// templates; htmx swaps in results without full reloads.
//
// The app never writes the main database. It reads the copy that `build`
// and the nightly job publish (store.Snapshot), reopening it whenever a
// newer one appears, and it writes only the tracker file and the exports
// uploaded to it, which the next build loads. It's meant to be reached
// over Tailscale and listens on localhost only.
package web

import (
	"context"
	"embed"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	vectors "github.com/KevinNavarreteParra/recbook/internal/embed"
	"github.com/KevinNavarreteParra/recbook/internal/graph"
	"github.com/KevinNavarreteParra/recbook/internal/recommend"
	"github.com/KevinNavarreteParra/recbook/internal/store"
)

//go:embed templates/*.html
var templateFiles embed.FS

//go:embed static
var staticFiles embed.FS

// pages are the templates that render a whole page; each defines "content"
// inside the shared layout.
var pages = []string{"search", "recommend", "foryou", "backlog", "history", "author"}

// templateFuncs are the helpers templates can call.
var templateFuncs = template.FuncMap{
	"markLabel": markLabel,
	// marks lists the tracker buttons every book gets.
	"marks": func() []struct{ Kind, Label string } {
		var out []struct{ Kind, Label string }
		for _, k := range marks {
			out = append(out, struct{ Kind, Label string }{string(k), markLabel(k)})
		}
		return out
	},
}

// Server serves the app.
type Server struct {
	snapshot  string   // path of the published database copy
	languages []string // search, recommendations and author pages keep recordings in these; nil for any
	tracker   *Tracker
	inbox     string                        // where uploaded exports wait for the next build
	tmpl      map[string]*template.Template // by page name

	mu    sync.RWMutex // guards st and stamp; requests hold it for reading
	st    *store.Store
	stamp time.Time // modification time of the copy st has open

	graphMu    sync.Mutex   // guards graph and graphStamp
	graph      *graph.Graph // the graph view of the copy published at graphStamp
	graphStamp time.Time
}

// New opens the published copy and the tracker file; uploaded exports go
// to the inbox directory. Search, recommendations and author pages show
// only recordings in languages (ISO 639-1 codes, "en"), or in any language
// if it's nil.
func New(ctx context.Context, snapshot, trackerPath, inbox string, languages []string) (*Server, error) {
	tracker, err := OpenTracker(trackerPath)
	if err != nil {
		return nil, err
	}
	s := &Server{snapshot: snapshot, languages: languages, tracker: tracker, inbox: inbox,
		tmpl: map[string]*template.Template{}}

	base := template.Must(template.New("").Funcs(templateFuncs).
		ParseFS(templateFiles, "templates/layout.html", "templates/partials.html"))
	for _, page := range pages {
		s.tmpl[page] = template.Must(template.Must(base.Clone()).ParseFS(templateFiles, "templates/"+page+".html"))
	}
	if err := s.reopen(ctx); err != nil {
		return nil, err
	}
	return s, nil
}

// Handler returns the app's routes.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.searchPage)
	mux.HandleFunc("GET /search", s.searchPage)
	mux.HandleFunc("GET /recommend", s.recommendPage)
	mux.HandleFunc("GET /similar", s.recommendPage)
	mux.HandleFunc("GET /foryou", s.forYouPage)
	mux.HandleFunc("GET /seeds", s.seedOptions)
	mux.HandleFunc("GET /seed-chip", s.seedChip)
	mux.HandleFunc("GET /backlog", s.backlogPage)
	mux.HandleFunc("GET /author", s.authorPage)
	mux.HandleFunc("GET /history", s.historyPage)
	mux.Handle("GET /listened", http.RedirectHandler("/history", http.StatusMovedPermanently)) // its old name
	mux.HandleFunc("POST /mark", s.mark)
	mux.HandleFunc("POST /undo", s.undo)
	mux.HandleFunc("POST /inbox/{source}", s.upload)
	static, _ := fs.Sub(staticFiles, "static")
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(static)))
	return sameOriginWrites(mux)
}

// Watch reopens the published copy whenever a newer one appears, until
// ctx is done.
func (s *Server) Watch(ctx context.Context) {
	tick := time.NewTicker(30 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			info, err := os.Stat(s.snapshot)
			if err != nil || !info.ModTime().After(s.currentStamp()) {
				continue
			}
			if err := s.reopen(ctx); err != nil {
				log.Printf("reopening %s: %v", s.snapshot, err)
			} else {
				log.Printf("now serving the copy published at %s", info.ModTime().Format(time.DateTime))
			}
		}
	}
}

// Close closes the database.
func (s *Server) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.st.Close()
}

func (s *Server) reopen(ctx context.Context) error {
	info, err := os.Stat(s.snapshot)
	if err != nil {
		return err
	}
	st, err := store.OpenReadOnly(ctx, s.snapshot)
	if err != nil {
		return err
	}
	s.mu.Lock()
	old := s.st
	s.st, s.stamp = st, info.ModTime()
	s.mu.Unlock()
	if old != nil {
		old.Close() // the lock waited for requests using it to finish
	}
	return nil
}

func (s *Server) currentStamp() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.stamp
}

// graphOf returns the graph view of the open copy st, for recommendations
// whose blend uses it, loading it the first time a request needs it (a few
// seconds) and keeping it until a newer copy is published. It's nil if the
// default blend doesn't use the graph. The caller holds s.mu for reading.
func (s *Server) graphOf(ctx context.Context, st *store.Store) (*graph.Graph, error) {
	if recommend.DefaultBlend[recommend.Graph] == 0 {
		return nil, nil
	}
	s.graphMu.Lock()
	defer s.graphMu.Unlock()
	if s.graph == nil || !s.graphStamp.Equal(s.stamp) {
		g, err := graph.Load(ctx, st, vectors.DefaultModel, graph.DefaultWeights)
		if err != nil {
			return nil, err
		}
		s.graph, s.graphStamp = g, s.stamp
	}
	return s.graph, nil
}

// store returns the open copy and a func to call when done with it.
func (s *Server) store() (*store.Store, func()) {
	s.mu.RLock()
	return s.st, s.mu.RUnlock
}

// sameOriginWrites rejects any POST that a page of this app didn't send:
// browsers mark cross-site requests in Sec-Fetch-Site, and htmx adds
// HX-Request. That's the CSRF defence; there are no cookies to steal.
// Uploads come from a Shortcut, not a page, and carry uploadHeader instead.
func sameOriginWrites(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := r.Header.Get("Sec-Fetch-Site") == "same-origin" && r.Header.Get("HX-Request") == "true"
		upload := strings.HasPrefix(r.URL.Path, "/inbox/") && r.Header.Get(uploadHeader) != ""
		if r.Method == http.MethodPost && !page && !upload {
			msg := "writes are only accepted from this app's own pages"
			if strings.HasPrefix(r.URL.Path, "/inbox/") {
				msg = "an upload needs a " + uploadHeader + " header"
			}
			http.Error(w, msg, http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}
