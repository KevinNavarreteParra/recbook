package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/KevinNavarreteParra/recbook/internal/contract"
)

func openTest(t *testing.T) *Store {
	t.Helper()
	s, err := Open(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func count(t *testing.T, s *Store, query string, args ...any) int {
	t.Helper()
	var n int
	if err := s.DB().QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n
}

func TestReopenDoesNotReapplyMigrations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.duckdb")
	for range 2 {
		s, err := Open(context.Background(), path)
		if err != nil {
			t.Fatal(err)
		}
		s.Close()
	}
}

func TestReplaceShelf(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	shelf := &contract.Shelf{
		Items: []contract.ShelfItem{
			{ID: "a", Title: "Dune", Authors: []string{"Frank Herbert"}, Format: contract.Print,
				ISBNs: []string{"0-441-17271-7"}, Refs: map[string]string{contract.RefGoodreads: "44767458"}},
			{ID: "b", Title: "Emma", Authors: []string{"Jane Austen"}, Format: contract.Audiobook},
		},
		Events: []contract.ShelfEvent{
			{ItemID: "a", Kind: contract.Finished, On: contract.NewDay(2020, 1, 5), DateQuality: contract.DateUser},
			{ItemID: "b", Kind: contract.Wanted},
		},
	}
	if err := s.ReplaceShelf(ctx, "demo", "a.jsonl", shelf); err != nil {
		t.Fatal(err)
	}

	if got := count(t, s, `SELECT count(*) FROM shelf_item WHERE isbns = ['9780441172719']`); got != 1 {
		t.Errorf("ISBN-10 should be stored as ISBN-13; matching rows = %d", got)
	}
	if got := count(t, s, `SELECT count(*) FROM shelf_event WHERE date_quality = 'missing' AND happened_on IS NULL`); got != 1 {
		t.Errorf("undated event should be stored as missing; rows = %d", got)
	}
	if got := count(t, s, `SELECT count(*) FROM shelf_item WHERE refs['goodreads'] = '44767458'`); got != 1 {
		t.Errorf("refs not stored as a map; rows = %d", got)
	}

	// A second ingest replaces the first rather than adding to it.
	shelf.Items, shelf.Events = shelf.Items[:1], shelf.Events[:1]
	if err := s.ReplaceShelf(ctx, "demo", "b.jsonl", shelf); err != nil {
		t.Fatal(err)
	}
	if got := count(t, s, `SELECT count(*) FROM shelf_item`); got != 1 {
		t.Errorf("after re-ingest: %d items, want 1", got)
	}
	if got := count(t, s, `SELECT count(*) FROM shelf_import WHERE file = 'b.jsonl' AND items = 1`); got != 1 {
		t.Error("shelf_import should record the latest file")
	}

	// An invalid shelf is rejected and leaves the old data alone.
	if err := s.ReplaceShelf(ctx, "demo", "c.jsonl", &contract.Shelf{Items: []contract.ShelfItem{{ID: "x"}}}); err == nil {
		t.Error("invalid shelf was accepted")
	}
	if got := count(t, s, `SELECT count(*) FROM shelf_item`); got != 1 {
		t.Errorf("rejected ingest changed the data: %d items", got)
	}
}

// The schema's CHECK constraints must accept exactly the contract's
// vocabularies. If this fails, a constant was added on one side only.
func TestSchemaAcceptsContractVocabulary(t *testing.T) {
	s := openTest(t)
	for _, k := range contract.EventKinds {
		if _, err := s.DB().Exec(`INSERT INTO shelf_event VALUES ('t', 'i', ?, NULL, 'missing', NULL)`, string(k)); err != nil {
			t.Errorf("event kind %q rejected by schema: %v", k, err)
		}
	}
	for _, q := range []contract.DateQuality{contract.DateSystem, contract.DateUser} {
		if _, err := s.DB().Exec(`INSERT INTO shelf_event VALUES ('t', 'i', 'finished', DATE '2020-01-01', ?, NULL)`, string(q)); err != nil {
			t.Errorf("date quality %q rejected by schema: %v", q, err)
		}
	}
	for i, f := range contract.Formats {
		if _, err := s.DB().Exec(`INSERT INTO shelf_item (source, item_id, title, authors, isbns, format, refs, subjects)
			VALUES ('t', ?, 'x', ['y'], [], ?, MAP {}, [])`, string(rune('a'+i)), string(f)); err != nil {
			t.Errorf("format %q rejected by schema: %v", f, err)
		}
	}
	if _, err := s.DB().Exec(`INSERT INTO shelf_event VALUES ('t', 'i', 'loved', NULL, 'missing', NULL)`); err == nil {
		t.Error("schema accepted an event kind the contract doesn't have")
	}
}

func TestSaveCatalogUpserts(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	wait := 30
	titles := []contract.CatalogTitle{{
		ID: "100", Title: "Dune", Format: contract.Audiobook,
		Creators: []contract.Creator{{Name: "Frank Herbert", Role: contract.Author}, {Name: "Scott Brick", Role: contract.Narrator}},
		Subjects: []contract.Subject{{Scheme: "overdrive", Code: "80", Name: "Science Fiction"}},
		Refs:     map[string]string{contract.RefOverDrive: "100"},
	}}
	first := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	second := first.AddDate(0, 0, 7)

	if err := s.SaveCatalog(ctx, "lib", titles, []contract.Availability{{TitleID: "100", CopiesOwned: 2, Holds: 5, WaitDays: &wait}}, first); err != nil {
		t.Fatal(err)
	}
	titles[0].Title = "Dune (Unabridged)"
	if err := s.SaveCatalog(ctx, "lib", titles, []contract.Availability{{TitleID: "100", Available: true, CopiesOwned: 2, CopiesAvailable: 1}}, second); err != nil {
		t.Fatal(err)
	}

	if got := count(t, s, `SELECT count(*) FROM catalog_title
		WHERE title = 'Dune (Unabridged)' AND first_seen_at = ? AND last_seen_at = ?
		  AND creators[2].role = 'narrator' AND subjects[1].code = '80'`, first, second); got != 1 {
		t.Error("upsert should update fields and last_seen_at but keep first_seen_at")
	}
	if got := count(t, s, `SELECT count(*) FROM availability`); got != 2 {
		t.Errorf("availability snapshots = %d, want 2", got)
	}
	if got := count(t, s, `SELECT count(*) FROM latest_availability WHERE available AND wait_days IS NULL`); got != 1 {
		t.Error("latest_availability should show only the newest snapshot")
	}
}

func TestRunScript(t *testing.T) {
	s := openTest(t)
	script := `CREATE OR REPLACE TABLE t AS SELECT getvariable('n') AS n, getvariable('label') AS label;
	           INSERT INTO t VALUES (2, 'two');`
	if err := s.RunScript(context.Background(), script, Vars{"n": 1, "label": "it's one"}); err != nil {
		t.Fatal(err)
	}
	if got := count(t, s, `SELECT count(*) FROM t WHERE (n = 1 AND label = 'it''s one') OR n = 2`); got != 2 {
		t.Errorf("script should set variables and run every statement; rows = %d", got)
	}
}

func TestSnapshotAndOpenReadOnly(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, err := Open(ctx, filepath.Join(dir, "live.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.DB().Exec(`CREATE TABLE note AS SELECT 'published' AS text`); err != nil {
		t.Fatal(err)
	}
	copyPath := filepath.Join(dir, "serve.duckdb")
	if err := s.Snapshot(ctx, copyPath); err != nil {
		t.Fatal(err)
	}

	// The live database stays writable while the copy is open elsewhere.
	if _, err := s.DB().Exec(`INSERT INTO note VALUES ('live only')`); err != nil {
		t.Fatal(err)
	}

	ro, err := OpenReadOnly(ctx, copyPath)
	if err != nil {
		t.Fatal(err)
	}
	defer ro.Close()
	if got := count(t, ro, `SELECT count(*) FROM note`); got != 1 {
		t.Errorf("the copy should hold what was there when it was taken; rows = %d", got)
	}
	if _, err := ro.DB().Exec(`CREATE TABLE x (a INT)`); err == nil {
		t.Error("the served copy must be read-only")
	}
	if _, err := ro.DB().Exec(`SELECT * FROM read_csv('/etc/hostname')`); err == nil {
		t.Error("queries on the served copy must not read local files")
	}
	if _, err := ro.DB().Exec(`SET enable_external_access = true`); err == nil {
		t.Error("the lockdown must not be reversible")
	}
}
