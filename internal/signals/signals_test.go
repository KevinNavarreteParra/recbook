package signals

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/KevinNavarreteParra/recbook/internal/contract"
	"github.com/KevinNavarreteParra/recbook/internal/demo"
	"github.com/KevinNavarreteParra/recbook/internal/resolve"
	"github.com/KevinNavarreteParra/recbook/internal/store"
)

func TestRunOnTheDemo(t *testing.T) {
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
	if err := Run(ctx, st, Options{}); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		ref, why                      string
		tier, label, conf, provenance string // "" means NULL
	}{
		{"reading_app:ra-1", "finished, bought three months later", "T1", "1", "high", ""},
		{"reading_app:ra-2", "finished and owned, purchase undated", "T2", "1", "high", ""},
		{"reading_app:ra-6", "finished; the purchase date is a batch date, so the order is unknown", "T2", "1", "high", ""},
		{"reading_app:ra-3", "finished, borrowed off the list", "T3", "1", "high", "to_read_list"},
		{"owned_books:ob-3", "owned, never finished", "T4", "", "", ""},
		{"reading_app:ra-4", "only wanted", "T5", "", "", ""},
		{"reading_app:ra-5", "wanted, borrowed, ran out", "T5", "0", "low", "to_read_list"},
		{"library_loans:9002", "returned early; never listed, which isn't proof of browsing", "N", "0", "high", "unknown"},
		{"library_loans:9004", "renewed and never finished, listed months later", "T5", "", "", "search"},
		{"library_loans:9006", "borrowed long ago, no return recorded", "", "0", "low", "unknown"},
		{"library_loans:9005", "a hold only", "", "", "", ""},
	}
	for _, tt := range tests {
		var tier, label, conf, prov sql.NullString
		err := st.DB().QueryRow(`
			SELECT s.tier, CAST(s.label AS VARCHAR), s.label_confidence, s.provenance
			FROM work_signal s JOIN work_record w USING (work_id) WHERE w.ref = ?`, tt.ref).
			Scan(&tier, &label, &conf, &prov)
		if err != nil {
			t.Fatalf("%s: %v", tt.ref, err)
		}
		got := [4]string{tier.String, label.String, conf.String, prov.String}
		want := [4]string{tt.tier, tt.label, tt.conf, tt.provenance}
		if got != want {
			t.Errorf("%s (%s): tier, label, confidence, provenance = %q, want %q", tt.ref, tt.why, got, want)
		}
	}

	var batched int
	st.DB().QueryRow(`SELECT count(*) FROM history WHERE quality = 'user_batch' AND trusted_on IS NULL`).Scan(&batched)
	if batched != 6 {
		t.Errorf("the six purchases dated 2019-12-25 should be a batch; got %d", batched)
	}

	tiers, err := Tiers(ctx, st)
	if err != nil || len(tiers) != 6 || tiers[len(tiers)-1].Tier != "N" {
		t.Errorf("Tiers = %+v, %v", tiers, err)
	}
	rates, err := Provenance(ctx, st)
	if err != nil || len(rates) != 3 {
		t.Errorf("Provenance = %+v, %v", rates, err)
	}
}

// A loan that is still running has no outcome yet, so no label.
func TestRunningLoanHasNoLabel(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	shelf := &contract.Shelf{
		Items: []contract.ShelfItem{{ID: "1", Title: "On Loan", Authors: []string{"Ada Fenwick"}, Format: contract.Audiobook}},
		Events: []contract.ShelfEvent{{ItemID: "1", Kind: contract.Borrowed,
			On: contract.DayOf(time.Now().AddDate(0, 0, -5)), DateQuality: contract.DateSystem}},
	}
	if err := st.ReplaceShelf(ctx, "libby", "test", shelf); err != nil {
		t.Fatal(err)
	}
	if _, err := resolve.Run(ctx, st, resolve.Options{}); err != nil {
		t.Fatal(err)
	}
	if err := Run(ctx, st, Options{}); err != nil {
		t.Fatal(err)
	}
	var label sql.NullInt64
	if err := st.DB().QueryRow(`SELECT label FROM work_signal`).Scan(&label); err != nil {
		t.Fatal(err)
	}
	if label.Valid {
		t.Errorf("a five-day-old loan got label %d", label.Int64)
	}
}
