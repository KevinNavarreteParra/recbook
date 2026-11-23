package store

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"time"
)

// OpenReadOnly opens a published copy of the database (see Snapshot) for
// serving. It applies no migrations, and it locks DuckDB down: once the
// full-text search extension is loaded, queries can no longer read files,
// fetch URLs or load extensions, and that setting can't be undone. A bug
// in a query then can't reach anything outside the database.
//
// It opens the copy through a hard link of its own, removed once the copy
// is open (an open file stays readable without a name). The DuckDB driver
// keeps one database per path in a process and hands it to the next open
// of that path, so opening the published path again while the last copy
// is open, as the web app does when a newer one replaces it, would get the
// last copy back, with its settings already locked.
func OpenReadOnly(ctx context.Context, path string) (*Store, error) {
	link := fmt.Sprintf("%s.%d", path, time.Now().UnixNano())
	if err := os.Link(path, link); err != nil {
		return nil, err
	}
	defer os.Remove(link)
	db, err := sql.Open("duckdb", link+"?access_mode=READ_ONLY")
	if err != nil {
		return nil, err
	}
	for _, stmt := range []string{
		"LOAD fts",
		"SET enable_external_access = false",
		"SET lock_configuration = true",
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			db.Close()
			return nil, fmt.Errorf("%s: %w", stmt, err)
		}
	}
	return &Store{db: db}, nil
}

// Snapshot publishes a copy of the database at dest for a reader such as
// the web server, which can't share the live file: DuckDB lets only one
// process open a database that another is writing. It checkpoints so the
// file holds everything, copies it beside dest, and renames it into
// place, so a reader never sees a half-written copy. Call it when nothing
// else is writing.
func (s *Store) Snapshot(ctx context.Context, dest string) error {
	if s.path == "" {
		return fmt.Errorf("an in-memory database has no file to publish")
	}
	if _, err := s.db.ExecContext(ctx, "CHECKPOINT"); err != nil {
		return err
	}
	src, err := os.Open(s.path)
	if err != nil {
		return err
	}
	defer src.Close()

	tmp := dest + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, src); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, dest)
}
