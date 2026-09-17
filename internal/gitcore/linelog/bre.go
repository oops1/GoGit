package linelog

import (
	"fmt"
	"regexp"
	"strings"
)

func compileBRE(pattern string) (*regexp.Regexp, error) {
	translated, err := translateBRE(pattern)
	if err != nil {
		return nil, err
	}
	re, err := regexp.Compile("(?m)" + translated)
	if err != nil {
		return nil, fmt.Errorf("%w: %q: %w", ErrPattern, pattern, err)
	}
	re.Longest()
	return re, nil
}

type breWriter struct {
	out         strings.Builder
	atExprStart bool
}

func translateBRE(pattern string) (string, error) {
	w := &breWriter{atExprStart: true}
	for at := 0; at < len(pattern); {
		next, err := w.step(pattern, at)
		if err != nil {
			return "", err
		}
		at = next
	}
	return w.out.String(), nil
}

func (w *breWriter) step(pattern string, at int) (int, error) {
	start := w.atExprStart
	w.atExprStart = false
	switch pattern[at] {
	case '\\':
		return w.escape(pattern, at)
	case '[':
		return w.bracket(pattern, at)
	case '*':
		if start {
			w.out.WriteString(`\*`)
		} else {
			w.out.WriteByte('*')
		}
	case '^':
		if start {
			w.out.WriteByte('^')
			w.atExprStart = true
		} else {
			w.out.WriteString(`\^`)
		}
	case '$':
		rest := pattern[at+1:]
		if rest == "" || strings.HasPrefix(rest, `\)`) || strings.HasPrefix(rest, `\|`) {
			w.out.WriteByte('$')
		} else {
			w.out.WriteString(`\$`)
		}
	case '.':
		w.out.WriteByte('.')
	default:
		w.out.WriteString(regexp.QuoteMeta(pattern[at : at+1]))
	}
	return at + 1, nil
}

func (w *breWriter) escape(pattern string, at int) (int, error) {
	if at+1 == len(pattern) {
		return 0, fmt.Errorf("%w: %q ends with a backslash", ErrPattern, pattern)
	}
	c := pattern[at+1]
	switch c {
	case '(', '|':
		w.out.WriteByte(c)
		w.atExprStart = true
	case ')', '{', '}', '+', '?':
		w.out.WriteByte(c)
	case '<', '>':
		w.out.WriteString(`\b`)
	case 'b', 'B', 'w', 'W', 's', 'S':
		w.out.WriteByte('\\')
		w.out.WriteByte(c)
	case '`':
		w.out.WriteString(`\A`)
	case '\'':
		w.out.WriteString(`\z`)
	case '1', '2', '3', '4', '5', '6', '7', '8', '9':
		return 0, fmt.Errorf("%w: %q uses a back reference", ErrPattern, pattern)
	default:
		w.out.WriteString(regexp.QuoteMeta(pattern[at+1 : at+2]))
	}
	return at + 2, nil
}

func (w *breWriter) bracket(pattern string, at int) (int, error) {
	end := at + 1
	if end < len(pattern) && pattern[end] == '^' {
		end++
	}
	if end < len(pattern) && pattern[end] == ']' {
		end++
	}
	for end < len(pattern) && pattern[end] != ']' {
		end += classLength(pattern[end:])
	}
	if end >= len(pattern) {
		return 0, fmt.Errorf("%w: %q has an unclosed bracket", ErrPattern, pattern)
	}
	w.out.WriteString(bracketSet(pattern[at+1 : end]))
	return end + 1, nil
}

func bracketSet(body string) string {
	var out strings.Builder
	out.WriteByte('[')
	if rest, negated := strings.CutPrefix(body, "^"); negated {
		out.WriteString(`^\n`)
		body = rest
	}
	for at := 0; at < len(body); {
		size := classLength(body[at:])
		if size == 1 && strings.IndexByte(`\[]^`, body[at]) >= 0 {
			out.WriteByte('\\')
		}
		out.WriteString(body[at : at+size])
		at += size
	}
	out.WriteByte(']')
	return out.String()
}

func classLength(rest string) int {
	if strings.HasPrefix(rest, "[:") {
		if closing := strings.Index(rest[2:], ":]"); closing >= 0 {
			return closing + 4
		}
	}
	return 1
}
