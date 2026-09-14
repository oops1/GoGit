package wildmatch

import "strings"

type Flags uint8

const (
	CaseFold Flags = 1 << iota
	Pathname
)

type outcome uint8

const (
	noMatch outcome = iota
	matched
	abortAll
	abortToStarStar
)

func Match(pattern, text string, flags Flags) bool {
	return dowild(pattern, text, flags, true) == matched
}

func lower(c byte) byte {
	if c >= 'A' && c <= 'Z' {
		return c + 'a' - 'A'
	}
	return c
}

func eqByte(a, b byte, flags Flags) bool {
	if flags&CaseFold != 0 {
		return lower(a) == lower(b)
	}
	return a == b
}

func inRange(lo, hi, c byte, flags Flags) bool {
	if c >= lo && c <= hi {
		return true
	}
	if flags&CaseFold != 0 {
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

func dowild(p, t string, flags Flags, compStart bool) outcome {
	i, j := 0, 0
	cs := compStart
	for i < len(p) {
		if p[i] == '*' {
			return star(p, i, t[j:], flags, cs)
		}
		if j >= len(t) {
			return abortAll
		}
		tc := t[j]
		switch p[i] {
		case '?':
			if tc == '/' && flags&Pathname != 0 {
				return noMatch
			}
			i++
		case '[':
			n, ok := matchBracket(p[i:], tc, flags)
			if !ok {
				return noMatch
			}
			i += n
		default:
			pc := p[i]
			if pc == '\\' && i+1 < len(p) {
				i++
				pc = p[i]
			}
			if !eqByte(pc, tc, flags) {
				return noMatch
			}
			i++
		}
		cs = tc == '/'
		j++
	}
	if j == len(t) {
		return matched
	}
	return noMatch
}

func star(p string, i int, tail string, flags Flags, compStart bool) outcome {
	k := i
	for k < len(p) && p[k] == '*' {
		k++
	}
	rest := p[k:]
	pathname := flags&Pathname != 0
	matchSlash := !pathname
	if k-i > 1 && pathname && compStart && (i == 0 || p[i-1] == '/') && (rest == "" || rest[0] == '/') {
		if rest == "" {
			return matched
		}
		if dowild(rest[1:], tail, flags, true) == matched {
			return matched
		}
		matchSlash = true
	}
	if rest == "" {
		if matchSlash || !strings.Contains(tail, "/") {
			return matched
		}
		return noMatch
	}
	for m := range len(tail) {
		result := dowild(rest, tail[m:], flags, false)
		if result != noMatch {
			if !matchSlash || result != abortToStarStar {
				return result
			}
		} else if !matchSlash && tail[m] == '/' {
			return abortToStarStar
		}
	}
	return abortAll
}

func matchBracket(p string, tc byte, flags Flags) (int, bool) {
	if tc == '/' && flags&Pathname != 0 {
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
			if eqByte(c, tc, flags) {
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
			if inRange(prev, c, tc, flags) {
				matched = true
			}
			hasPrev = false
			i++
		default:
			if eqByte(c, tc, flags) {
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
