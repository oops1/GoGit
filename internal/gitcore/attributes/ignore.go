package attributes

import (
	"cmp"
	"iter"
	"slices"
	"strings"
)

const DefaultIgnoreFile = ".gitignore"

type Rule struct {
	Source   string
	Line     int
	Pattern  string
	Negative bool
	DirOnly  bool

	pat pattern
}

func (r Rule) Valid() bool { return r.Line > 0 }

func (r Rule) String() string {
	if !r.Valid() {
		return ""
	}
	var b strings.Builder
	if r.Negative {
		b.WriteByte('!')
	}
	b.WriteString(r.Pattern)
	if r.DirOnly {
		b.WriteByte('/')
	}
	return b.String()
}

func parseIgnoreFile(source, base string, data []byte) []Rule {
	var rules []Rule
	for i, raw := range lines(data) {
		if raw == "" || raw[0] == '#' {
			continue
		}
		text := trimTrailingSpaces(strings.TrimSuffix(raw, "\r"))
		pat := parsePattern(text, base)
		rules = append(rules, Rule{
			Source:   source,
			Line:     i + 1,
			Pattern:  pat.text,
			Negative: pat.negative,
			DirOnly:  pat.dirOnly,
			pat:      pat,
		})
	}
	return rules
}

func lastMatch(rules []Rule, path string, isDir, icase bool) (Rule, bool) {
	for i := len(rules) - 1; i >= 0; i-- {
		if rules[i].pat.match(path, isDir, icase) {
			return rules[i], true
		}
	}
	return Rule{}, false
}

type IgnoreOptions struct {
	Work         Loader
	Global       Loader
	PerDirectory string
	InfoExclude  string
	ExcludesFile string
	IgnoreCase   bool
}

type Path struct {
	Name  string
	IsDir bool
}

type Match struct {
	Path    string
	IsDir   bool
	Ignored bool
	Rule    Rule
}

type ignoreNode struct {
	parent  *ignoreNode
	rules   []Rule
	blocked bool
	rule    Rule
	err     error
}

type Matcher struct {
	opts        IgnoreOptions
	perDir      *fileCache[Rule]
	globalFiles *fileCache[Rule]
	nodes       map[string]*ignoreNode
	globals     [][]Rule
	globalErr   error
	globalsRead bool
}

func NewMatcher(opts IgnoreOptions) *Matcher {
	if opts.PerDirectory == "" {
		opts.PerDirectory = DefaultIgnoreFile
	}
	return &Matcher{
		opts: opts,
		perDir: newFileCache(opts.Work, func(source string, data []byte) []Rule {
			return parseIgnoreFile(source, parentDir(source), data)
		}),
		globalFiles: newFileCache(opts.Global, func(source string, data []byte) []Rule {
			return parseIgnoreFile(source, "", data)
		}),
		nodes: map[string]*ignoreNode{},
	}
}

func (m *Matcher) globalLists() ([][]Rule, error) {
	if m.globalsRead {
		return m.globals, m.globalErr
	}
	m.globalsRead = true
	for _, name := range []string{m.opts.InfoExclude, m.opts.ExcludesFile} {
		rules, err := m.globalFiles.get(name)
		if err != nil && m.globalErr == nil {
			m.globalErr = err
		}
		m.globals = append(m.globals, rules)
	}
	return m.globals, m.globalErr
}

func (m *Matcher) dirFile(dir string) string {
	if dir == "" {
		return m.opts.PerDirectory
	}
	return dir + "/" + m.opts.PerDirectory
}

func (m *Matcher) node(dir string) *ignoreNode {
	if n, ok := m.nodes[dir]; ok {
		return n
	}
	n := &ignoreNode{}
	m.nodes[dir] = n
	if dir != "" {
		n.parent = m.node(parentDir(dir))
		n.err = n.parent.err
		if n.parent.blocked {
			n.blocked, n.rule = true, n.parent.rule
			return n
		}
		if rule, ok := m.matchRules(n.parent, dir, true); ok && !rule.Negative {
			n.blocked, n.rule = true, rule
			return n
		}
	}
	rules, err := m.perDir.get(m.dirFile(dir))
	n.rules = rules
	if n.err == nil {
		n.err = err
	}
	return n
}

func (m *Matcher) matchRules(n *ignoreNode, path string, isDir bool) (Rule, bool) {
	for cur := n; cur != nil; cur = cur.parent {
		if rule, ok := lastMatch(cur.rules, path, isDir, m.opts.IgnoreCase); ok {
			return rule, true
		}
	}
	lists, _ := m.globalLists()
	for _, list := range lists {
		if rule, ok := lastMatch(list, path, isDir, m.opts.IgnoreCase); ok {
			return rule, true
		}
	}
	return Rule{}, false
}

func (m *Matcher) Ignored(path string, isDir bool) (bool, Rule) {
	match, _ := m.Lookup(path, isDir)
	return match.Ignored, match.Rule
}

func (m *Matcher) Lookup(path string, isDir bool) (Match, error) {
	clean, trailing := normalizePath(path)
	isDir = isDir || trailing
	result := Match{Path: path, IsDir: isDir}
	if clean == "" {
		return result, nil
	}
	n := m.node(parentDir(clean))
	_, globalErr := m.globalLists()
	err := cmp.Or(n.err, globalErr)
	if n.blocked {
		result.Ignored, result.Rule = true, n.rule
		return result, err
	}
	if rule, ok := m.matchRules(n, clean, isDir); ok {
		result.Ignored, result.Rule = !rule.Negative, rule
	}
	return result, err
}

func directoryFirst(isDir bool) int {
	if isDir {
		return 0
	}
	return 1
}

func (m *Matcher) Check(paths []Path) iter.Seq2[Match, error] {
	ordered := slices.Clone(paths)
	slices.SortStableFunc(ordered, func(a, b Path) int {
		if c := cmp.Compare(a.Name, b.Name); c != 0 {
			return c
		}
		return cmp.Compare(directoryFirst(a.IsDir), directoryFirst(b.IsDir))
	})
	return func(yield func(Match, error) bool) {
		for _, p := range ordered {
			if !yield(m.Lookup(p.Name, p.IsDir)) {
				return
			}
		}
	}
}
