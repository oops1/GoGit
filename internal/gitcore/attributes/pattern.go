package attributes

import (
	"strings"

	"github.com/oops1/gogit/internal/gitcore/wildmatch"
)

type pattern struct {
	text     string
	base     string
	dirOnly  bool
	negative bool
	noDir    bool
}

func parsePattern(text, base string) pattern {
	p := pattern{base: base}
	if strings.HasPrefix(text, "!") {
		p.negative = true
		text = text[1:]
	}
	if strings.HasSuffix(text, "/") {
		p.dirOnly = true
		text = text[:len(text)-1]
	}
	p.text = text
	p.noDir = !strings.Contains(text, "/")
	return p
}

func (p pattern) match(path string, isDir, icase bool) bool {
	if p.dirOnly && !isDir {
		return false
	}
	if p.noDir {
		return wildmatch.Match(p.text, baseName(path), foldFlags(icase))
	}
	return p.matchPathname(path, icase)
}

func (p pattern) matchPathname(path string, icase bool) bool {
	text := strings.TrimPrefix(p.text, "/")
	name := path
	if p.base != "" {
		if len(path) < len(p.base)+1 || path[len(p.base)] != '/' || !equalFold(path[:len(p.base)], p.base, icase) {
			return false
		}
		name = path[len(p.base)+1:]
	} else if path == "" {
		return false
	}
	return wildmatch.Match(text, name, wildmatch.Pathname|foldFlags(icase))
}

func foldFlags(icase bool) wildmatch.Flags {
	if icase {
		return wildmatch.CaseFold
	}
	return 0
}

func equalFold(a, b string, icase bool) bool {
	if !icase {
		return a == b
	}
	if len(a) != len(b) {
		return false
	}
	for i := range len(a) {
		if lower(a[i]) != lower(b[i]) {
			return false
		}
	}
	return true
}

func lower(c byte) byte {
	if c >= 'A' && c <= 'Z' {
		return c + 'a' - 'A'
	}
	return c
}

func baseName(path string) string {
	if i := strings.LastIndexByte(path, '/'); i >= 0 {
		return path[i+1:]
	}
	return path
}

func parentDir(path string) string {
	if i := strings.LastIndexByte(path, '/'); i >= 0 {
		return path[:i]
	}
	return ""
}

func normalizePath(path string) (string, bool) {
	path = strings.TrimPrefix(path, "./")
	dir := false
	for strings.HasSuffix(path, "/") {
		path = path[:len(path)-1]
		dir = true
	}
	return path, dir
}

const utf8BOM = "\xef\xbb\xbf"

func lines(data []byte) []string {
	text := strings.TrimPrefix(string(data), utf8BOM)
	if text == "" {
		return nil
	}
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	out := strings.Split(text, "\n")
	return out[:len(out)-1]
}

func trimTrailingSpaces(s string) string {
	last := -1
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case ' ':
			if last < 0 {
				last = i
			}
		case '\\':
			i++
			if i >= len(s) {
				return s
			}
			last = -1
		default:
			last = -1
		}
	}
	if last >= 0 {
		return s[:last]
	}
	return s
}

func unquoteC(s string) (string, string, bool) {
	if len(s) == 0 || s[0] != '"' {
		return "", "", false
	}
	var out strings.Builder
	i := 1
	for i < len(s) {
		c := s[i]
		i++
		switch c {
		case '"':
			return out.String(), s[i:], true
		case '\\':
		default:
			out.WriteByte(c)
			continue
		}
		if i >= len(s) {
			return "", "", false
		}
		esc := s[i]
		i++
		switch esc {
		case 'a':
			out.WriteByte('\a')
		case 'b':
			out.WriteByte('\b')
		case 'f':
			out.WriteByte('\f')
		case 'n':
			out.WriteByte('\n')
		case 'r':
			out.WriteByte('\r')
		case 't':
			out.WriteByte('\t')
		case 'v':
			out.WriteByte('\v')
		case '\\', '"':
			out.WriteByte(esc)
		default:
			if esc < '0' || esc > '7' || i+1 >= len(s) {
				return "", "", false
			}
			value := int(esc-'0') << 6
			for shift := 3; shift >= 0; shift -= 3 {
				digit := s[i]
				if digit < '0' || digit > '7' {
					return "", "", false
				}
				value |= int(digit-'0') << shift
				i++
			}
			out.WriteByte(byte(value))
		}
	}
	return "", "", false
}
