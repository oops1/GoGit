package journal

import (
	"strings"
	"unicode"
)

func Initials(author string) string {
	words := strings.Fields(author)
	switch len(words) {
	case 0:
		return ""
	case 1:
		return singleWordInitials(words[0])
	default:
		return string([]rune{firstRune(words[0]), firstRune(words[1])})
	}
}

func singleWordInitials(word string) string {
	runes := []rune(word)
	if len(runes) >= 2 {
		return string([]rune{unicode.ToUpper(runes[0]), unicode.ToUpper(runes[1])})
	}
	return string(unicode.ToUpper(runes[0]))
}

func firstRune(word string) rune {
	return unicode.ToUpper([]rune(word)[0])
}
