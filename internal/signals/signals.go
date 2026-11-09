// Package signals turns my history into judgments (spec §2 and §10): which
// dates to trust, and for each work its label (did it hold my attention?),
// its signal tier (how strongly it says "my taste"), and, for borrows,
// whether it came off the to-read list or from browsing.
//
// It rebuilds these tables, after resolve has built the works:
//
//	history      every shelf event with its work and a judged date  (history.sql)
//	tier         the tiers, their meaning and seed weights          (signals.sql)
//	work_signal  per-work facts, label, provenance, tier, weight    (signals.sql)
package signals

import (
	"context"
	_ "embed"
	"fmt"

	"github.com/KevinNavarreteParra/recbook/internal/store"
)

var (
	//go:embed history.sql
	historySQL string
	//go:embed signals.sql
	signalsSQL string
)

// Options tunes the date rules.
type Options struct {
	// BatchMin is how many typed-in dates of one kind from one source must
	// fall on the same day before they count as a catch-up batch. Default 5.
	BatchMin int

	// OrderGapDays is how far apart two trusted dates must be to say which
	// came first. Default 30.
	OrderGapDays int

	// LoanDays is the longest a loan runs. A borrow older than this with no
	// recorded return is taken to be over. Default 21, Libby's longest loan.
	LoanDays int
}

// Run rebuilds the signal tables.
func Run(ctx context.Context, st *store.Store, opts Options) error {
	if opts.BatchMin == 0 {
		opts.BatchMin = 5
	}
	if opts.OrderGapDays == 0 {
		opts.OrderGapDays = 30
	}
	if opts.LoanDays == 0 {
		opts.LoanDays = 21
	}
	vars := store.Vars{"batch_min": opts.BatchMin, "order_gap_days": opts.OrderGapDays, "loan_days": opts.LoanDays}

	if err := st.RunScript(ctx, historySQL, vars); err != nil {
		return fmt.Errorf("signals history: %w", err)
	}
	if err := st.RunScript(ctx, signalsSQL, vars); err != nil {
		return fmt.Errorf("signals: %w", err)
	}
	return nil
}

// TierCount is how many works fall in one tier.
type TierCount struct {
	Tier, Meaning string
	SeedWeight    float64
	Works         int
}

// Tiers counts works per tier, including empty tiers.
func Tiers(ctx context.Context, st *store.Store) ([]TierCount, error) {
	rows, err := st.DB().QueryContext(ctx, `
		SELECT t.tier, t.meaning, t.seed_weight, count(s.work_id)
		FROM tier t LEFT JOIN work_signal s ON s.tier = t.tier
		GROUP BY t.tier, t.meaning, t.seed_weight
		ORDER BY t.tier = 'N', t.tier`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TierCount
	for rows.Next() {
		var c TierCount
		if err := rows.Scan(&c.Tier, &c.Meaning, &c.SeedWeight, &c.Works); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ProvenanceRate is how borrows from one provenance turned out: the first
// analysis in spec §10, testing whether to-read-list borrows get finished
// more often than ones found while browsing.
type ProvenanceRate struct {
	Provenance string
	Borrowed   int // works borrowed
	Labeled    int // ...whose outcome is known
	Finished   int // ...and that I finished
}

// Provenance returns finish rates by provenance.
func Provenance(ctx context.Context, st *store.Store) ([]ProvenanceRate, error) {
	rows, err := st.DB().QueryContext(ctx, `
		SELECT provenance, count(*), count(label), count(*) FILTER (label = 1)
		FROM work_signal WHERE provenance IS NOT NULL
		GROUP BY provenance ORDER BY provenance`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ProvenanceRate
	for rows.Next() {
		var p ProvenanceRate
		if err := rows.Scan(&p.Provenance, &p.Borrowed, &p.Labeled, &p.Finished); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
