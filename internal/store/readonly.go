package store

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
)

// OpenReadOnly opens a published copy of the database (see Snapshot) for
// serving. It applies no migrations, and it locks DuckDB down: once the
// full-text search extension is loaded, queries can no longer read files,
// fetch URLs or load extensions, and that setting can't be undone. A bug
// in a query then can't reach anything outside the database.
func OpenReadOnly(ctx context.Context, path string) (*Store, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	db, err := sql.Open("duckdb", path+"?access_mode=READ_ONLY")
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
