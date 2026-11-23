package web

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/KevinNavarreteParra/recbook/internal/sources"
)

// Uploads are exports sent from elsewhere, to replace a source's last one.
// A Shortcut on my phone runs Reading List's "Export data" action each
// night and posts the CSV:
//
//	POST /inbox/readinglist
//	Recbook-Upload: shortcut
//
//	<the export>
//
// The app can't write the database, so it checks the export with its
// source's reader and saves it to the inbox, where the next build (the
// nightly run's) loads it. A file that isn't an export is refused here
// rather than failing the night.

// uploadHeader marks a request as an upload. A page on another site can't
// send it: a custom header needs a CORS preflight, which this app never
// answers. So it does for uploads what Sec-Fetch-Site does for the app's
// own pages (sameOriginWrites).
const uploadHeader = "Recbook-Upload"

// maxUpload bounds an upload. Reading List's export of 750 books is 1 MB.
const maxUpload = 64 << 20

func (s *Server) upload(w http.ResponseWriter, r *http.Request) {
	src, ok := sources.LookupShelf(r.PathValue("source"))
	if !ok {
		http.Error(w, "no such source", http.StatusNotFound)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxUpload))
	if err != nil {
		code := http.StatusBadRequest
		if _, tooBig := errors.AsType[*http.MaxBytesError](err); tooBig {
			code = http.StatusRequestEntityTooLarge
		}
		http.Error(w, err.Error(), code)
		return
	}
	shelf, err := src.Read(bytes.NewReader(body))
	if err == nil {
		err = shelf.Validate()
	}
	if err != nil {
		http.Error(w, fmt.Sprintf("%s: %v", src.Name, err), http.StatusBadRequest)
		return
	}
	if err := saveUpload(s.inbox, src.Name, body); err != nil {
		s.fail(w, err)
		return
	}
	fmt.Fprintf(w, "%s: %d items, %d events; the next nightly run loads them\n",
		src.Name, len(shelf.Items), len(shelf.Events))
}

// saveUpload writes an export to the inbox under its source's name,
// atomically, so a build never reads half of one.
func saveUpload(dir, source string, body []byte) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, source)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, body, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
