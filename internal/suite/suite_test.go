package suite

import (
	"bytes"
	"slices"
	"strings"
	"testing"
)

const file = `# comments and blank lines are skipped

spy thrillers: similar --seed "tinker tailor" --seed "red october" --view genre
    top 20 has 2 subject "thrillers / espionage"
    top 10 has author "Le Carré"
    top 20 lacks title "da vinci"
le carre: author le carre
`

func TestParse(t *testing.T) {
	qs, err := Parse(strings.NewReader(file))
	if err != nil {
		t.Fatal(err)
	}
	if len(qs) != 2 {
		t.Fatalf("%d queries, want 2", len(qs))
	}
	q := qs[0]
	if q.Name != "spy thrillers" || q.Line != 3 {
		t.Errorf("query %q at line %d", q.Name, q.Line)
	}
	want := []string{"similar", "--seed", "tinker tailor", "--seed", "red october", "--view", "genre"}
	if !slices.Equal(q.Args, want) {
		t.Errorf("args = %q, want %q", q.Args, want)
	}
	if len(q.Checks) != 3 {
		t.Fatalf("%d checks, want 3", len(q.Checks))
	}
	got := q.Checks[0]
	if got.Top != 20 || got.Min != 2 || got.Lacks || got.Field != "subject" || got.Value != "thrillers espionage" {
		t.Errorf("check 1 = %+v", got)
	}
	if got := q.Checks[1]; got.Min != 1 || got.Field != "author" || got.Value != "le carre" {
		t.Errorf("check 2 = %+v", got)
	}
	if got := q.Checks[2]; !got.Lacks || got.Min != 0 {
		t.Errorf("check 3 = %+v", got)
	}
	if !slices.Equal(qs[1].Args, []string{"author", "le", "carre"}) || len(qs[1].Checks) != 0 {
		t.Errorf("query 2 = %+v", qs[1])
	}

	for _, bad := range []string{
		"    top 20 has subject \"x\"\n",                // a check with no query
		"q: similar --seed \"dune\n",                    // unclosed quote
		"q: similar\n    top twenty has author \"x\"\n", // not a number
		"q: similar\n    top 20 has colour \"x\"\n",     // no such field
		"no colon here\n",
	} {
		if _, err := Parse(strings.NewReader(bad)); err == nil {
			t.Errorf("Parse(%q) should fail", bad)
		}
	}
}

func TestMatches(t *testing.T) {
	books := []Book{
		{Title: "Smiley's People", Author: "John le Carré", Subjects: []string{"Fiction / Thrillers / Espionage"}},
		{Title: "Red Storm Rising", Author: "Tom Clancy", Subjects: []string{"Thriller"}},
		{Title: "The Da Vinci Code", Author: "Dan Brown", Subjects: []string{"Fiction / Thrillers / Suspense"}},
	}
	qs, err := Parse(strings.NewReader(file))
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []struct {
		n  int
		ok bool
	}{{1, false}, {1, true}, {1, false}} {
		if n, ok := qs[0].Checks[i].Matches(books); n != want.n || ok != want.ok {
			t.Errorf("%s: %d, %v; want %d, %v", qs[0].Checks[i].Text, n, ok, want.n, want.ok)
		}
	}
	// Only the top N count.
	c, _ := parseCheck(`top 1 has subject "thriller"`)
	if n, _ := c.Matches(books); n != 1 {
		t.Errorf("top 1 counted %d books", n)
	}
}

func TestSnapshot(t *testing.T) {
	last := Snapshot{"a": {"One — X", "Two — Y", "Three"}, "b": {}}
	var buf bytes.Buffer
	if err := WriteSnapshot(&buf, last, []string{"a", "b"}); err != nil {
		t.Fatal(err)
	}
	read, err := ReadSnapshot(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(read["a"], last["a"]) || read["b"] == nil {
		t.Errorf("read back %q", read)
	}

	got := Compare(last["a"], []string{"Two — Y", "Four", "One — X"})
	want := []Change{{Line: "Four", Rank: 2}, {Line: "Three", Was: 3}}
	if !slices.Equal(got, want) {
		t.Errorf("Compare = %+v, want %+v", got, want)
	}
}
