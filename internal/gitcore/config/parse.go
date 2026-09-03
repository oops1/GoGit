package config

import (
	"fmt"
	"strings"
)

type itemKind uint8

const (
	kindSpace itemKind = iota
	kindNewline
	kindComment
	kindBOM
	kindSection
	kindEntry
)

const byteOrderMark = "\xef\xbb\xbf"

type item struct {
	kind     itemKind
	raw      string
	name     string
	sub      string
	hasSub   bool
	rawName  string
	assign   string
	rawValue string
	value    string
	hasValue bool
}

func (it *item) text() string {
	if it.kind == kindEntry {
		return it.rawName + it.assign + it.rawValue
	}
	return it.raw
}

func (it *item) matchesSection(n name) bool {
	return it.name == n.section && it.hasSub == n.hasSub && it.sub == n.sub
}

func isAlpha(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func isKeyChar(c byte) bool { return isAlpha(c) || isDigit(c) || c == '-' }

func isBlank(c byte) bool {
	return c == ' ' || c == '\t' || c == '\r' || c == '\v' || c == '\f'
}

func lower(c byte) byte {
	if c >= 'A' && c <= 'Z' {
		return c + 'a' - 'A'
	}
	return c
}

type parser struct {
	src   string
	pos   int
	line  int
	items []*item
}

func Parse(data []byte) (*File, error) {
	p := &parser{src: string(data), line: 1}
	if err := p.run(); err != nil {
		return nil, err
	}
	return &File{items: p.items}, nil
}

func (p *parser) fail(base error) error {
	return fmt.Errorf("gitconfig: line %d: %w", p.line, base)
}

func (p *parser) emit(it *item) {
	p.items = append(p.items, it)
}

func (p *parser) atNewline() bool {
	if p.pos >= len(p.src) {
		return true
	}
	c := p.src[p.pos]
	return c == '\n' || (c == '\r' && p.pos+1 < len(p.src) && p.src[p.pos+1] == '\n')
}

func (p *parser) run() error {
	if strings.HasPrefix(p.src, byteOrderMark) {
		p.emit(&item{kind: kindBOM, raw: byteOrderMark})
		p.pos = len(byteOrderMark)
	}
	for p.pos < len(p.src) {
		c := p.src[p.pos]
		switch {
		case p.atNewline():
			p.newline()
		case isBlank(c):
			p.blank()
		case c == '#' || c == ';':
			p.comment()
		case c == '[':
			if err := p.section(); err != nil {
				return err
			}
		case isAlpha(c):
			if err := p.entry(); err != nil {
				return err
			}
		default:
			return p.fail(ErrSyntax)
		}
	}
	return nil
}

func (p *parser) newline() {
	start := p.pos
	if p.src[p.pos] == '\r' {
		p.pos++
	}
	p.pos++
	p.line++
	p.emit(&item{kind: kindNewline, raw: p.src[start:p.pos]})
}

func (p *parser) blank() {
	start := p.pos
	for p.pos < len(p.src) && !p.atNewline() && isBlank(p.src[p.pos]) {
		p.pos++
	}
	p.emit(&item{kind: kindSpace, raw: p.src[start:p.pos]})
}

func (p *parser) comment() {
	start := p.pos
	for !p.atNewline() {
		p.pos++
	}
	p.emit(&item{kind: kindComment, raw: p.src[start:p.pos]})
}

func (p *parser) section() error {
	start := p.pos
	p.pos++
	nameStart := p.pos
	for {
		if p.atNewline() {
			return p.fail(ErrBadSection)
		}
		c := p.src[p.pos]
		if c == ']' {
			text := strings.ToLower(p.src[nameStart:p.pos])
			p.pos++
			if text == "" {
				return p.fail(ErrBadSection)
			}
			sec, sub, hasSub := splitLegacySection(text)
			p.emit(&item{kind: kindSection, raw: p.src[start:p.pos], name: sec, sub: sub, hasSub: hasSub})
			return nil
		}
		if isBlank(c) {
			return p.extendedSection(start, strings.ToLower(p.src[nameStart:p.pos]))
		}
		if !isKeyChar(c) && c != '.' {
			return p.fail(ErrBadSection)
		}
		p.pos++
	}
}

func splitLegacySection(text string) (string, string, bool) {
	if i := strings.IndexByte(text, '.'); i >= 0 {
		return text[:i], text[i+1:], true
	}
	return text, "", false
}

func (p *parser) extendedSection(start int, sec string) error {
	for !p.atNewline() && isBlank(p.src[p.pos]) {
		p.pos++
	}
	if p.atNewline() || p.src[p.pos] != '"' {
		return p.fail(ErrBadSection)
	}
	p.pos++
	var sub strings.Builder
	for {
		if p.atNewline() {
			return p.fail(ErrBadSection)
		}
		c := p.src[p.pos]
		p.pos++
		if c == '"' {
			break
		}
		if c == '\\' {
			if p.atNewline() {
				return p.fail(ErrBadSection)
			}
			c = p.src[p.pos]
			p.pos++
		}
		sub.WriteByte(c)
	}
	if p.atNewline() || p.src[p.pos] != ']' {
		return p.fail(ErrBadSection)
	}
	p.pos++
	p.emit(&item{kind: kindSection, raw: p.src[start:p.pos], name: sec, sub: sub.String(), hasSub: true})
	return nil
}

func (p *parser) entry() error {
	keyStart := p.pos
	for p.pos < len(p.src) && isKeyChar(p.src[p.pos]) {
		p.pos++
	}
	rawKey := p.src[keyStart:p.pos]
	q := p.pos
	for q < len(p.src) && (p.src[q] == ' ' || p.src[q] == '\t') {
		q++
	}
	if q >= len(p.src) || p.src[q] == '\n' || (p.src[q] == '\r' && q+1 < len(p.src) && p.src[q+1] == '\n') {
		p.emit(&item{kind: kindEntry, rawName: rawKey, name: strings.ToLower(rawKey)})
		return nil
	}
	if p.src[q] != '=' {
		return p.fail(ErrBadKey)
	}
	q++
	for q < len(p.src) && (p.src[q] == ' ' || p.src[q] == '\t') {
		q++
	}
	assign := p.src[p.pos:q]
	p.pos = q
	value, rawValue, err := p.parseValue()
	if err != nil {
		return err
	}
	p.emit(&item{
		kind:     kindEntry,
		rawName:  rawKey,
		name:     strings.ToLower(rawKey),
		assign:   assign,
		rawValue: rawValue,
		value:    value,
		hasValue: true,
	})
	return nil
}

func (p *parser) parseValue() (string, string, error) {
	start := p.pos
	lastSig := p.pos
	var buf []byte
	quoted := false
	commented := false
	trim := -1
	for {
		if p.atNewline() {
			if quoted {
				return "", "", p.fail(ErrUnterminatedQuote)
			}
			if trim >= 0 {
				buf = buf[:trim]
			}
			p.pos = lastSig
			return string(buf), p.src[start:lastSig], nil
		}
		c := p.src[p.pos]
		p.pos++
		if commented {
			continue
		}
		if isBlank(c) && !quoted {
			if trim < 0 {
				trim = len(buf)
			}
			if len(buf) > 0 {
				buf = append(buf, c)
			}
			continue
		}
		if !quoted && (c == ';' || c == '#') {
			commented = true
			continue
		}
		trim = -1
		if c == '\\' {
			if p.atNewline() {
				if p.pos < len(p.src) {
					p.newlineInValue()
				}
				lastSig = p.pos
				continue
			}
			e := p.src[p.pos]
			p.pos++
			switch e {
			case 't':
				c = '\t'
			case 'b':
				c = '\b'
			case 'n':
				c = '\n'
			case '\\', '"':
				c = e
			default:
				return "", "", p.fail(ErrBadEscape)
			}
			buf = append(buf, c)
			lastSig = p.pos
			continue
		}
		if c == '"' {
			quoted = !quoted
			lastSig = p.pos
			continue
		}
		buf = append(buf, c)
		lastSig = p.pos
	}
}

func (p *parser) newlineInValue() {
	if p.src[p.pos] == '\r' {
		p.pos++
	}
	p.pos++
	p.line++
}
