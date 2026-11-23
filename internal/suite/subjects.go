package suite

import (
	"context"
	"database/sql"
	_ "embed"

	"github.com/KevinNavarreteParra/recbook/internal/store"
)

//go:embed subjects.sql
var subjectsSQL string

// LoadSubjects fills in each book's subjects from the catalog.
func LoadSubjects(ctx context.Context, st *store.Store, books []Book) error {
	ids := make([]int64, 0, len(books))
	for _, b := range books {
		ids = append(ids, b.WorkID)
	}
	rows, err := st.DB().QueryContext(ctx, subjectsSQL, sql.Named("works", ids))
	if err != nil {
		return err
	}
	defer rows.Close()
	subjects := map[int64][]string{}
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return err
		}
		subjects[id] = append(subjects[id], name)
	}
	for i := range books {
		books[i].Subjects = subjects[books[i].WorkID]
	}
	return rows.Err()
}
