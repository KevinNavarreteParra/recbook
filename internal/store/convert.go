package store

import (
	duckdb "github.com/duckdb/duckdb-go/v2"

	"github.com/KevinNavarreteParra/recbook/internal/contract"
)

// Helpers that turn contract fields into values the DuckDB driver binds to
// the right column types. Empty optional fields become NULL rather than ""
// or 0, so SQL can tell "absent" from "blank".

func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullInt(n int) any {
	if n == 0 {
		return nil
	}
	return n
}

// list binds a string slice to a VARCHAR[] column; nil becomes [] because
// list columns are NOT NULL.
func list(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// isbn13s converts validated ISBNs to ISBN-13 and drops duplicates.
func isbn13s(isbns []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, s := range isbns {
		n, err := contract.NormalizeISBN(s)
		if err != nil || seen[n] { // Validate already rejected bad ones
			continue
		}
		seen[n] = true
		out = append(out, n)
	}
	return out
}

// refMap binds refs to a MAP(VARCHAR, VARCHAR) column.
func refMap(refs map[string]string) duckdb.Map {
	m := duckdb.Map{}
	for k, v := range refs {
		m[k] = v
	}
	return m
}

// creatorList binds creators to a STRUCT(name, role)[] column.
func creatorList(cs []contract.Creator) []map[string]any {
	out := []map[string]any{}
	for _, c := range cs {
		out = append(out, map[string]any{"name": c.Name, "role": string(c.Role)})
	}
	return out
}

// subjectList binds subjects to a STRUCT(scheme, code, name)[] column.
func subjectList(ss []contract.Subject) []map[string]any {
	out := []map[string]any{}
	for _, s := range ss {
		out = append(out, map[string]any{"scheme": s.Scheme, "code": nullString(s.Code), "name": s.Name})
	}
	return out
}

// day binds a Day to a DATE column: unknown becomes NULL. (Day implements
// driver.Valuer for queries, but the appender takes plain values.)
func day(d contract.Day) any {
	if d.IsZero() {
		return nil
	}
	return d.Time()
}
