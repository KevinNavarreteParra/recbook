package resolve

import (
	"context"

	"github.com/KevinNavarreteParra/recbook/internal/store"
	"github.com/KevinNavarreteParra/recbook/internal/textkey"
)

// writeKeys computes every record's matching keys in Go, where the
// normalization rules are readable and unit-tested, and loads them into
// record_key so the SQL that finds matches can join on them.
func writeKeys(ctx context.Context, st *store.Store) error {
	type rec struct{ ref, title, author string }
	var recs []rec

	rows, err := st.DB().QueryContext(ctx, `
		SELECT ref, title || coalesce(': ' || subtitle, ''), coalesce(author, '') FROM record`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var r rec
		if err := rows.Scan(&r.ref, &r.title, &r.author); err != nil {
			rows.Close()
			return err
		}
		recs = append(recs, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	return st.AppendRows(ctx, "record_key", func(add func(...any) error) error {
		for _, r := range recs {
			author := textkey.Author(r.author)
			if textkey.IsPlaceholderAuthor(author) {
				author = "" // "Anonymous" says nothing about which book it is
			}
			if err := add(r.ref, textkey.Title(r.title), textkey.MainTitle(r.title), author); err != nil {
				return err
			}
		}
		return nil
	})
}
