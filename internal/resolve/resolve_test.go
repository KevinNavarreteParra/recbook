package resolve

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KevinNavarreteParra/recbook/internal/demo"
	"github.com/KevinNavarreteParra/recbook/internal/store"
)

func demoStore(t *testing.T) *store.Store {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := demo.Load(ctx, st); err != nil {
		t.Fatal(err)
	}
	return st
}

// sameWork reports whether every ref ended up in one work.
func sameWork(t *testing.T, st *store.Store, refs ...string) bool {
	t.Helper()
	var works, found int
	err := st.DB().QueryRow(`SELECT count(DISTINCT work_id), count(*) FROM work_record WHERE list_contains(?, ref)`, refs).
		Scan(&works, &found)
	if err != nil {
		t.Fatal(err)
	}
	if found != len(refs) {
		t.Fatalf("only %d of %v are records", found, refs)
	}
	return works == 1
}

func TestRunMatchesTheDemo(t *testing.T) {
	st := demoStore(t)
	review := filepath.Join(t.TempDir(), "review.csv")
	rep, err := Run(context.Background(), st, Options{ReviewFile: review})
	if err != nil {
		t.Fatal(err)
	}

	same := []struct {
		why  string
		refs []string
	}{
		{"shared ISBN, and titles that differ only in punctuation",
			[]string{"reading_app:ra-1", "owned_books:ob-1", "catalog:demo:9013"}},
		{"same title, author written last-name-first",
			[]string{"reading_app:ra-2", "owned_books:ob-2"}},
		{"OverDrive id on the loan, title match to the reading app",
			[]string{"library_loans:9001", "catalog:demo:9001", "reading_app:ra-3"}},
		{"two recordings of one work",
			[]string{"owned_books:ob-3", "catalog:demo:9010", "catalog:demo:9014"}},
		{"main title, where the shelf has no subtitle",
			[]string{"owned_books:ob-4", "catalog:demo:9012"}},
	}
	for _, s := range same {
		if !sameWork(t, st, s.refs...) {
			t.Errorf("%s: %v should be one work", s.why, s.refs)
		}
	}

	if sameWork(t, st, "reading_app:ra-2", "catalog:demo:9018") {
		t.Error("Dune and Dune Messiah must stay apart")
	}
	if sameWork(t, st, "reading_app:ra-5", "reading_app:ra-1") {
		t.Error("two le Carré novels must stay apart")
	}

	if rep.ShelfRecords != 19 || rep.CatalogRecords != 15 {
		t.Errorf("records: %d shelf, %d catalog", rep.ShelfRecords, rep.CatalogRecords)
	}
	if rep.EdgesByMethod["isbn"] != 1 || rep.EdgesByMethod["ref:overdrive"] != 5 || rep.EdgesByMethod["main_title"] < 2 {
		t.Errorf("edges = %v", rep.EdgesByMethod)
	}

	body, err := os.ReadFile(review)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "main_title") || !strings.Contains(string(body), "Ethics") {
		t.Errorf("review file should list the main-title matches:\n%s", body)
	}
}

func TestOverrides(t *testing.T) {
	st := demoStore(t)
	file := filepath.Join(t.TempDir(), "overrides.csv")
	os.WriteFile(file, []byte(strings.Join([]string{
		"ref,link_to,note",
		"# cut the Ethics main-title match, and force an unlikely pairing",
		"catalog:demo:9012,none,",
		"reading_app:ra-4,catalog:demo:9016,just to prove links work",
	}, "\n")), 0o644)

	if _, err := Run(context.Background(), st, Options{OverridesFile: file}); err != nil {
		t.Fatal(err)
	}
	if sameWork(t, st, "owned_books:ob-4", "catalog:demo:9012") {
		t.Error(`"none" should cut the record's matches`)
	}
	if !sameWork(t, st, "reading_app:ra-4", "catalog:demo:9016") {
		t.Error("a link should join the two records")
	}

	os.WriteFile(file, []byte("ref,link_to\nreading_app:typo,none\n"), 0o644)
	if _, err := Run(context.Background(), st, Options{OverridesFile: file}); err == nil || !strings.Contains(err.Error(), ":2") {
		t.Errorf("an override naming no record should fail with its line number, got %v", err)
	}
}

func TestUnionFind(t *testing.T) {
	uf := newUnionFind(5)
	uf.union(3, 1)
	uf.union(4, 3)
	if uf.find(4) != 1 || uf.find(3) != 1 {
		t.Error("the group's representative should be its smallest member")
	}
	if uf.find(0) != 0 || uf.find(2) != 2 {
		t.Error("untouched items are their own groups")
	}
}
