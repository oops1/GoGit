package config

import "strings"

func wildMatch(pattern, text string, icase bool) bool {
	return dowild(pattern, text, icase, true)
}

func eqByte(a, b byte, icase bool) bool {
	if icase {
		return lower(a) == lower(b)
	}
	return a == b
}

func inRange(lo, hi, c byte, icase bool) bool {
	if c >= lo && c <= hi {
		return true
	}
	if icase {
		u := c
		if u >= 'a' && u <= 'z' {
			u = u - 'a' + 'A'
		} else if u >= 'A' && u <= 'Z' {
			u = u - 'A' + 'a'
		}
		return u >= lo && u <= hi
	}
	return false
}

func dowild(p, t string, icase, compStart bool) bool {
	i, j := 0, 0
	cs := compStart
	for i < len(p) {
		if p[i] == '*' {
			return star(p, i, t[j:], icase, cs)
		}
		if j >= len(t) {
			return false
		}
		tc := t[j]
		switch p[i] {
		case '?':
			if tc == '/' {
				return false
			}
			i++
		case '[':
			n, ok := matchBracket(p[i:], tc, icase)
			if !ok {
				return false
			}
			i += n
		default:
			pc := p[i]
			if pc == '\\' && i+1 < len(p) {
				i++
				pc = p[i]
			}
			if !eqByte(pc, tc, icase) {
				return false
			}
			i++
		}
		cs = tc == '/'
		j++
	}
	return j == len(t)
}

func star(p string, i int, tail string, icase, compStart bool) bool {
	k := i
	for k < len(p) && p[k] == '*' {
		k++
	}
	rest := p[k:]
	matchSlash := false
	if k-i > 1 {
		if !compStart || (rest != "" && rest[0] != '/') {
			return false
		}
		if rest == "" {
			return true
		}
		if dowild(rest[1:], tail, icase, true) {
			return true
		}
		matchSlash = true
	}
	if rest == "" {
		return !strings.Contains(tail, "/")
	}
	limit := len(tail)
	if !matchSlash {
		if x := strings.IndexByte(tail, '/'); x >= 0 {
			limit = x
		}
	}
	for m := 0; m <= limit; m++ {
		if dowild(rest, tail[m:], icase, false) {
			return true
		}
	}
	return false
}

func matchBracket(p string, tc byte, icase bool) (int, bool) {
	if tc == '/' {
		return 0, false
	}
	i := 1
	negated := false
	if i < len(p) && (p[i] == '!' || p[i] == '^') {
		negated = true
		i++
	}
	matched := false
	var prev byte
	hasPrev := false
	for {
		if i >= len(p) {
			return 0, false
		}
		c := p[i]
		switch {
		case c == '\\':
			i++
			if i >= len(p) {
				return 0, false
			}
			c = p[i]
			if eqByte(c, tc, icase) {
				matched = true
			}
			prev, hasPrev = c, true
			i++
		case c == '-' && hasPrev && i+1 < len(p) && p[i+1] != ']':
			i++
			c = p[i]
			if c == '\\' {
				i++
				if i >= len(p) {
					return 0, false
				}
				c = p[i]
			}
			if inRange(prev, c, tc, icase) {
				matched = true
			}
			hasPrev = false
			i++
		default:
			if eqByte(c, tc, icase) {
				matched = true
			}
			prev, hasPrev = c, true
			i++
		}
		if i < len(p) && p[i] == ']' {
			i++
			break
		}
	}
	return i, matched != negated
}
