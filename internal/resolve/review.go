package resolve

import (
	"context"
	"encoding/csv"
	"os"
	"path/filepath"

	"github.com/KevinNavarreteParra/recbook/internal/store"
)

// writeReview writes the match_review table to path as CSV, so weak and
// suspicious matches can be checked by eye and corrected with overrides.
func writeReview(ctx context.Context, st *store.Store, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	rows, err := st.DB().QueryContext(ctx, `
		SELECT reason, CAST(score AS VARCHAR), a, a_title, coalesce(a_author, ''),
		       coalesce(b, ''), coalesce(b_title, ''), coalesce(b_author, '')
		FROM match_review ORDER BY reason, score, a`)
	if err != nil {
		return err
	}
	defer rows.Close()

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	w.Write([]string{"reason", "score", "a", "a_title", "a_author", "b", "b_title", "b_author"})
	for rows.Next() {
		rec := make([]string, 8)
		ptrs := make([]any, len(rec))
		for i := range rec {
			ptrs[i] = &rec[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return err
		}
		w.Write(rec)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	w.Flush()
	return w.Error()
}
