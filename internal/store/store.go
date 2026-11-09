// Package store owns the DuckDB database: opening it, keeping its schema up
// to date, and writing contract records into it.
//
// The database has two layers. Contract tables (migrations/001_contract.sql)
// hold what sources reported, and only this package writes them. Derived
// tables are rebuilt from scratch by packages like resolve and signals, each
// from SQL that lives next to the Go that runs it, via RunScript.
package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	_ "github.com/duckdb/duckdb-go/v2" // registers the "duckdb" database/sql driver
)

// Store is an open recbook database.
type Store struct {
	db *sql.DB
}

// Open opens the database at path, creating it if needed, and applies any
// migrations it hasn't seen. An empty path opens a throwaway in-memory
// database, which is what tests use.
func Open(ctx context.Context, path string) (*Store, error) {
	if path != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, err
		}
	}
	db, err := sql.Open("duckdb", path)
	if err != nil {
		return nil, err
	}
	if err := migrate(ctx, db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrating %s: %w", path, err)
	}
	return &Store{db: db}, nil
}

// DB returns the underlying connection pool, for queries.
func (s *Store) DB() *sql.DB { return s.db }

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// Vars are values a SQL script reads with getvariable('name').
type Vars map[string]any

// RunScript runs a SQL script of one or more statements in a single
// transaction, after setting vars as DuckDB variables. Scripts use
// variables instead of ? placeholders because DuckDB doesn't take
// placeholders in DDL such as CREATE TABLE ... AS.
func (s *Store) RunScript(ctx context.Context, script string, vars Vars) error {
	return s.inTx(ctx, func(tx *sql.Tx) error {
		for _, name := range sortedKeys(vars) {
			lit, err := sqlLiteral(vars[name])
			if err != nil {
				return fmt.Errorf("variable %s: %w", name, err)
			}
			if _, err := tx.ExecContext(ctx, fmt.Sprintf("SET VARIABLE %s = %s", name, lit)); err != nil {
				return err
			}
		}
		_, err := tx.ExecContext(ctx, script)
		return err
	})
}

// inTx runs fn in a transaction, committing if it returns nil and rolling
// back otherwise.
func (s *Store) inTx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

//go:embed migrations/*.sql
var migrationFiles embed.FS

// migrate applies, in name order, each file in migrations/ that the
// database hasn't recorded yet. Each file runs in its own transaction. To
// change the schema, add a file; never edit one that has shipped.
func migrate(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migration (
		name VARCHAR PRIMARY KEY, applied_at TIMESTAMP DEFAULT current_timestamp)`); err != nil {
		return err
	}
	names, err := fs.Glob(migrationFiles, "migrations/*.sql")
	if err != nil {
		return err
	}
	slices.Sort(names)

	for _, name := range names {
		var applied bool
		err := db.QueryRowContext(ctx, `SELECT count(*) > 0 FROM schema_migration WHERE name = ?`, name).Scan(&applied)
		if err != nil || applied {
			if err != nil {
				return err
			}
			continue
		}
		body, err := migrationFiles.ReadFile(name)
		if err != nil {
			return err
		}
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, string(body)); err != nil {
			tx.Rollback()
			return fmt.Errorf("%s: %w", name, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migration (name) VALUES (?)`, name); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// sqlLiteral renders a script variable as SQL. Only numbers, booleans and
// strings are allowed, which is all scripts need.
func sqlLiteral(v any) (string, error) {
	switch v := v.(type) {
	case int, int64, float64, bool:
		return fmt.Sprint(v), nil
	case string:
		return "'" + strings.ReplaceAll(v, "'", "''") + "'", nil
	default:
		return "", fmt.Errorf("unsupported type %T", v)
	}
}

func sortedKeys(m Vars) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
