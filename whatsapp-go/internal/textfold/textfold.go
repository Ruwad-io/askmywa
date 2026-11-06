// Package textfold lowercases text and strips diacritics so that "Élodie"
// matches "elodie". It is the Go counterpart of the Python fold() helper and
// is applied to every name stored in the database (name_fold columns) and to
// every search term.
package textfold

import (
	"strings"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

var folder = transform.Chain(norm.NFKD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)

// Fold returns the lower-cased, accent-free form of s.
func Fold(s string) string {
	out, _, err := transform.String(folder, s)
	if err != nil {
		out = s
	}
	return strings.ToLower(strings.TrimSpace(out))
}

// Digits keeps only the decimal digits of s ("+33 6 12-34" becomes "3361234").
func Digits(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// IsDigits reports whether s is non-empty and made only of decimal digits.
func IsDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
