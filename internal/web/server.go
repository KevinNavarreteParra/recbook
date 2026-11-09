// Package web is recbook's private web app: keyword search, "more like
// this", recommendations from seeds, the backlog, and a tracker for what
// I'm listening to. Pages are server-rendered Go templates; htmx swaps in
// results without full reloads.
//
// The app never writes the main database. It reads the copy that `build`
// and the nightly job publish (store.Snapshot), reopening it whenever a
// newer one appears, and it writes only the tracker file. It's meant to be
// reached over Tailscale and listens on localhost only.
package web

import (
	"context"
	"embed"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/KevinNavarreteParra/recbook/internal/store"
)

//go:embed templates/*.html
var templateFiles embed.FS

//go:embed static
var staticFiles embed.FS

// pages are the templates that render a whole page; each defines "content"
// inside the shared layout.
var pages = []string{"search", "recommend", "backlog", "listened"}

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
	snapshot string // path of the published database copy
	tracker  *Tracker
	tmpl     map[string]*template.Template // by page name

	mu    sync.RWMutex // guards st and stamp; requests hold it for reading
	st    *store.Store
	stamp time.Time // modification time of the copy st has open
}

// New opens the published copy and the tracker file.
func New(ctx context.Context, snapshot, trackerPath string) (*Server, error) {
	tracker, err := OpenTracker(trackerPath)
	if err != nil {
		return nil, err
	}
	s := &Server{snapshot: snapshot, tracker: tracker, tmpl: map[string]*template.Template{}}

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
	mux.HandleFunc("GET /seeds", s.seedOptions)
	mux.HandleFunc("GET /seed-chip", s.seedChip)
	mux.HandleFunc("GET /backlog", s.backlogPage)
	mux.HandleFunc("GET /listened", s.listenedPage)
	mux.HandleFunc("POST /mark", s.mark)
	mux.HandleFunc("POST /undo", s.undo)
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

// store returns the open copy and a func to call when done with it.
func (s *Server) store() (*store.Store, func()) {
	s.mu.RLock()
	return s.st, s.mu.RUnlock
}

// sameOriginWrites rejects any POST that a page of this app didn't send:
// browsers mark cross-site requests in Sec-Fetch-Site, and htmx adds
// HX-Request. That's the CSRF defence; there are no cookies to steal.
func sameOriginWrites(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost &&
			(r.Header.Get("Sec-Fetch-Site") != "same-origin" || r.Header.Get("HX-Request") != "true") {
			http.Error(w, "writes are only accepted from this app's own pages", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}
