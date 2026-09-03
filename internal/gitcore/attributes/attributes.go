package attributes

import (
	"cmp"
	"maps"
	"strings"
)

const (
	DefaultAttributesName = ".gitattributes"
	macroPrefix           = "[attr]"
	blank                 = " \t\r\n"
	maxAttributesLine     = 2048
)

type Kind uint8

const (
	Unspecified Kind = iota
	Set
	Unset
	Valued
)

type Value struct {
	kind Kind
	text string
}

func SetValue() Value             { return Value{kind: Set} }
func UnsetValue() Value           { return Value{kind: Unset} }
func UnspecifiedValue() Value     { return Value{} }
func TextValue(text string) Value { return Value{kind: Valued, text: text} }

func (v Value) Kind() Kind          { return v.kind }
func (v Value) Text() string        { return v.text }
func (v Value) IsSet() bool         { return v.kind == Set }
func (v Value) IsUnset() bool       { return v.kind == Unset }
func (v Value) IsUnspecified() bool { return v.kind == Unspecified }

func (v Value) String() string {
	switch v.kind {
	case Set:
		return "set"
	case Unset:
		return "unset"
	case Valued:
		return v.text
	}
	return "unspecified"
}

type attrState struct {
	name  string
	value Value
}

type attrLine struct {
	pat    pattern
	macro  string
	states []attrState
	source string
	line   int
}

var builtinAttributes = parseAttributesFile("[builtin]", "", []byte("[attr]binary -diff -merge -text\n"), true)

func parseAttributesFile(source, base string, data []byte, macroOK bool) []attrLine {
	var out []attrLine
	for i, raw := range lines(data) {
		if line, ok := parseAttributesLine(raw, source, base, i+1, macroOK); ok {
			out = append(out, line)
		}
	}
	return out
}

func parseAttributesLine(raw, source, base string, lineno int, macroOK bool) (attrLine, bool) {
	if len(raw) >= maxAttributesLine {
		return attrLine{}, false
	}
	rest := strings.TrimLeft(raw, blank)
	if rest == "" || rest[0] == '#' {
		return attrLine{}, false
	}
	name, states, ok := unquoteC(rest)
	if !ok {
		n := strings.IndexAny(rest, blank)
		if n < 0 {
			n = len(rest)
		}
		name, states = rest[:n], rest[n:]
	}
	line := attrLine{source: source, line: lineno}
	if len(name) > len(macroPrefix) && strings.HasPrefix(name, macroPrefix) {
		if !macroOK {
			return attrLine{}, false
		}
		macro := strings.TrimLeft(name[len(macroPrefix):], blank)
		if n := strings.IndexAny(macro, blank); n >= 0 {
			macro = macro[:n]
		}
		if !attributeNameValid(macro) {
			return attrLine{}, false
		}
		line.macro = macro
	} else {
		line.pat = parsePattern(name, base)
		if line.pat.negative {
			return attrLine{}, false
		}
	}
	parsed, ok := parseAttributeStates(strings.TrimLeft(states, blank))
	if !ok {
		return attrLine{}, false
	}
	line.states = parsed
	return line, true
}

func parseAttributeStates(text string) ([]attrState, bool) {
	var out []attrState
	for text != "" {
		token := text
		if n := strings.IndexAny(text, blank); n >= 0 {
			token, text = text[:n], text[n:]
		} else {
			text = ""
		}
		state, ok := parseAttributeToken(token)
		if !ok {
			return nil, false
		}
		out = append(out, state)
		text = strings.TrimLeft(text, blank)
	}
	return out, true
}

func parseAttributeToken(token string) (attrState, bool) {
	name, text, hasValue := strings.Cut(token, "=")
	var value Value
	switch {
	case strings.HasPrefix(name, "-"):
		value, name = UnsetValue(), name[1:]
	case strings.HasPrefix(name, "!"):
		value, name = UnspecifiedValue(), name[1:]
	case hasValue:
		value = TextValue(text)
	default:
		value = SetValue()
	}
	if !attributeNameValid(name) {
		return attrState{}, false
	}
	return attrState{name: name, value: value}, true
}

func attributeNameValid(name string) bool {
	if name == "" || name[0] == '-' {
		return false
	}
	for i := range len(name) {
		switch c := name[i]; {
		case c == '-' || c == '.' || c == '_':
		case c >= '0' && c <= '9':
		case c >= 'a' && c <= 'z':
		case c >= 'A' && c <= 'Z':
		default:
			return false
		}
	}
	return true
}

type AttributeOptions struct {
	Work           Loader
	Global         Loader
	PerDirectory   string
	InfoFile       string
	AttributesFile string
	SystemFile     string
	IgnoreCase     bool
	AutoCRLF       string
	EOL            string
	PlatformEOL    string
}

type attrNode struct {
	parent *attrNode
	lines  []attrLine
	err    error
}

type Attributes struct {
	opts        AttributeOptions
	perDir      *fileCache[attrLine]
	globalFiles *fileCache[attrLine]
	nodes       map[string]*attrNode
	resolved    map[string]map[string]Value
	globals     [][]attrLine
	globalErr   error
	globalsRead bool
}

func New(opts AttributeOptions) *Attributes {
	if opts.PerDirectory == "" {
		opts.PerDirectory = DefaultAttributesName
	}
	return &Attributes{
		opts: opts,
		perDir: newFileCache(opts.Work, func(source string, data []byte) []attrLine {
			base := parentDir(source)
			return parseAttributesFile(source, base, data, base == "")
		}),
		globalFiles: newFileCache(opts.Global, func(source string, data []byte) []attrLine {
			return parseAttributesFile(source, "", data, true)
		}),
		nodes:    map[string]*attrNode{},
		resolved: map[string]map[string]Value{},
	}
}

func (a *Attributes) dirFile(dir string) string {
	if dir == "" {
		return a.opts.PerDirectory
	}
	return dir + "/" + a.opts.PerDirectory
}

func (a *Attributes) node(dir string) *attrNode {
	if n, ok := a.nodes[dir]; ok {
		return n
	}
	n := &attrNode{}
	a.nodes[dir] = n
	if dir != "" {
		n.parent = a.node(parentDir(dir))
		n.err = n.parent.err
	}
	lines, err := a.perDir.get(a.dirFile(dir))
	n.lines = lines
	n.err = cmp.Or(n.err, err)
	return n
}

func (a *Attributes) globalLists() ([][]attrLine, error) {
	if a.globalsRead {
		return a.globals, a.globalErr
	}
	a.globalsRead = true
	for _, name := range []string{a.opts.InfoFile, a.opts.AttributesFile, a.opts.SystemFile} {
		lines, err := a.globalFiles.get(name)
		a.globalErr = cmp.Or(a.globalErr, err)
		a.globals = append(a.globals, lines)
	}
	return a.globals, a.globalErr
}

func (a *Attributes) stack(dir string) ([][]attrLine, error) {
	node := a.node(dir)
	globals, globalErr := a.globalLists()
	lists := [][]attrLine{globals[0]}
	for cur := node; cur != nil; cur = cur.parent {
		lists = append(lists, cur.lines)
	}
	lists = append(lists, globals[1], globals[2], builtinAttributes)
	return lists, cmp.Or(node.err, globalErr)
}

func determineMacros(lists [][]attrLine) map[string][]attrState {
	macros := map[string][]attrState{}
	for _, list := range lists {
		for i := len(list) - 1; i >= 0; i-- {
			name := list[i].macro
			if name == "" {
				continue
			}
			if _, seen := macros[name]; !seen {
				macros[name] = list[i].states
			}
		}
	}
	return macros
}

func fillStates(out map[string]Value, states []attrState, macros map[string][]attrState) {
	for i := len(states) - 1; i >= 0; i-- {
		state := states[i]
		if _, known := out[state.name]; known {
			continue
		}
		out[state.name] = state.value
		if state.value.kind == Set {
			if def, ok := macros[state.name]; ok {
				fillStates(out, def, macros)
			}
		}
	}
}

func (a *Attributes) Lookup(path string) (map[string]Value, error) {
	clean, isDir := normalizePath(path)
	lists, err := a.stack(parentDir(clean))
	key := clean
	if isDir {
		key += "/"
	}
	if cached, ok := a.resolved[key]; ok {
		return cached, err
	}
	macros := determineMacros(lists)
	out := map[string]Value{}
	for _, list := range lists {
		for i := len(list) - 1; i >= 0; i-- {
			line := &list[i]
			if line.macro != "" || !line.pat.match(clean, isDir, a.opts.IgnoreCase) {
				continue
			}
			fillStates(out, line.states, macros)
		}
	}
	a.resolved[key] = out
	return out, err
}

func (a *Attributes) Get(path string, names ...string) map[string]Value {
	all, _ := a.Lookup(path)
	if len(names) == 0 {
		out := maps.Clone(all)
		maps.DeleteFunc(out, func(_ string, v Value) bool { return v.kind == Unspecified })
		return out
	}
	out := make(map[string]Value, len(names))
	for _, name := range names {
		out[name] = all[name]
	}
	return out
}

func (a *Attributes) Binary(path string) bool {
	return a.Get(path, "diff")["diff"].IsUnset()
}

func driverName(v Value) string {
	if v.kind == Valued {
		return v.text
	}
	return ""
}

func (a *Attributes) Diff(path string) string  { return driverName(a.Get(path, "diff")["diff"]) }
func (a *Attributes) Merge(path string) string { return driverName(a.Get(path, "merge")["merge"]) }
func (a *Attributes) Filter(path string) string {
	return driverName(a.Get(path, "filter")["filter"])
}
