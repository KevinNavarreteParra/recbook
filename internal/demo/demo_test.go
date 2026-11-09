package demo

import (
	"context"
	"testing"

	"github.com/KevinNavarreteParra/recbook/internal/store"
)

func TestLoad(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := Load(ctx, st); err != nil {
		t.Fatal(err)
	}
	var sources, titles int
	st.DB().QueryRow(`SELECT count(DISTINCT source) FROM shelf_item`).Scan(&sources)
	st.DB().QueryRow(`SELECT count(*) FROM catalog_title`).Scan(&titles)
	if sources != len(Shelves) || titles != 15 {
		t.Errorf("loaded %d shelf sources and %d titles", sources, titles)
	}
}
