package attributes

import "strings"

type SparseOptions struct {
	Cone       bool
	IgnoreCase bool
}

type Sparse struct {
	rules     []Rule
	cone      bool
	full      bool
	recursive map[string]bool
	parents   map[string]bool
	icase     bool
}

type coneMatch uint8

const (
	coneExcluded coneMatch = iota
	coneIncluded
	coneRecursive
)

func ParseSparse(data []byte, opts SparseOptions) *Sparse {
	s := &Sparse{cone: opts.Cone, icase: opts.IgnoreCase, recursive: map[string]bool{}, parents: map[string]bool{}}
	s.rules = parseIgnoreFile("", "", data)
	for _, rule := range s.rules {
		s.addCone(rule.pat)
	}
	return s
}

func (s *Sparse) Cone() bool { return s.cone }

func (s *Sparse) addCone(p pattern) {
	if !s.cone {
		return
	}
	text := p.text
	switch {
	case p.negative && p.dirOnly && text == "/*":
		s.full = false
		return
	case !p.negative && !p.dirOnly && text == "/*":
		s.full = true
		return
	case len(text) < 2 || text[0] != '/' || strings.Contains(text, "**") || !p.dirOnly && text != "/*" || !coneGlobFree(text):
		s.disableCone()
		return
	}
	if len(text) > 2 && strings.HasSuffix(text, "/*") {
		key := s.key(unescapeCone(text))
		if !p.negative || !s.recursive[key] {
			s.disableCone()
			return
		}
		s.parents[key] = true
		delete(s.recursive, key)
		return
	}
	if p.negative {
		s.disableCone()
		return
	}
	key := s.key(unescapeCone(text))
	s.recursive[key] = true
	delete(s.parents, key)
}

func (s *Sparse) disableCone() {
	s.cone = false
	s.recursive, s.parents = nil, nil
}

func globSpecial(c byte) bool {
	return c == '*' || c == '?' || c == '[' || c == '\\'
}

func coneGlobFree(text string) bool {
	for at := 1; at < len(text); at++ {
		c, prev := text[at], text[at-1]
		var next byte
		if at+1 < len(text) {
			next = text[at+1]
		}
		switch {
		case !globSpecial(c), prev == '\\', c == '\\' && globSpecial(next), c == '*' && next == 0 && prev == '/':
			continue
		}
		return false
	}
	return true
}

func unescapeCone(text string) string {
	var out strings.Builder
	for at := 0; at < len(text); at++ {
		if text[at] == '\\' {
			at++
		}
		out.WriteByte(text[at])
	}
	return strings.TrimSuffix(out.String(), "/*")
}

func (s *Sparse) key(path string) string {
	if !s.icase {
		return path
	}
	folded := []byte(path)
	for at := range folded {
		folded[at] = lower(folded[at])
	}
	return string(folded)
}

func (s *Sparse) Includes(path string, isDir bool) bool {
	if s.cone {
		return s.coneIncludes(path)
	}
	included := false
	for at := range len(path) {
		if path[at] != '/' {
			continue
		}
		if rule, ok := lastMatch(s.rules, path[:at], true, s.icase); ok {
			included = !rule.Negative
		}
	}
	if rule, ok := lastMatch(s.rules, path, isDir, s.icase); ok {
		included = !rule.Negative
	}
	return included
}

func (s *Sparse) coneIncludes(path string) bool {
	if s.full {
		return true
	}
	for at := range len(path) {
		if path[at] != '/' {
			continue
		}
		switch s.coneMatch(path[:at]) {
		case coneRecursive:
			return true
		case coneExcluded:
			return false
		}
	}
	return s.coneMatch(path) != coneExcluded
}

func (s *Sparse) coneMatch(path string) coneMatch {
	full := s.key("/" + path)
	if s.recursive[full] {
		return coneRecursive
	}
	slash := strings.LastIndexByte(full, '/')
	if slash == 0 {
		return coneIncluded
	}
	if s.parents[full[:slash]] {
		return coneIncluded
	}
	return coneExcluded
}
