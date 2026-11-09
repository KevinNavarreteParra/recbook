package store

import (
	"context"
	"database/sql"
	"fmt"
)

// Contract records are written in two steps. Each batch is first
// bulk-loaded into a temporary staging table with the target's columns
// (temp.stage_<table>), then moved into the real table by SQL inside one
// transaction. Row-by-row INSERTs into DuckDB manage a few hundred rows a
// second, and this manages hundreds of thousands. It also keeps the merge
// rules (replace? upsert? keep which columns?) in SQL, where they read
// plainly.

// stage is one table's rows for a staged write.
type stage struct {
	table string
	rows  Rows
}

// writeStaged loads each stage into temp.stage_<table>, then runs merge in a
// transaction on the same connection, where the staging tables are visible.
// Staging tables drop constraints, so the real table's constraints are
// checked when merge moves the rows.
func (s *Store) writeStaged(ctx context.Context, stages []stage, merge func(tx *sql.Tx) error) error {
	conn, err := s.db.Conn(ctx) // temporary tables belong to one connection
	if err != nil {
		return err
	}
	defer conn.Close()

	for _, st := range stages {
		create := fmt.Sprintf("CREATE OR REPLACE TEMP TABLE stage_%s AS FROM %s LIMIT 0", st.table, st.table)
		if _, err := conn.ExecContext(ctx, create); err != nil {
			return err
		}
		if err := appendRows(conn, "temp", "stage_"+st.table, st.rows); err != nil {
			return err
		}
	}

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := merge(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

// execAll runs statements in order, stopping at the first error.
func execAll(ctx context.Context, tx *sql.Tx, statements ...string) error {
	for _, stmt := range statements {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}
