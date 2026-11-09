package contract

import (
	"errors"
	"strings"
)

// NormalizeISBN returns s as a 13-digit ISBN. It accepts ISBN-10 and
// ISBN-13, with hyphens or spaces, and rejects anything whose check digit
// is wrong.
func NormalizeISBN(s string) (string, error) {
	digits := strings.Map(func(r rune) rune {
		switch {
		case r >= '0' && r <= '9':
			return r
		case r == 'x' || r == 'X':
			return 'X'
		case r == '-' || r == ' ':
			return -1 // drop separators
		default:
			return '?' // anything else makes the ISBN invalid below
		}
	}, s)

	switch len(digits) {
	case 13:
		if !validISBN13(digits) {
			return "", errors.New("bad ISBN-13 check digit")
		}
		return digits, nil
	case 10:
		if !validISBN10(digits) {
			return "", errors.New("bad ISBN-10 check digit")
		}
		body := "978" + digits[:9]
		return body + isbn13CheckDigit(body), nil
	default:
		return "", errors.New("an ISBN has 10 or 13 digits")
	}
}

// validISBN10 checks an ISBN-10: digits weighted 10 down to 1 must sum to a
// multiple of 11, where a final X counts as 10.
func validISBN10(s string) bool {
	sum := 0
	for i, r := range s {
		var v int
		switch {
		case r >= '0' && r <= '9':
			v = int(r - '0')
		case r == 'X' && i == 9:
			v = 10
		default:
			return false
		}
		sum += v * (10 - i)
	}
	return sum%11 == 0
}

// validISBN13 checks an ISBN-13: its last digit must match the check digit
// computed from the first twelve.
func validISBN13(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return isbn13CheckDigit(s[:12]) == s[12:]
}

// isbn13CheckDigit computes the check digit for the first twelve digits of
// an ISBN-13, weighting them 1, 3, 1, 3, ...
func isbn13CheckDigit(first12 string) string {
	sum := 0
	for i, r := range first12 {
		w := 1
		if i%2 == 1 {
			w = 3
		}
		sum += int(r-'0') * w
	}
	return string(rune('0' + (10-sum%10)%10))
}
