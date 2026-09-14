package attributes

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/hash"
)

var ErrFilterUnsupported = errors.New("attributes: paths cleaned by a filter driver cannot be staged")

type FilterKind uint8

const (
	FilterNone FilterKind = iota
	FilterLFS
	FilterExternal
)

const lfsExtensionPrefix = "extension."

var lfsCleanCommands = []string{"git-lfs filter-process", "git-lfs clean -- %f"}

type Policy struct {
	Path     string
	Text     TextPolicy
	Ident    bool
	Encoding Value
	Filter   string
	Clean    FilterKind
	format   hash.Format
}

func (a *Attributes) Policy(path string) Policy {
	values := a.Get(path, "ident", "filter", "working-tree-encoding")
	filter := driverName(values["filter"])
	return Policy{
		Path:     path,
		Text:     a.Text(path),
		Ident:    values["ident"].IsSet(),
		Encoding: values["working-tree-encoding"],
		Filter:   filter,
		Clean:    a.cleanFilter(filter),
		format:   a.opts.ObjectFormat,
	}
}

func (a *Attributes) cleanFilter(name string) FilterKind {
	cfg := a.opts.Config
	if name == "" || cfg == nil {
		return FilterNone
	}
	command, hasProcess := cfg.Get("filter." + name + ".process")
	if !hasProcess {
		command, _ = cfg.Get("filter." + name + ".clean")
	}
	switch {
	case command == "":
		return FilterNone
	case !slices.Contains(lfsCleanCommands, command):
		return FilterExternal
	}
	for _, sub := range cfg.Subsections("lfs") {
		if strings.HasPrefix(sub, lfsExtensionPrefix) {
			return FilterExternal
		}
	}
	return FilterLFS
}

func (p Policy) filterError() error {
	return fmt.Errorf("%w: %s (filter=%s)", ErrFilterUnsupported, p.Path, p.Filter)
}

func (p Policy) ToGit(data []byte, index IndexBlob) ([]byte, error) {
	return p.toGit(data, index, true)
}

func (p Policy) CompareToGit(data []byte, index IndexBlob) []byte {
	out, _ := p.toGit(data, index, false)
	return out
}

func (p Policy) toGit(data []byte, index IndexBlob, strict bool) ([]byte, error) {
	if strict && p.Clean == FilterExternal {
		return nil, p.filterError()
	}
	passedThrough := true
	if p.Clean == FilterLFS {
		data, passedThrough = lfsClean(data)
	}
	data, err := p.encodeToGit(data, strict)
	if err != nil {
		return nil, err
	}
	data = p.Text.ToGit(data, index)
	if p.Ident {
		data = identToGit(data)
	}
	if strict && !passedThrough && !indexHolds(index, data) {
		return nil, p.filterError()
	}
	return data, nil
}

func indexHolds(index IndexBlob, data []byte) bool {
	if index == nil {
		return false
	}
	blob, ok := index()
	return ok && bytes.Equal(blob, data)
}

func (p Policy) encodeToGit(data []byte, strict bool) ([]byte, error) {
	name, err := encodingName(p.Encoding)
	switch {
	case err != nil && strict:
		return nil, fmt.Errorf("%w: %s", err, p.Path)
	case err != nil || name == "" || len(data) == 0:
		return data, nil
	}
	if err := validateEncoding(name, data); err != nil {
		if strict {
			return nil, fmt.Errorf("%w: %s as %s", err, p.Path, name)
		}
		return data, nil
	}
	decoded, ok := decodeToUTF8(name, data)
	switch {
	case ok:
		return decoded, nil
	case strict:
		return nil, fmt.Errorf("%w: %s from %s to %s", ErrEncodingFailed, p.Path, name, defaultEncoding)
	}
	return data, nil
}

func (p Policy) ToWorkingTree(data []byte) []byte {
	if p.Ident {
		data = p.identToWorkingTree(data)
	}
	data = p.Text.ToWorkingTree(data)
	name, err := encodingName(p.Encoding)
	if err != nil || name == "" || len(data) == 0 {
		return data
	}
	if encoded, ok := encodeFromUTF8(name, data); ok {
		return encoded
	}
	return data
}

func (p Policy) identToWorkingTree(data []byte) []byte {
	format := p.format
	if format == 0 {
		format = hash.SHA1
	}
	id, err := hash.Sum(format, "blob", data)
	if err != nil {
		return data
	}
	return identToWorkingTree(data, id.String())
}
