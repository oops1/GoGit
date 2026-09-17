package userdiff

import (
	"bytes"
	"errors"
	"fmt"
	"iter"
	"strings"
	"sync"

	"github.com/oops1/gogit/internal/gitcore/config"
	"github.com/oops1/gogit/internal/gitcore/posixre"
)

const (
	DefaultDriver = "default"
	HeaderLimit   = 80

	configSection = "diff"
	keyFuncName   = "funcname"
	keyXFuncName  = "xfuncname"
	keyWordRegex  = "wordregex"
)

var ErrLastNegated = errors.New("userdiff: the last expression of a function name pattern must not be negated")

type Pattern struct {
	Source string
	Flags  posixre.Flags
}

type Driver struct {
	Name      string
	FuncName  Pattern
	WordRegex string
}

func Builtin(name string) (Driver, bool) {
	for _, driver := range builtinDrivers {
		if driver.Name == name {
			return driver, true
		}
	}
	return Driver{}, false
}

func Builtins() []Driver {
	return append([]Driver(nil), builtinDrivers...)
}

type Entries interface {
	All() iter.Seq[config.Entry]
}

type Drivers struct {
	custom   []Driver
	builtin  []Driver
	mu       sync.Mutex
	matchers map[string]compiled
}

type compiled struct {
	matcher *Matcher
	err     error
}

func Load(cfg Entries) *Drivers {
	d := &Drivers{builtin: Builtins(), matchers: map[string]compiled{}}
	if cfg == nil {
		return d
	}
	for entry := range cfg.All() {
		if entry.Section != configSection || !entry.HasSubsection || !entry.HasValue {
			continue
		}
		d.apply(entry)
	}
	return d
}

func (d *Drivers) apply(entry config.Entry) {
	driver := d.find(entry.Subsection)
	if driver == nil {
		d.custom = append(d.custom, Driver{Name: entry.Subsection})
		driver = &d.custom[len(d.custom)-1]
	}
	switch entry.Key {
	case keyFuncName:
		driver.FuncName = Pattern{Source: entry.Value}
	case keyXFuncName:
		driver.FuncName = Pattern{Source: entry.Value, Flags: posixre.Extended}
	case keyWordRegex:
		driver.WordRegex = entry.Value
	}
}

func (d *Drivers) find(name string) *Driver {
	for at := range d.custom {
		if d.custom[at].Name == name {
			return &d.custom[at]
		}
	}
	for at := range d.builtin {
		if d.builtin[at].Name == name {
			return &d.builtin[at]
		}
	}
	return nil
}

func (d *Drivers) Driver(name string) (Driver, bool) {
	driver := d.find(name)
	if driver == nil {
		return Driver{}, false
	}
	return *driver, true
}

func (d *Drivers) Matcher(name string) (*Matcher, error) {
	driver := d.find(name)
	if driver == nil || driver.FuncName.Source == "" {
		return nil, nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if known, ok := d.matchers[name]; ok {
		return known.matcher, known.err
	}
	matcher, err := Compile(driver.FuncName)
	d.matchers[name] = compiled{matcher: matcher, err: err}
	return matcher, err
}

type rule struct {
	re     *posixre.Regexp
	negate bool
}

type Matcher struct {
	rules []rule
}

func Compile(p Pattern) (*Matcher, error) {
	lines := strings.Split(p.Source, "\n")
	m := &Matcher{rules: make([]rule, 0, len(lines))}
	for at, line := range lines {
		expression, negate := strings.CutPrefix(line, "!")
		if negate && at == len(lines)-1 {
			return nil, fmt.Errorf("%w: %q", ErrLastNegated, line)
		}
		re, err := posixre.Compile(expression, p.Flags)
		if err != nil {
			return nil, err
		}
		m.rules = append(m.rules, rule{re: re, negate: negate})
	}
	return m, nil
}

func (m *Matcher) Match(line []byte) ([]byte, bool) {
	if m == nil {
		return defaultMatch(line)
	}
	line = withoutLineEnd(line)
	for _, r := range m.rules {
		loc := r.re.FindSubmatchIndex(line)
		if loc == nil {
			continue
		}
		if r.negate {
			return nil, false
		}
		if len(loc) >= 4 && loc[2] >= 0 {
			return line[loc[2]:loc[3]], true
		}
		return line[loc[0]:loc[1]], true
	}
	return nil, false
}

func (m *Matcher) Header(line []byte, limit int) (string, bool) {
	text, ok := m.Match(line)
	if !ok {
		return "", false
	}
	if len(text) > limit {
		text = text[:limit]
	}
	return string(bytes.TrimRight(text, " \t\n\r")), true
}

func withoutLineEnd(line []byte) []byte {
	if body, found := bytes.CutSuffix(line, []byte("\n")); found {
		return bytes.TrimSuffix(body, []byte("\r"))
	}
	return line
}

func defaultMatch(line []byte) ([]byte, bool) {
	if len(line) == 0 {
		return nil, false
	}
	switch c := line[0]; {
	case 'a' <= c && c <= 'z', 'A' <= c && c <= 'Z', c == '_', c == '$':
		return line, true
	}
	return nil, false
}
