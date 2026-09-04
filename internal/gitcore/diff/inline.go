package diff

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

type tokenClass uint8

const (
	classWord tokenClass = iota
	classSpace
	classOther
)

func classOf(r rune) tokenClass {
	switch {
	case r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r):
		return classWord
	case unicode.IsSpace(r):
		return classSpace
	default:
		return classOther
	}
}

func tokenize(line string) []string {
	var tokens []string
	for at := 0; at < len(line); {
		r, size := utf8.DecodeRuneInString(line[at:])
		kind := classOf(r)
		end := at + size
		if kind != classOther {
			for end < len(line) {
				next, nextSize := utf8.DecodeRuneInString(line[end:])
				if classOf(next) != kind {
					break
				}
				end += nextSize
			}
		}
		tokens = append(tokens, line[at:end])
		at = end
	}
	return tokens
}

func InlineDiff(oldLine, newLine string) []Span {
	oldTokens, newTokens := tokenize(oldLine), tokenize(newLine)
	e := prepareEnv(oldTokens, newTokens, Options{})
	e.myers()
	compact(e.a, e.b, false)
	compact(e.b, e.a, false)

	var spans []Span
	at := 0
	for _, c := range e.script() {
		spans = appendSpan(spans, KindContext, oldTokens[at:c.i1])
		spans = appendSpan(spans, KindDel, oldTokens[c.i1:c.i1+c.chg1])
		spans = appendSpan(spans, KindAdd, newTokens[c.i2:c.i2+c.chg2])
		at = c.i1 + c.chg1
	}
	return appendSpan(spans, KindContext, oldTokens[at:])
}

func appendSpan(spans []Span, kind Kind, tokens []string) []Span {
	if len(tokens) == 0 {
		return spans
	}
	return append(spans, Span{Kind: kind, Text: strings.Join(tokens, "")})
}
