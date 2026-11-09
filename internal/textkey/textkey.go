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

	// keys of non-people: "Anonymous", "Various", "Unknown", "Various Authors".
	placeholderAuthors = map[string]bool{"anonymous": true, "various": true, "unknown": true, "authors v": true}

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
