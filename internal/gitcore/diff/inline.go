package diff

import (
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

func tokenize(line string) Text {
	if line == "" {
		return Text{}
	}
	data := []byte(line)
	offs := make([]int, 1, len(data)/2+2)
	for at := 0; at < len(data); {
		r, size := utf8.DecodeRune(data[at:])
		kind := classOf(r)
		end := at + size
		if kind != classOther {
			for end < len(data) {
				next, nextSize := utf8.DecodeRune(data[end:])
				if classOf(next) != kind {
					break
				}
				end += nextSize
			}
		}
		offs = append(offs, end)
		at = end
	}
	return Text{data: data, offs: offs}
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
		spans = appendSpan(spans, KindContext, oldTokens.span(at, c.i1))
		spans = appendSpan(spans, KindDel, oldTokens.span(c.i1, c.i1+c.chg1))
		spans = appendSpan(spans, KindAdd, newTokens.span(c.i2, c.i2+c.chg2))
		at = c.i1 + c.chg1
	}
	return appendSpan(spans, KindContext, oldTokens.span(at, oldTokens.Count()))
}

func appendSpan(spans []Span, kind Kind, text []byte) []Span {
	if len(text) == 0 {
		return spans
	}
	return append(spans, Span{Kind: kind, Text: string(text)})
}
