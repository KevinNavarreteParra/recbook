// Package blurb cleans publisher descriptions before they're embedded
// (spec §3.5). Marketing copy carries the same noise on every bestseller:
// praise quotes, award lists, "#1 New York Times bestselling author", and
// audiobook boilerplate. Left in, that noise makes unrelated books look
// alike, so Clean removes it.
//
// Cleaning works sentence by sentence and drops any sentence that matches a
// noise pattern. It errs toward keeping text: a missed praise line costs a
// little similarity, while a dropped plot sentence loses the signal.
package blurb

import (
	"html"
	"regexp"
	"strings"
)

// ThinChars is the length below which a cleaned blurb is too short to say
// much on its own.
const ThinChars = 150

// Clean returns desc without HTML or marketing noise, as plain sentences
// separated by single spaces.
func Clean(desc string) string {
	text := toPlainText(desc)

	var kept []string
	for _, para := range strings.Split(text, "\n") {
		if isPraiseParagraph(para) {
			continue
		}
		for _, s := range sentences(para) {
			if !isNoise(s) {
				kept = append(kept, s)
			}
		}
	}
	return strings.Join(kept, " ")
}

// IsThin reports whether a cleaned blurb is too short to rely on.
func IsThin(cleaned string) bool {
	return len(cleaned) < ThinChars
}

var (
	blockTag   = regexp.MustCompile(`(?i)<\s*(br|/p|p|/div|div|/li|li|h\d|/h\d)\b[^>]*>`)
	anyTag     = regexp.MustCompile(`<[^>]*>`)
	spaces     = regexp.MustCompile(`[ \t\x{00a0}]+`)
	sentenceRe = regexp.MustCompile(`[^.!?]+(?:[.!?]+["”’)]*|$)`)

	// A paragraph that is only a quotation and its source:
	//   "A masterpiece." —The New York Times
	praiseParagraph = regexp.MustCompile(`^\s*["“].*["”]\s*[—–-]{1,2}\s*\S.{0,80}$`)

	// Sentences that sell rather than describe.
	noise = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\b(new york times|usa today|wall street journal|sunday times|indie|international|instant|national|global|#1)\b.{0,20}\bbest[- ]?sell`),
		regexp.MustCompile(`(?i)\bbest[- ]?selling author\b`),
		regexp.MustCompile(`(?i)\b(winner|finalist|shortlisted|longlisted)\b.{0,40}\b(prize|award|medal)\b`),
		regexp.MustCompile(`(?i)\b(prize|award)[- ]winning\b`),
		regexp.MustCompile(`(?i)\bnamed (one of )?the best books?\b|\bbest books? of (the year|\d{4})\b|\bnotable book\b`),
		regexp.MustCompile(`(?i)\b(oprah'?s|reese'?s|good morning america|today show|jenna)\b.{0,20}\bbook club\b`),
		regexp.MustCompile(`["”]\s*[—–]\s*[A-Z]`), // a quote with its attribution, inside a sentence
		regexp.MustCompile(`(?i)\b(narrated|read) by\b|\b(un)?abridged\b|\b(bonus|accompanying|includes a) pdf\b|\baudio ?book\b`),
		regexp.MustCompile(`(?i)\bdon'?t miss\b|\bnext (thrilling |exciting )?installment\b|^(and )?look for\b|\bcoming soon\b`),
		regexp.MustCompile(`(?i)\b(now|soon) a (major )?(motion picture|netflix|hbo|hulu|tv|television|limited)\b`),
	}
)

// toPlainText turns HTML into text, one paragraph per line.
func toPlainText(s string) string {
	s = blockTag.ReplaceAllString(s, "\n")
	s = anyTag.ReplaceAllString(s, "")
	s = html.UnescapeString(s)

	var lines []string
	for _, line := range strings.Split(s, "\n") {
		if line = strings.TrimSpace(spaces.ReplaceAllString(line, " ")); line != "" {
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n")
}

// sentences splits a paragraph at sentence-ending punctuation. It's rough
// (abbreviations such as "Dr." split too), which is fine for deciding what
// to drop.
func sentences(para string) []string {
	var out []string
	for _, s := range sentenceRe.FindAllString(para, -1) {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func isPraiseParagraph(p string) bool {
	return praiseParagraph.MatchString(p)
}

func isNoise(sentence string) bool {
	for _, re := range noise {
		if re.MatchString(sentence) {
			return true
		}
	}
	return false
}
