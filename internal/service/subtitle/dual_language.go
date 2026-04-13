package subtitle

import (
	"unicode"
)

type lineLanguage int

const (
	langUnknown lineLanguage = iota
	langChinese
	langEnglish
	langMixed
)

func containsCJK(s string) bool {
	for _, r := range s {
		if isCJK(r) {
			return true
		}
	}
	return false
}

func containsLatin(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) && unicode.In(r, unicode.Latin) {
			return true
		}
	}
	return false
}

func isCJK(r rune) bool {
	return unicode.In(r, unicode.Han)
}

func classifyLine(line string) lineLanguage {
	hasCJK := containsCJK(line)
	hasLatin := containsLatin(line)

	switch {
	case hasCJK && hasLatin:
		return langMixed
	case hasCJK:
		return langChinese
	case hasLatin:
		return langEnglish
	default:
		return langUnknown
	}
}
