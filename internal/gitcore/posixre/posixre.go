package posixre

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

type Flags uint8

const (
	Extended Flags = 1 << iota
	IgnoreCase
	Newline
)

const (
	maxRepeat      = 0x7fff
	maxSymbolName  = 32
	backRefLimit   = 9
	matchNothing   = `[^\x00-\x{10FFFF}]`
	multiLineFlags = "(?m)"
)

var (
	ErrPattern     = errors.New("posixre: invalid regular expression")
	ErrUnsupported = errors.New("posixre: back references are not supported")

	errBadRepeat  = errors.New("invalid preceding regular expression")
	errParen      = errors.New("unmatched ( or \\(")
	errBackslash  = errors.New("trailing backslash")
	errBadBrace   = errors.New("invalid content of \\{\\}")
	errBrace      = errors.New("unmatched \\{")
	errBracket    = errors.New("unmatched [ or [^")
	errBadPattern = errors.New("invalid regular expression")
	errRange      = errors.New("invalid range end")
	errCollate    = errors.New("invalid collation character")
	errClass      = errors.New("invalid character class name")
	errBackRef    = errors.New("invalid back reference")
)

func Compile(pattern string, flags Flags) (*Regexp, error) {
	translated, err := Translate(pattern, flags)
	if err != nil {
		return nil, err
	}
	re, err := regexp.Compile(translated)
	if err != nil {
		return nil, fmt.Errorf("%w: %q: %w", ErrPattern, pattern, err)
	}
	re.Longest()
	return &Regexp{re: re}, nil
}

func Translate(pattern string, flags Flags) (string, error) {
	p := &parser{raw: pattern, buf: pattern, flags: flags}
	if flags&IgnoreCase != 0 {
		p.buf = upperASCII(pattern)
	}
	var tok token
	p.fetch(&tok, true)
	body, err := p.regExp(&tok, 0)
	if errors.Is(err, ErrUnsupported) {
		return "", fmt.Errorf("%w: %q", err, pattern)
	}
	if err != nil {
		return "", fmt.Errorf("%w: %q: %w", ErrPattern, pattern, err)
	}
	if flags&Newline != 0 {
		return multiLineFlags + body, nil
	}
	return body, nil
}

func upperASCII(s string) string {
	out := []byte(s)
	for at, c := range out {
		out[at] = toUpper(c)
	}
	return string(out)
}

func toUpper(c byte) byte {
	if 'a' <= c && c <= 'z' {
		return c - 'a' + 'A'
	}
	return c
}

type kind uint8

const (
	kindEnd kind = iota
	kindChar
	kindTrailingBackslash
	kindAlt
	kindBackRef
	kindAnchor
	kindClass
	kindOpenGroup
	kindCloseGroup
	kindStar
	kindPlus
	kindQuestion
	kindOpenInterval
	kindCloseInterval
	kindBracket
	kindPeriod
)

type token struct {
	kind kind
	c    byte
	size int
	text string
}

type parser struct {
	raw       string
	buf       string
	pos       int
	flags     Flags
	groups    int
	completed int
}

func (p *parser) extended() bool { return p.flags&Extended != 0 }

func (p *parser) fetch(tok *token, caretHere bool) {
	*tok = p.peek(p.pos, caretHere)
	p.pos += tok.size
}

func (p *parser) peek(at int, caretHere bool) token {
	if at >= len(p.buf) {
		return token{kind: kindEnd}
	}
	c := p.buf[at]
	if c == '\\' {
		if at+1 >= len(p.buf) {
			return token{kind: kindTrailingBackslash, c: c, size: 1}
		}
		return p.peekEscape(p.raw[at+1])
	}
	tok := token{kind: kindChar, c: c, size: 1}
	switch c {
	case '|':
		tok.kind = p.whenExtended(kindAlt)
	case '*':
		tok.kind = kindStar
	case '+':
		tok.kind = p.whenExtended(kindPlus)
	case '?':
		tok.kind = p.whenExtended(kindQuestion)
	case '{':
		tok.kind = p.whenExtended(kindOpenInterval)
	case '}':
		tok.kind = p.whenExtended(kindCloseInterval)
	case '(':
		tok.kind = p.whenExtended(kindOpenGroup)
	case ')':
		tok.kind = p.whenExtended(kindCloseGroup)
	case '[':
		tok.kind = kindBracket
	case '.':
		tok.kind = kindPeriod
	case '^':
		if p.extended() || caretHere || at == 0 {
			tok.kind, tok.text = kindAnchor, "^"
		}
	case '$':
		if p.extended() || at+1 == len(p.buf) || p.closesBranch(at+1) {
			tok.kind, tok.text = kindAnchor, "$"
		}
	}
	return tok
}

func (p *parser) whenExtended(special kind) kind {
	if p.extended() {
		return special
	}
	return kindChar
}

func (p *parser) whenBasic(special kind) kind {
	if p.extended() {
		return kindChar
	}
	return special
}

func (p *parser) closesBranch(next int) bool {
	following := p.peek(next, false)
	return following.kind == kindAlt || following.kind == kindCloseGroup
}

func (p *parser) peekEscape(c byte) token {
	tok := token{kind: kindChar, c: c, size: 2}
	switch c {
	case '|':
		tok.kind = p.whenBasic(kindAlt)
	case '1', '2', '3', '4', '5', '6', '7', '8', '9':
		tok.kind = kindBackRef
	case '<', '>', 'b':
		tok.kind, tok.text = kindAnchor, `\b`
	case 'B':
		tok.kind, tok.text = kindAnchor, `\B`
	case '`':
		tok.kind, tok.text = kindAnchor, `\A`
	case '\'':
		tok.kind, tok.text = kindAnchor, `\z`
	case 'w':
		tok.kind, tok.text = kindClass, `[0-9A-Z_a-z]`
	case 'W':
		tok.kind, tok.text = kindClass, `[^0-9A-Z_a-z]`
	case 's':
		tok.kind, tok.text = kindClass, `[\t-\r ]`
	case 'S':
		tok.kind, tok.text = kindClass, `[^\t-\r ]`
	case '(':
		tok.kind = p.whenBasic(kindOpenGroup)
	case ')':
		tok.kind = p.whenBasic(kindCloseGroup)
	case '+':
		tok.kind = p.whenBasic(kindPlus)
	case '?':
		tok.kind = p.whenBasic(kindQuestion)
	case '{':
		tok.kind = p.whenBasic(kindOpenInterval)
	case '}':
		tok.kind = p.whenBasic(kindCloseInterval)
	}
	return tok
}

func endsBranch(tok token, nest int) bool {
	return tok.kind == kindAlt || tok.kind == kindEnd || nest > 0 && tok.kind == kindCloseGroup
}

func (p *parser) regExp(tok *token, nest int) (string, error) {
	var out strings.Builder
	first, err := p.branch(tok, nest)
	if err != nil {
		return "", err
	}
	out.WriteString(first)
	for tok.kind == kindAlt {
		p.fetch(tok, true)
		out.WriteByte('|')
		if endsBranch(*tok, nest) {
			continue
		}
		next, err := p.branch(tok, nest)
		if err != nil {
			return "", err
		}
		out.WriteString(next)
	}
	return out.String(), nil
}

func (p *parser) branch(tok *token, nest int) (string, error) {
	var out strings.Builder
	for {
		piece, err := p.expression(tok, nest)
		if err != nil {
			return "", err
		}
		out.WriteString(piece)
		if endsBranch(*tok, nest) {
			return out.String(), nil
		}
	}
}

func (p *parser) expression(tok *token, nest int) (string, error) {
	var atom string
	switch tok.kind {
	case kindChar, kindCloseInterval:
		atom = p.literal(tok.c)
	case kindOpenGroup:
		group, err := p.group(tok, nest+1)
		if err != nil {
			return "", err
		}
		atom = group
	case kindBracket:
		set, err := p.bracket()
		if err != nil {
			return "", err
		}
		atom = set
	case kindBackRef:
		if p.completed&(1<<(tok.c-'1')) == 0 {
			return "", errBackRef
		}
		return "", ErrUnsupported
	case kindOpenInterval:
		return "", errBadRepeat
	case kindStar, kindPlus, kindQuestion:
		if p.extended() {
			return "", errBadRepeat
		}
		atom = p.literal(tok.c)
	case kindCloseGroup:
		if !p.extended() {
			return "", errParen
		}
		atom = p.literal(tok.c)
	case kindAnchor:
		anchor := tok.text
		p.fetch(tok, false)
		return anchor, nil
	case kindPeriod:
		atom = p.period()
	case kindClass:
		atom = tok.text
	case kindTrailingBackslash:
		return "", errBackslash
	default:
		return "", nil
	}
	p.fetch(tok, false)
	return p.repeats(tok, atom)
}

func (p *parser) literal(c byte) string {
	if p.flags&IgnoreCase != 0 {
		switch {
		case 'a' <= c && c <= 'z':
			return matchNothing
		case 'A' <= c && c <= 'Z':
			return "[" + string(c) + string(c-'A'+'a') + "]"
		}
	}
	if c < ' ' || c >= 0x7f {
		return fmt.Sprintf(`\x{%02x}`, c)
	}
	return regexp.QuoteMeta(string(c))
}

func (p *parser) period() string {
	if p.flags&Newline != 0 {
		return `[^\x00\n]`
	}
	return `[^\x00]`
}

func (p *parser) group(tok *token, nest int) (string, error) {
	index := p.groups
	p.groups++
	p.fetch(tok, true)
	body := ""
	if tok.kind != kindCloseGroup {
		inner, err := p.regExp(tok, nest)
		if err != nil {
			return "", err
		}
		if tok.kind != kindCloseGroup {
			return "", errParen
		}
		body = inner
	}
	if index < backRefLimit {
		p.completed |= 1 << index
	}
	return "(" + body + ")", nil
}

func isRepeat(k kind) bool {
	return k == kindStar || k == kindPlus || k == kindQuestion || k == kindOpenInterval
}

func (p *parser) repeats(tok *token, atom string) (string, error) {
	out := atom
	for applied := 0; isRepeat(tok.kind); applied++ {
		op, err := p.repeatOperator(tok)
		if err != nil {
			return "", err
		}
		if applied > 0 {
			out = "(?:" + out + ")"
		}
		out += op
		if !p.extended() && (tok.kind == kindStar || tok.kind == kindOpenInterval) {
			return "", errBadRepeat
		}
	}
	return out, nil
}

func (p *parser) repeatOperator(tok *token) (string, error) {
	op := ""
	switch tok.kind {
	case kindStar:
		op = "*"
	case kindPlus:
		op = "+"
	case kindQuestion:
		op = "?"
	default:
		interval, err := p.interval(tok)
		if err != nil {
			return "", err
		}
		op = interval
	}
	p.fetch(tok, false)
	return op, nil
}

func (p *parser) interval(tok *token) (string, error) {
	start := p.number(tok)
	if start == -1 {
		if tok.kind != kindChar || tok.c != ',' {
			return "", errBadBrace
		}
		start = 0
	}
	end := -2
	switch {
	case start == -2:
	case tok.kind == kindCloseInterval:
		end = start
	case tok.kind == kindChar && tok.c == ',':
		end = p.number(tok)
	}
	if start == -2 || end == -2 {
		if tok.kind == kindEnd {
			return "", errBrace
		}
		return "", errBadBrace
	}
	if end != -1 && start > end || tok.kind != kindCloseInterval {
		return "", errBadBrace
	}
	switch end {
	case -1:
		return fmt.Sprintf("{%d,}", start), nil
	case start:
		return fmt.Sprintf("{%d}", start), nil
	default:
		return fmt.Sprintf("{%d,%d}", start, end), nil
	}
}

func (p *parser) number(tok *token) int {
	num := -1
	for {
		p.fetch(tok, false)
		if tok.kind == kindEnd {
			return -2
		}
		if tok.kind == kindCloseInterval || tok.c == ',' {
			return num
		}
		switch {
		case tok.kind != kindChar || tok.c < '0' || '9' < tok.c || num == -2:
			num = -2
		case num == -1:
			num = int(tok.c - '0')
		default:
			num = num*10 + int(tok.c-'0')
		}
		if num > maxRepeat {
			num = -2
		}
	}
}
