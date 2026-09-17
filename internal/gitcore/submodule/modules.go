package submodule

import (
	"fmt"
	"slices"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/config"
)

type UpdateType int

const (
	UpdateUnspecified UpdateType = iota
	UpdateCheckout
	UpdateRebase
	UpdateMerge
	UpdateNone
	UpdateCommand
)

type UpdateStrategy struct {
	Type    UpdateType
	Command string
}

type Ignore string

const (
	IgnoreUnset     Ignore = ""
	IgnoreNone      Ignore = "none"
	IgnoreUntracked Ignore = "untracked"
	IgnoreDirty     Ignore = "dirty"
	IgnoreAll       Ignore = "all"
)

type Module struct {
	Name       string
	Path       string
	URL        string
	Branch     string
	Update     UpdateStrategy
	Ignore     Ignore
	Shallow    bool
	ShallowSet bool
}

type Modules struct {
	order  []string
	byName map[string]*Module
	byPath map[string]string
}

func newModules() *Modules {
	return &Modules{byName: map[string]*Module{}, byPath: map[string]string{}}
}

func (m *Modules) ByPath(path string) (Module, bool) {
	name, ok := m.byPath[path]
	if !ok {
		return Module{}, false
	}
	return *m.byName[name], true
}

func (m *Modules) ByName(name string) (Module, bool) {
	module, ok := m.byName[name]
	if !ok {
		return Module{}, false
	}
	return *module, true
}

func (m *Modules) All() []Module {
	out := make([]Module, 0, len(m.order))
	for _, name := range m.order {
		out = append(out, *m.byName[name])
	}
	return out
}

func (m *Modules) Len() int {
	return len(m.order)
}

func ParseUpdateType(value string) UpdateType {
	switch {
	case value == "none":
		return UpdateNone
	case value == "checkout":
		return UpdateCheckout
	case value == "rebase":
		return UpdateRebase
	case value == "merge":
		return UpdateMerge
	case strings.HasPrefix(value, "!"):
		return UpdateCommand
	}
	return UpdateUnspecified
}

func ParseUpdateStrategy(value string) (UpdateStrategy, error) {
	kind := ParseUpdateType(value)
	switch kind {
	case UpdateUnspecified:
		return UpdateStrategy{}, fmt.Errorf("%w: %q", ErrInvalidUpdate, value)
	case UpdateCommand:
		return UpdateStrategy{Type: kind, Command: value[1:]}, nil
	}
	return UpdateStrategy{Type: kind}, nil
}

func (t UpdateType) String() string {
	switch t {
	case UpdateCheckout:
		return "checkout"
	case UpdateRebase:
		return "rebase"
	case UpdateMerge:
		return "merge"
	case UpdateNone:
		return "none"
	case UpdateCommand:
		return "command"
	}
	return ""
}

func ParseIgnore(value string) (Ignore, error) {
	switch ignore := Ignore(value); ignore {
	case IgnoreNone, IgnoreUntracked, IgnoreDirty, IgnoreAll:
		return ignore, nil
	}
	return IgnoreUnset, fmt.Errorf("%w: %q", ErrInvalidIgnore, value)
}

func Parse(data []byte) (*Modules, error) {
	variables, err := config.ParseVariables(data)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidGitmodules, err)
	}
	modules := newModules()
	for _, v := range variables {
		if err := modules.apply(v); err != nil {
			return nil, err
		}
	}
	return modules, nil
}

func (m *Modules) module(name string) *Module {
	if module, ok := m.byName[name]; ok {
		return module
	}
	module := &Module{Name: name}
	m.byName[name] = module
	m.order = append(m.order, name)
	return module
}

func looksLikeOption(value string) bool {
	return strings.HasPrefix(value, "-")
}

var valuedKeys = []string{"path", "ignore", "url", "update", "branch"}

func (m *Modules) apply(v config.Variable) error {
	if v.Section != "submodule" || !v.HasSubsection || !NameAllowed(v.Subsection) {
		return nil
	}
	key := strings.ToLower(v.Key)
	if !v.HasValue && slices.Contains(valuedKeys, key) {
		return fmt.Errorf("%w: submodule.%s.%s has no value", ErrInvalidGitmodules, v.Subsection, key)
	}
	module := m.module(v.Subsection)
	switch key {
	case "path":
		m.setPath(module, v.Value)
	case "ignore":
		if ignore, err := ParseIgnore(v.Value); err == nil {
			module.Ignore = ignore
		}
	case "url":
		if !looksLikeOption(v.Value) {
			module.URL = v.Value
		}
	case "update":
		strategy, err := ParseUpdateStrategy(v.Value)
		if err != nil || strategy.Type == UpdateCommand {
			return fmt.Errorf("%w: submodule.%s.update = %q", ErrInvalidGitmodules, v.Subsection, v.Value)
		}
		module.Update = strategy
	case "shallow":
		shallow := true
		if v.HasValue {
			parsed, err := config.ParseBool(v.Value)
			if err != nil {
				return fmt.Errorf("%w: submodule.%s.shallow: %w", ErrInvalidGitmodules, v.Subsection, err)
			}
			shallow = parsed
		}
		module.Shallow, module.ShallowSet = shallow, true
	case "branch":
		module.Branch = v.Value
	}
	return nil
}

func (m *Modules) setPath(module *Module, path string) {
	if looksLikeOption(path) {
		return
	}
	if module.Path != "" {
		delete(m.byPath, module.Path)
	}
	module.Path = path
	m.byPath[path] = module.Name
}

func NameAllowed(name string) bool {
	separator := func(c rune) bool { return c == '/' || c == '\\' }
	return name != "" && !slices.Contains(strings.FieldsFunc(name, separator), "..")
}
