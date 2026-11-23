package suite

import (
	"bufio"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
)

// A snapshot keeps each query's results from a run, one line per book
// ("Title — Author"), so the next run can say what changed. It's a text
// file that reads well and diffs well on its own:
//
//	## spy thrillers
//	1. Smiley's People — John le Carré
//	2. Red Storm Rising — Tom Clancy

// Snapshot is the results of a run, by query name.
type Snapshot map[string][]string

// Line is how a book appears in a snapshot.
func Line(b Book) string {
	if b.Author == "" {
		return b.Title
	}
	return b.Title + " — " + b.Author
}

// WriteSnapshot writes the snapshot's queries in the order of names.
func WriteSnapshot(w io.Writer, s Snapshot, names []string) error {
	bw := bufio.NewWriter(w)
	for i, name := range names {
		if i > 0 {
			fmt.Fprintln(bw)
		}
		fmt.Fprintf(bw, "## %s\n", name)
		for j, line := range s[name] {
			fmt.Fprintf(bw, "%d. %s\n", j+1, line)
		}
	}
	return bw.Flush()
}

// ReadSnapshot reads what WriteSnapshot wrote.
func ReadSnapshot(r io.Reader) (Snapshot, error) {
	s := Snapshot{}
	var name string
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := sc.Text()
		if n, ok := strings.CutPrefix(line, "## "); ok {
			name = n
			s[name] = []string{}
			continue
		}
		if rank, book, ok := strings.Cut(line, ". "); ok && name != "" {
			if _, err := strconv.Atoi(rank); err == nil {
				s[name] = append(s[name], book)
			}
		}
	}
	return s, sc.Err()
}

// Change is a book that entered a query's results or left them.
type Change struct {
	Line string
	Rank int // where it ranks now (1-based), or 0 if it left
	Was  int // where it ranked last run, or 0 if it's new
}

// Compare lists the books that entered the results since the last run,
// best first, then those that left, in their old order. A book that only
// moved isn't a change.
func Compare(last, now []string) []Change {
	var out []Change
	for i, line := range now {
		if !slices.Contains(last, line) {
			out = append(out, Change{Line: line, Rank: i + 1})
		}
	}
	for i, line := range last {
		if !slices.Contains(now, line) {
			out = append(out, Change{Line: line, Was: i + 1})
		}
	}
	return out
}
