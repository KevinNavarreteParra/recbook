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
// and the first initial, "tolkien j". It reads names as parseName does,
// and ignores particles ("le", "de", "van") by keeping only the surname's
// last word.
func Author(name string) string {
	given, family := parseName(name)
	if len(family) == 0 {
		return ""
	}
	key := family[len(family)-1]
	if len(given) > 0 {
		key += " " + given[0][:1]
	}
	return key
}

// Person returns the key that names one person, for telling an author's
// books apart from everyone else's (internal/authors). It is the whole
// name, given names first, as parseName reads it: "John le Carré", "John
// Le Carre" and "le Carré, John" are all "john le carre", and "J.M.
// Barrie", "JM Barrie" and "Barrie, J. M." are all "j m barrie".
// Placeholders such as "Anonymous", and companies ("Penton Overseas,
// Inc.", "Editors of Life Magazine"), give "": they're no one.
//
// Unlike Author it keeps every name, because a catalog of 100k books has
// dozens of J. Smiths. The price is that "James Matthew Barrie" and
// "J. M. Barrie" stay apart: telling that pair from "Min Jin Lee" and
// "M. J. Lee", who are two people, takes more than their names.
func Person(name string) string {
	if isOrganization(name) || IsPlaceholderAuthor(Author(name)) {
		return ""
	}
	given, family := parseName(name)
	var key []string
	for _, g := range given {
		key = append(key, initials(g)...)
	}
	return strings.Join(append(key, family...), " ")
}

// parseName splits a credited name into given and family names, folded to
// ASCII, for Author and Person. It reads "First Last" and "Last, First",
// and drops what isn't the name: suffixes ("Jr.", "PhD", "USN, retired"),
// titles ("Dr.", "Sir", "Lieutenant General"), bracketed notes ("(ed.)"),
// and a note of someone else's part (", translated by Mark Harman"). A
// name written on both sides of the comma ("Plato, Plato", as Reading
// List and LibraryThing write a single name) counts once.
func parseName(name string) (given, family []string) {
	s := roleNote.ReplaceAllString(bracketed.ReplaceAllString(fold(name), " "), "")
	var parts []string
	for _, p := range strings.Split(s, ",") {
		if len(nameWords(p)) > 0 && !isSuffix(p) {
			parts = append(parts, p)
		}
	}
	switch len(parts) {
	case 0:
		return nil, nil
	case 1:
		names := nameWords(parts[0])
		for len(names) > 1 && suffixes[names[len(names)-1]] {
			names = names[:len(names)-1]
		}
		names = dropTitles(names)
		return names[:len(names)-1], names[len(names)-1:]
	default: // "Last, First"; anything after a second comma isn't the name
		family, given = nameWords(parts[0]), dropTitles(nameWords(parts[1]))
		if slices.Equal(family, given) {
			given = nil // "Plato, Plato"
		}
		return given, family
	}
}

// dropTitles drops titles from the front of a name ("dr", "sir"), keeping
// at least one word.
func dropTitles(names []string) []string {
	for len(names) > 1 && titles[names[0]] {
		names = names[1:]
	}
	return names
}

// isOrganization reports whether a credit names a company or a staff
// rather than a person: "Penton Overseas, Inc.", "Charles River Editors".
// "Press" alone doesn't count: Eyal Press is a person.
func isOrganization(name string) bool {
	for _, w := range nameWords(fold(name)) {
		if organizationWords[w] {
			return true
		}
	}
	return false
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
// suffixes or titles, and so not a name: "Jr.", "Ph.D.", "MD/PHD",
// "Captain", "Dr." (as in "Seuss, Dr.").
func isSuffix(part string) bool {
	ws := strings.Fields(words(strings.ReplaceAll(part, ".", "")))
	for _, w := range ws {
		if !suffixes[w] && !titles[w] {
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

	// suffixes that can follow a name: generations, degrees, and a
	// service's retired officers. "ma", "ms" and "do" are left out: Yo-Yo
	// Ma and Anh Do.
	suffixes = map[string]bool{
		"jr": true, "sr": true, "ii": true, "iii": true, "iv": true, "editor": true,
		"phd": true, "md": true, "dphil": true, "edd": true, "psyd": true, "mba": true, "rn": true, "pt": true,
		"lcsw": true, "dds": true, "dvm": true, "usa": true, "usn": true, "usmc": true, "usaf": true, "ret": true, "retired": true,
	}

	// titles that can come before a name. "gen" and "col" are left out:
	// both are also given names.
	titles = map[string]bool{
		"dr": true, "mr": true, "mrs": true, "ms": true, "sir": true, "dame": true, "lord": true, "lady": true,
		"rev": true, "reverend": true, "prof": true, "professor": true, "major": true, "captain": true, "capt": true,
		"colonel": true, "general": true, "admiral": true, "lieutenant": true, "lt": true, "sgt": true, "sergeant": true,
	}

	// words that mark a credit as a company or a staff, not a person.
	organizationWords = map[string]bool{
		"inc": true, "llc": true, "ltd": true, "corp": true, "corporation": true, "plc": true, "gmbh": true,
		"publishing": true, "publishers": true, "editors": true, "magazine": true, "associates": true, "group": true,
	}

	// roleNote is a note, after the name, of someone else's part in the book.
	roleNote = regexp.MustCompile(`[,;]?\s*\b(translated|edited|introduced|illustrated|adapted|abridged|retold|compiled|selected)\s+(by|and)\b.*$`)

	// keys of non-people: "Anonymous" (and the French "Anonyme"), "Various",
	// "Unknown", "Various Authors".
	placeholderAuthors = map[string]bool{"anonymous": true, "anonyme": true, "various": true, "unknown": true, "authors v": true}

	// letters that don't decompose into a base letter plus an accent.
	ligatures = strings.NewReplacer("ß", "ss", "æ", "ae", "œ", "oe", "ø", "o", "ł", "l", "đ", "d", "þ", "th", "ı", "i")
)

// Words returns s as Title compares it, without dropping anything: folded
// to lowercase ASCII, with punctuation as single spaces. It's for loose
// matching by substring: Words("John le Carré") contains Words("le carre").
func Words(s string) string {
	return words(fold(s))
}

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
