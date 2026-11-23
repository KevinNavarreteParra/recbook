// Package suite is the saved query suite of spec §11.4: a hand-kept list
// of queries, each with what its results should show, rerun after every
// change to the ranking. A run says which expectations failed and, for
// each query, which books entered or left its results since the last run,
// so a change can be judged by eye as well as by the eval's numbers.
//
// The suite file is plain text (see Parse). This package reads it, checks
// results against it and compares runs; running the queries is up to the
// caller, since they are recbook's own commands (internal/cli).
package suite

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/KevinNavarreteParra/recbook/internal/textkey"
)

// Query is one saved query.
type Query struct {
	Name   string
	Args   []string // a recbook command and its flags: "similar", "--seed", "dune"
	Checks []Check
	Line   int // where it starts in the file, for errors
}

// Check is one expectation: that at least Min of the top Top results match,
// or with Lacks, that none do.
type Check struct {
	Text  string // as written
	Top   int
	Min   int
	Lacks bool
	Field string // "author", "title" or "subject"
	Value string // textkey.Words of the text to look for
}

// Book is one result, as checks see it.
type Book struct {
	WorkID   int64
	Title    string
	Author   string
	Subjects []string // the catalog's subjects; see LoadSubjects
}

// Parse reads a suite file. Blank lines and lines starting with # are
// skipped. A query is a line "name: command flags...", the command and
// flags written as for recbook, with "double quotes" around anything with
// spaces; each indented line under it is a check:
//
//	spy thrillers: similar --seed "tinker tailor" --seed "red october"
//	    top 20 has 10 subject "thriller"
//	    top 20 has author "le carre"
//	    top 20 lacks author "dan brown"
//
// "has K" asks for at least K matches (1 if K is left out), "lacks" for
// none. A book matches when its author, its title, or any one of its
// subjects contains the text, ignoring case, accents and punctuation.
func Parse(r io.Reader) ([]Query, error) {
	var out []Query
	sc := bufio.NewScanner(r)
	for n := 1; sc.Scan(); n++ {
		line := sc.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if line[0] == ' ' || line[0] == '\t' {
			if len(out) == 0 {
				return nil, fmt.Errorf("line %d: a check before any query", n)
			}
			c, err := parseCheck(trimmed)
			if err != nil {
				return nil, fmt.Errorf("line %d: %w", n, err)
			}
			q := &out[len(out)-1]
			q.Checks = append(q.Checks, c)
			continue
		}
		name, command, ok := strings.Cut(trimmed, ":")
		if !ok {
			return nil, fmt.Errorf("line %d: want \"name: command\"", n)
		}
		args, err := split(command)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", n, err)
		}
		if len(args) == 0 {
			return nil, fmt.Errorf("line %d: query %q has no command", n, name)
		}
		out = append(out, Query{Name: strings.TrimSpace(name), Args: args, Line: n})
	}
	return out, sc.Err()
}

// parseCheck reads "top N has [K] field "text"" or "top N lacks field "text"".
func parseCheck(s string) (Check, error) {
	words, err := split(s)
	if err != nil {
		return Check{}, err
	}
	c := Check{Text: s, Min: 1}
	bad := fmt.Errorf("want `top N has [K] author|title|subject \"text\"` or `top N lacks ...`, not %q", s)
	if len(words) < 5 || words[0] != "top" {
		return c, bad
	}
	if c.Top, err = strconv.Atoi(words[1]); err != nil || c.Top < 1 {
		return c, bad
	}
	rest := words[3:]
	switch words[2] {
	case "lacks":
		c.Lacks, c.Min = true, 0
	case "has":
		if k, err := strconv.Atoi(rest[0]); err == nil {
			c.Min, rest = k, rest[1:]
		}
	default:
		return c, bad
	}
	if len(rest) != 2 {
		return c, bad
	}
	switch rest[0] {
	case "author", "title", "subject":
		c.Field = rest[0]
	default:
		return c, bad
	}
	c.Value = textkey.Words(rest[1])
	return c, nil
}

// split breaks a command line into words at spaces, keeping "quoted text"
// as one word.
func split(s string) ([]string, error) {
	var out []string
	var word strings.Builder
	inWord, quoted := false, false
	for _, r := range s {
		switch {
		case r == '"':
			quoted, inWord = !quoted, true
		case (r == ' ' || r == '\t') && !quoted:
			if inWord {
				out = append(out, word.String())
				word.Reset()
				inWord = false
			}
		default:
			word.WriteRune(r)
			inWord = true
		}
	}
	if quoted {
		return nil, fmt.Errorf("unclosed quote in %q", s)
	}
	if inWord {
		out = append(out, word.String())
	}
	return out, nil
}

// Matches counts the books among the top c.Top that match c, and says
// whether that's what c expects.
func (c Check) Matches(books []Book) (int, bool) {
	n := 0
	for _, b := range books[:min(len(books), c.Top)] {
		if c.match(b) {
			n++
		}
	}
	if c.Lacks {
		return n, n == 0
	}
	return n, n >= c.Min
}

func (c Check) match(b Book) bool {
	switch c.Field {
	case "author":
		return strings.Contains(textkey.Words(b.Author), c.Value)
	case "title":
		return strings.Contains(textkey.Words(b.Title), c.Value)
	}
	for _, s := range b.Subjects {
		if strings.Contains(textkey.Words(s), c.Value) {
			return true
		}
	}
	return false
}

// Depth is how many results a query keeps for checking and comparing:
// the deepest of its checks, and at least 20.
func (q Query) Depth() int {
	d := 20
	for _, c := range q.Checks {
		d = max(d, c.Top)
	}
	return d
}
