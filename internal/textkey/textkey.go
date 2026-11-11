// Package textkey turns titles and author names into matching keys, so the
// same book written slightly differently by two sources compares equal:
//
//	Title("The Hunt for Red October (Jack Ryan, #1)")  == "hunt for red october"
//	Author("John le Carré") == Author("le Carré, John") == "carre j"
//
// Keys are for matching only. Nothing displays them.
package textkey

import (
	"regexp"
	"slices"
	"strings"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// Title returns the key for a whole title, subtitle included. It lowercases,
// folds accents to ASCII, and drops bracketed notes ("(Unabridged)",
// "(Jack Ryan, #1)"), a trailing "A Novel", a leading article, and
// punctuation.
func Title(title string) string {
	s := fold(title)
	s = bracketed.ReplaceAllString(s, " ")
	s = aNovel.ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, "&", " and ")
	return dropLeadingArticle(words(s))
}

// MainTitle returns the key for the part of a title before its first colon,
// so "Ethics: Demonstrated in Geometrical Order" can match a source that
// only says "Ethics". Matches on it are weaker than on Title, because
// "Star Wars: Thrawn" and "Star Wars: Bloodline" share one.
func MainTitle(title string) string {
	main, _, _ := strings.Cut(title, ":")
	return Title(main)
}

// Author returns the key for a person's name: the last word of the surname
// and the first initial, "tolkien j". It reads both "First Last" and
// "Last, First", ignores particles ("le", "de", "van") by keeping only the
// surname's last word, and drops suffixes such as "Jr." and "III".
func Author(name string) string {
	s := strings.ReplaceAll(fold(name), ".", " ")

	var surname, given string
	if last, first, ok := strings.Cut(s, ","); ok {
		surname = last
		given, _, _ = strings.Cut(first, ",") // "King, Martin Luther, Jr." keeps "Martin Luther"
	} else {
		parts := strings.Fields(words(s))
		for len(parts) > 1 && suffixes[parts[len(parts)-1]] {
			parts = parts[:len(parts)-1]
		}
		if len(parts) == 0 {
			return ""
		}
		surname = parts[len(parts)-1]
		given = strings.Join(parts[:len(parts)-1], " ")
	}

	surnameWords := strings.Fields(words(surname))
	if len(surnameWords) == 0 {
		return ""
	}
	key := surnameWords[len(surnameWords)-1]
	if g := strings.Fields(words(given)); len(g) > 0 {
		key += " " + g[0][:1]
	}
	return key
}

// Person returns the key that names one person, for telling an author's
// books apart from everyone else's (internal/authors). It is the whole
// name, given names first, folded to ASCII and without punctuation:
// "John le Carré", "John Le Carre" and "le Carré, John" are all
// "john le carre", and "J.M. Barrie", "JM Barrie" and "Barrie, J. M." are
// all "j m barrie". Suffixes ("Jr.", "Ph.D.", "MD") are dropped, and a
// name written on both sides of the comma ("Plato, Plato", as LibraryThing
// writes a single name) counts once. Placeholders such as "Anonymous" give
// "".
//
// Unlike Author it keeps every name, because a catalog of 100k books has
// dozens of J. Smiths. The price is that "James Matthew Barrie" and
// "J. M. Barrie" stay apart: telling that pair from "Min Jin Lee" and
// "M. J. Lee", who are two people, takes more than their names.
func Person(name string) string {
	var parts []string
	for _, p := range strings.Split(fold(name), ",") {
		if !isSuffix(p) {
			parts = append(parts, p)
		}
	}
	if len(parts) == 0 || IsPlaceholderAuthor(Author(name)) {
		return ""
	}

	var given, family []string
	if len(parts) == 1 {
		names := nameWords(parts[0])
		for len(names) > 1 && suffixes[names[len(names)-1]] {
			names = names[:len(names)-1]
		}
		if len(names) == 0 {
			return ""
		}
		given, family = names[:len(names)-1], names[len(names)-1:]
	} else {
		family, given = nameWords(parts[0]), nameWords(parts[1]) // "Last, First"
		if slices.Equal(family, given) {
			given = nil // "Plato, Plato"
		}
	}

	var key []string
	for _, g := range given {
		key = append(key, initials(g)...)
	}
	return strings.Join(append(key, family...), " ")
}

// LikelySamePerson reports whether two Person keys look like one name
// spelt two ways: the same words but one, and those two long and a letter
// or two apart, as transliterations and typos are ("dostoyevsky" and
// "dostoevsky", "hermann" and "herman"). It's a guess, good enough to keep
// a person from being listed as similar to themselves, but not to merge
// their books: "Mark Smith" and "Mary Smith" would pass.
func LikelySamePerson(a, b string) bool {
	aw, bw := strings.Fields(a), strings.Fields(b)
	if a == b || len(aw) != len(bw) {
		return false
	}
	differ := 0
	for i := range aw {
		if aw[i] == bw[i] {
			continue
		}
		differ++
		if differ > 1 || min(len(aw[i]), len(bw[i])) < 5 || editDistance(aw[i], bw[i]) > 2 {
			return false
		}
	}
	return true
}

// editDistance is the Levenshtein distance between two ASCII words: how
// many letters must be inserted, deleted or changed to turn one into the
// other. It keeps one row of the usual table at a time.
func editDistance(a, b string) int {
	row := make([]int, len(b)+1)
	for j := range row {
		row[j] = j
	}
	for i := 1; i <= len(a); i++ {
		diagonal := row[0] // the cell above and to the left
		row[0] = i
		for j := 1; j <= len(b); j++ {
			change := diagonal
			if a[i-1] != b[j-1] {
				change++
			}
			diagonal = row[j]
			row[j] = min(row[j]+1, row[j-1]+1, change)
		}
	}
	return row[len(b)]
}

// nameWords splits a name into words, treating a dot like a space so that
// "J.M." is two initials.
func nameWords(s string) []string {
	return strings.Fields(words(strings.ReplaceAll(s, ".", " ")))
}

// isSuffix reports whether a part of a name between commas is only
// suffixes: "Jr.", "M.D.", "Ph.D.", "MD/PHD".
func isSuffix(part string) bool {
	ws := strings.Fields(words(strings.ReplaceAll(part, ".", "")))
	for _, w := range ws {
		if !suffixes[w] {
			return false
		}
	}
	return len(ws) > 0
}

// initials spells out a given name written as run-together initials, which
// is how a short word with no vowels reads: "jm" → "j", "m".
func initials(word string) []string {
	if len(word) < 2 || len(word) > 3 || strings.ContainsAny(word, "aeiouy") {
		return []string{word}
	}
	return strings.Split(word, "")
}

// IsPlaceholderAuthor reports whether an author key stands for no one in
// particular ("Anonymous", "Various"). Books by placeholders can't be
// matched on author.
func IsPlaceholderAuthor(key string) bool {
	return placeholderAuthors[key]
}

var (
	bracketed = regexp.MustCompile(`\([^)]*\)|\[[^\]]*\]`)
	aNovel    = regexp.MustCompile(`[\s:,.-]*\ba novel\s*$`)
	nonAlnum  = regexp.MustCompile(`[^a-z0-9]+`)

	suffixes = map[string]bool{"jr": true, "sr": true, "ii": true, "iii": true, "iv": true, "phd": true, "md": true}

	// keys of non-people: "Anonymous" (and the French "Anonyme"), "Various",
	// "Unknown", "Various Authors".
	placeholderAuthors = map[string]bool{"anonymous": true, "anonyme": true, "various": true, "unknown": true, "authors v": true}

	// letters that don't decompose into a base letter plus an accent.
	ligatures = strings.NewReplacer("ß", "ss", "æ", "ae", "œ", "oe", "ø", "o", "ł", "l", "đ", "d", "þ", "th", "ı", "i")
)

// fold lowercases s and reduces it to ASCII letters where it can:
// "Brontë" → "bronte", "Straße" → "strasse".
func fold(s string) string {
	s = ligatures.Replace(strings.ToLower(s))
	stripAccents := transform.Chain(norm.NFKD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
	out, _, err := transform.String(stripAccents, s)
	if err != nil {
		return s
	}
	// Apostrophes join rather than split: "o'brien" → "obrien".
	return strings.NewReplacer("'", "", "’", "").Replace(out)
}

// words replaces every run of non-alphanumeric characters with one space.
func words(s string) string {
	return strings.TrimSpace(nonAlnum.ReplaceAllString(s, " "))
}

func dropLeadingArticle(s string) string {
	for _, article := range []string{"the ", "a ", "an "} {
		if rest, ok := strings.CutPrefix(s, article); ok && rest != "" {
			return rest
		}
	}
	return s
}
