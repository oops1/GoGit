package posixre

import (
	"fmt"
	"strings"
)

type bracketKind uint8

const (
	bracketEnd bracketKind = iota
	bracketChar
	bracketCollating
	bracketEquivalence
	bracketClass
	bracketRange
	bracketClose
	bracketNegate
)

type bracketToken struct {
	kind bracketKind
	c    byte
	size int
}

type elementKind uint8

const (
	elementChar elementKind = iota
	elementCollating
	elementEquivalence
	elementClass
)

type element struct {
	kind elementKind
	c    byte
	name string
}

const byteValues = 256

type charset [byteValues]bool

func (p *parser) peekBracket(at int) bracketToken {
	if at >= len(p.buf) {
		return bracketToken{kind: bracketEnd}
	}
	c := p.buf[at]
	tok := bracketToken{kind: bracketChar, c: c, size: 1}
	switch c {
	case '[':
		if at+1 < len(p.buf) {
			switch p.buf[at+1] {
			case '.':
				tok.kind, tok.size = bracketCollating, 2
			case '=':
				tok.kind, tok.size = bracketEquivalence, 2
			case ':':
				tok.kind, tok.size = bracketClass, 2
			}
		}
	case '-':
		tok.kind = bracketRange
	case ']':
		tok.kind = bracketClose
	case '^':
		tok.kind = bracketNegate
	}
	return tok
}

func (p *parser) bracket() (string, error) {
	tok := p.peekBracket(p.pos)
	if tok.kind == bracketEnd {
		return "", errBadPattern
	}
	negated := tok.kind == bracketNegate
	if negated {
		p.pos += tok.size
		if tok = p.peekBracket(p.pos); tok.kind == bracketEnd {
			return "", errBadPattern
		}
	}
	if tok.kind == bracketClose {
		tok.kind = bracketChar
	}
	var set charset
	for first := true; ; first = false {
		start, err := p.bracketElement(tok, first)
		if err != nil {
			return "", err
		}
		tok = p.peekBracket(p.pos)
		end, ranged, err := p.rangeEnd(&tok, start)
		if err != nil {
			return "", err
		}
		if ranged {
			err = addRange(&set, start, end)
		} else {
			err = p.addElement(&set, start)
		}
		if err != nil {
			return "", err
		}
		if tok.kind == bracketEnd {
			return "", errBracket
		}
		if tok.kind == bracketClose {
			break
		}
	}
	p.pos += tok.size
	return p.emitSet(&set, negated), nil
}

func (p *parser) rangeEnd(tok *bracketToken, start element) (element, bool, error) {
	if start.kind == elementClass || start.kind == elementEquivalence {
		return element{}, false, nil
	}
	if tok.kind == bracketEnd {
		return element{}, false, errBracket
	}
	if tok.kind != bracketRange {
		return element{}, false, nil
	}
	p.pos += tok.size
	next := p.peekBracket(p.pos)
	switch next.kind {
	case bracketEnd:
		return element{}, false, errBracket
	case bracketClose:
		p.pos -= tok.size
		tok.kind = bracketChar
		return element{}, false, nil
	}
	end, err := p.bracketElement(next, true)
	if err != nil {
		return element{}, false, err
	}
	*tok = p.peekBracket(p.pos)
	return end, true, nil
}

func (p *parser) bracketElement(tok bracketToken, acceptHyphen bool) (element, error) {
	p.pos += tok.size
	switch tok.kind {
	case bracketCollating:
		return p.bracketSymbol('.', elementCollating)
	case bracketEquivalence:
		return p.bracketSymbol('=', elementEquivalence)
	case bracketClass:
		return p.bracketSymbol(':', elementClass)
	case bracketRange:
		if !acceptHyphen && p.peekBracket(p.pos).kind != bracketClose {
			return element{}, errRange
		}
	}
	return element{kind: elementChar, c: tok.c}, nil
}

func (p *parser) bracketSymbol(delim byte, kind elementKind) (element, error) {
	source := p.buf
	if kind == elementClass {
		source = p.raw
	}
	if p.pos >= len(p.buf) {
		return element{}, errBracket
	}
	var name strings.Builder
	for {
		if name.Len() >= maxSymbolName {
			return element{}, errBracket
		}
		ch := source[p.pos]
		p.pos++
		if p.pos >= len(p.buf) {
			return element{}, errBracket
		}
		if ch == delim && p.buf[p.pos] == ']' {
			break
		}
		name.WriteByte(ch)
	}
	p.pos++
	return element{kind: kind, name: name.String()}, nil
}

func (p *parser) addElement(set *charset, e element) error {
	switch e.kind {
	case elementClass:
		members, ok := p.classMembers(e.name)
		if !ok {
			return errClass
		}
		for c := range byteValues {
			set[c] = set[c] || members(byte(c))
		}
	case elementCollating, elementEquivalence:
		if len(e.name) != 1 {
			return errCollate
		}
		set[e.name[0]] = true
	default:
		set[e.c] = true
	}
	return nil
}

func addRange(set *charset, start, end element) error {
	if end.kind == elementClass || end.kind == elementEquivalence {
		return errRange
	}
	lo, err := rangePoint(start)
	if err != nil {
		return err
	}
	hi, err := rangePoint(end)
	if err != nil {
		return err
	}
	if lo > hi {
		return errRange
	}
	for c := int(lo); c <= int(hi); c++ {
		set[c] = true
	}
	return nil
}

func rangePoint(e element) (byte, error) {
	if e.kind != elementCollating {
		return e.c, nil
	}
	switch len(e.name) {
	case 0:
		return 0, nil
	case 1:
		return e.name[0], nil
	default:
		return 0, errCollate
	}
}

func (p *parser) classMembers(name string) (func(byte) bool, bool) {
	if p.flags&IgnoreCase != 0 && (name == "upper" || name == "lower") {
		name = "alpha"
	}
	members, ok := classes[name]
	return members, ok
}

var classes = map[string]func(byte) bool{
	"alpha":  isAlpha,
	"upper":  func(c byte) bool { return 'A' <= c && c <= 'Z' },
	"lower":  func(c byte) bool { return 'a' <= c && c <= 'z' },
	"digit":  isDigit,
	"alnum":  func(c byte) bool { return isAlpha(c) || isDigit(c) },
	"xdigit": func(c byte) bool { return isDigit(c) || 'a' <= c && c <= 'f' || 'A' <= c && c <= 'F' },
	"space":  func(c byte) bool { return c == ' ' || '\t' <= c && c <= '\r' },
	"blank":  func(c byte) bool { return c == ' ' || c == '\t' },
	"cntrl":  func(c byte) bool { return c < ' ' || c == 0x7f },
	"print":  func(c byte) bool { return ' ' <= c && c < 0x7f },
	"graph":  func(c byte) bool { return ' ' < c && c < 0x7f },
	"punct":  func(c byte) bool { return ' ' < c && c < 0x7f && !isAlpha(c) && !isDigit(c) },
}

func isAlpha(c byte) bool { return 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' }

func isDigit(c byte) bool { return '0' <= c && c <= '9' }

func (p *parser) emitSet(set *charset, negated bool) string {
	var listed []int
	for c := range byteValues {
		if p.matches(set, byte(c), negated) != negated {
			listed = append(listed, c)
		}
	}
	var out strings.Builder
	out.WriteByte('[')
	if negated {
		out.WriteByte('^')
	}
	for at := 0; at < len(listed); {
		last := at
		for last+1 < len(listed) && listed[last+1] == listed[last]+1 {
			last++
		}
		out.WriteString(classByte(listed[at]))
		if last > at {
			out.WriteByte('-')
			out.WriteString(classByte(listed[last]))
		}
		at = last + 1
	}
	out.WriteByte(']')
	return out.String()
}

func (p *parser) matches(set *charset, c byte, negated bool) bool {
	if negated && c == '\n' && p.flags&Newline != 0 {
		return false
	}
	normalized := c
	if p.flags&IgnoreCase != 0 {
		normalized = toUpper(c)
	}
	return set[normalized] != negated
}

func classByte(c int) string {
	if c < 0x80 && (isAlpha(byte(c)) || isDigit(byte(c))) {
		return string(rune(c))
	}
	return fmt.Sprintf(`\x{%x}`, c)
}
