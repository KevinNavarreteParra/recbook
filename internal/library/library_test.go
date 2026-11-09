package library

import (
	"context"
	"testing"

	"github.com/KevinNavarreteParra/recbook/internal/demo"
	"github.com/KevinNavarreteParra/recbook/internal/resolve"
	"github.com/KevinNavarreteParra/recbook/internal/store"
)

func TestBuildPicksOneRecordingPerWork(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := demo.Load(ctx, st); err != nil {
		t.Fatal(err)
	}
	if _, err := resolve.Run(ctx, st, resolve.Options{}); err != nil {
		t.Fatal(err)
	}
	if err := Build(ctx, st); err != nil {
		t.Fatal(err)
	}

	tests := []struct{ title, wantID string }{
		{"The Brothers Karamazov", "9010"}, // available full text, over a waitlisted one and a dramatised one
		{"A Gentleman in Moscow", "9011"},  // full text with a wait, over a dramatized one available now
	}
	for _, tt := range tests {
		var id string
		var recordings int
		err := st.DB().QueryRow(`SELECT title_id, recordings FROM borrowable WHERE title = ?`, tt.title).Scan(&id, &recordings)
		if err != nil {
			t.Fatalf("%s: %v", tt.title, err)
		}
		if id != tt.wantID || recordings < 2 {
			t.Errorf("%s: offered %s of %d recordings, want %s", tt.title, id, recordings, tt.wantID)
		}
	}
}
