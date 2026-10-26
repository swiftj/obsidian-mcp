package search

import (
	"strings"
	"unicode"
)

// tokens splits s into lower-cased words: runs of letters, marks and digits.
// Han, Hiragana and Katakana have no spaces between words, so each of their
// characters is a token of its own [policy D-010].
func tokens(s string) []string {
	var out []string
	start := -1
	flush := func(end int) {
		if start >= 0 {
			out = append(out, strings.ToLower(s[start:end]))
			start = -1
		}
	}
	for i, r := range s {
		switch {
		case isIdeograph(r):
			flush(i)
			out = append(out, string(r))
		case unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.Is(unicode.Mn, r):
			if start < 0 {
				start = i
			}
		default:
			flush(i)
		}
	}
	flush(len(s))
	return out
}

func isIdeograph(r rune) bool {
	return unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana)
}

// parseQuery splits query text into words and quoted phrases. Each phrase's
// words are also returned as words.
func parseQuery(q string) (words, phrases []string) {
	for i, part := range strings.Split(q, `"`) {
		if i%2 == 1 && strings.TrimSpace(part) != "" {
			phrases = append(phrases, strings.ToLower(strings.TrimSpace(part)))
		}
		words = append(words, tokens(part)...)
	}
	return words, phrases
}
