package revision

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/refs"
)

const (
	minAbbrev     = 4
	maxPeelDepth  = 16
	typeAny       = object.Type(0)
	markSeparator = "@{"
)

type parser struct {
	ctx   Context
	store *store
}

func Parse(spec string, ctx Context) (Rev, error) {
	return newParser(ctx).parse(spec)
}

func newParser(ctx Context) *parser {
	return &parser{ctx: ctx, store: newStore(ctx.Objects)}
}

func (p *parser) parse(spec string) (Rev, error) {
	if spec == "" {
		return Rev{}, fmt.Errorf("%w: empty specification", ErrSyntax)
	}
	if spec[0] == ':' {
		if text, ok := strings.CutPrefix(spec, ":/"); ok && text != "" {
			return p.onelineFromRefs(text)
		}
		return Rev{}, fmt.Errorf("%w: %q names an index entry", ErrUnsupported, spec)
	}
	rev, err := p.rev(spec)
	if err == nil {
		return rev, nil
	}
	if colon := topLevelColon(spec); colon >= 0 {
		return p.pathRev(spec[:colon], spec[colon+1:])
	}
	return Rev{}, err
}

func topLevelColon(spec string) int {
	depth := 0
	for index := range len(spec) {
		switch {
		case spec[index] == '{':
			depth++
		case depth > 0 && spec[index] == '}':
			depth--
		case depth == 0 && spec[index] == ':':
			return index
		}
	}
	return -1
}

func (p *parser) rev(spec string) (Rev, error) {
	if kind, num, base, ok := numericSuffix(spec); ok {
		parsed, err := p.rev(base)
		if err != nil {
			return Rev{}, err
		}
		if kind == '^' {
			return p.nthParent(parsed.ID, num)
		}
		return p.ancestor(parsed.ID, num)
	}
	if rev, ok, err := p.peelOnion(spec); ok || err != nil {
		return rev, err
	}
	return p.basic(spec)
}

func numericSuffix(spec string) (byte, int, string, bool) {
	end := len(spec)
	for end > 0 && spec[end-1] >= '0' && spec[end-1] <= '9' {
		end--
	}
	if end == 0 || end == 1 {
		return 0, 0, "", false
	}
	kind := spec[end-1]
	if kind != '^' && kind != '~' {
		return 0, 0, "", false
	}
	digits := spec[end:]
	if digits == "" {
		return kind, 1, spec[:end-1], true
	}
	num, err := strconv.Atoi(digits)
	if err != nil {
		return 0, 0, "", false
	}
	return kind, num, spec[:end-1], true
}

func (p *parser) nthParent(id hash.ObjectID, num int) (Rev, error) {
	target, commit, err := p.peelCommitObject(id)
	if err != nil {
		return Rev{}, err
	}
	if num == 0 {
		return Rev{ID: target, Type: object.TypeCommit}, nil
	}
	if num > len(commit.Parents) {
		return Rev{}, fmt.Errorf("%w: %s has %d parents, parent %d requested",
			ErrNotFound, target, len(commit.Parents), num)
	}
	return Rev{ID: commit.Parents[num-1], Type: object.TypeCommit}, nil
}

func (p *parser) ancestor(id hash.ObjectID, num int) (Rev, error) {
	target, err := p.peel(id, object.TypeCommit)
	if err != nil {
		return Rev{}, err
	}
	for range num {
		commit, err := p.store.commit(target)
		if err != nil {
			return Rev{}, err
		}
		if len(commit.Parents) == 0 {
			return Rev{}, fmt.Errorf("%w: %s is a root commit", ErrNotFound, target)
		}
		target = commit.Parents[0]
	}
	return Rev{ID: target, Type: object.TypeCommit}, nil
}

func (p *parser) peelOnion(spec string) (Rev, bool, error) {
	if len(spec) < 4 || spec[len(spec)-1] != '}' {
		return Rev{}, false, nil
	}
	open := -1
	for index := len(spec) - 1; index > 0; index-- {
		if spec[index] == '{' && spec[index-1] == '^' {
			open = index
			break
		}
	}
	if open <= 1 {
		return Rev{}, false, nil
	}
	inner := spec[open+1 : len(spec)-1]
	base := spec[:open-1]
	if text, ok := strings.CutPrefix(inner, "/"); ok {
		rev, err := p.onelineFrom(base, text)
		return rev, true, err
	}
	want, ok := peelTarget(inner)
	if !ok {
		return Rev{}, false, nil
	}
	parsed, err := p.rev(base)
	if err != nil {
		return Rev{}, true, err
	}
	if inner == "object" {
		rev, err := p.revFor(parsed.ID)
		return rev, true, err
	}
	target, err := p.peel(parsed.ID, want)
	if err != nil {
		return Rev{}, true, err
	}
	rev, err := p.revFor(target)
	return rev, true, err
}

func peelTarget(inner string) (object.Type, bool) {
	switch inner {
	case "":
		return typeAny, true
	case "object":
		return typeAny, true
	case "commit", "tree", "blob", "tag":
		kind, err := object.ParseType(inner)
		return kind, err == nil
	default:
		return typeAny, false
	}
}

func (p *parser) peel(id hash.ObjectID, want object.Type) (hash.ObjectID, error) {
	target, _, err := p.peelTo(id, want)
	return target, err
}

func (p *parser) peelCommitObject(id hash.ObjectID) (hash.ObjectID, *object.Commit, error) {
	target, parsed, err := p.peelTo(id, object.TypeCommit)
	if err != nil {
		return hash.Zero, nil, err
	}
	commit, _ := parsed.(*object.Commit)
	return target, commit, nil
}

func (p *parser) peelTo(id hash.ObjectID, want object.Type) (hash.ObjectID, object.Object, error) {
	current := id
	for range maxPeelDepth {
		kind, parsed, err := p.store.object(current)
		if err != nil {
			return hash.Zero, nil, err
		}
		if kind == want || (want == typeAny && kind != object.TypeTag) {
			return current, parsed, nil
		}
		if tag, ok := parsed.(*object.Tag); ok {
			current = tag.Object
			continue
		}
		if commit, ok := parsed.(*object.Commit); ok && want == object.TypeTree {
			return commit.Tree, nil, nil
		}
		return hash.Zero, nil, wrongType(current, kind, want)
	}
	return hash.Zero, nil, fmt.Errorf("%w: %s nests tags too deeply", ErrNotFound, id)
}

func wrongType(id hash.ObjectID, kind, want object.Type) error {
	if want == object.TypeCommit {
		return fmt.Errorf("%w: %s is a %s", ErrNotCommit, id, kind)
	}
	return fmt.Errorf("%w: %s is a %s, not a %s", ErrNotFound, id, kind, want)
}

func (p *parser) revFor(id hash.ObjectID) (Rev, error) {
	kind, _, err := p.store.object(id)
	if err != nil {
		return Rev{}, err
	}
	return Rev{ID: id, Type: kind}, nil
}

func (p *parser) basic(spec string) (Rev, error) {
	if len(spec) == hash.HexSize {
		if id, err := hash.Parse(spec); err == nil {
			return p.revFor(id)
		}
	}
	if name, inner, ok := splitMark(spec); ok {
		return p.markRev(name, inner)
	}
	rev, ok, err := p.namedRev(spec)
	if ok || err != nil {
		return rev, err
	}
	return p.shortRev(spec)
}

func splitMark(spec string) (string, string, bool) {
	if len(spec) < 4 || spec[len(spec)-1] != '}' {
		return "", "", false
	}
	for at := len(spec) - 4; at >= 0; at-- {
		if spec[at] == '@' && spec[at+1] == '{' {
			return spec[:at], spec[at+2 : len(spec)-1], true
		}
	}
	return "", "", false
}

func (p *parser) markRev(name, inner string) (Rev, error) {
	switch {
	case strings.EqualFold(inner, "upstream"), strings.EqualFold(inner, "u"):
		return p.upstreamRev(name, false)
	case strings.EqualFold(inner, "push"):
		return p.upstreamRev(name, true)
	case strings.HasPrefix(inner, "-"):
		if name != "" {
			return Rev{}, fmt.Errorf("%w: %q must stand alone", ErrSyntax, markSeparator+inner+"}")
		}
		return p.priorRev(inner[1:])
	}
	num, err := strconv.Atoi(inner)
	if err != nil || num < 0 {
		return Rev{}, fmt.Errorf("%w: reflog dates in %q are not implemented", ErrUnsupported, inner)
	}
	return p.reflogRev(name, num)
}

func (p *parser) namedRev(spec string) (Rev, bool, error) {
	name := spec
	if name == "@" {
		name = string(refs.HEAD)
	}
	full, ref, ok, err := p.dwim(name)
	if err != nil || !ok {
		return Rev{}, false, err
	}
	rev, err := p.revFor(ref.Target)
	if err != nil {
		return Rev{}, false, err
	}
	rev.Ref = full
	return rev, true, nil
}

func (p *parser) shortRev(spec string) (Rev, error) {
	if len(spec) < minAbbrev || len(spec) >= hash.HexSize || !isHex(spec) {
		return Rev{}, fmt.Errorf("%w: %q", ErrNotFound, spec)
	}
	resolver, ok := p.ctx.Objects.(PrefixResolver)
	if !ok {
		return Rev{}, fmt.Errorf("%w: %q needs an object source that resolves prefixes", ErrNotFound, spec)
	}
	ids, err := resolver.ResolveShort(spec)
	if err != nil {
		return Rev{}, fmt.Errorf("revision: prefix %q: %w", spec, err)
	}
	switch len(ids) {
	case 0:
		return Rev{}, fmt.Errorf("%w: %q", ErrNotFound, spec)
	case 1:
		return p.revFor(ids[0])
	default:
		return Rev{}, fmt.Errorf("%w: %q matches %d objects", ErrAmbiguous, spec, len(ids))
	}
}

func isHex(text string) bool {
	for index := range len(text) {
		current := text[index]
		digit := current >= '0' && current <= '9'
		lower := current >= 'a' && current <= 'f'
		upper := current >= 'A' && current <= 'F'
		if !digit && !lower && !upper {
			return false
		}
	}
	return true
}

func (p *parser) pathRev(base, path string) (Rev, error) {
	parsed, err := p.rev(base)
	if err != nil {
		return Rev{}, err
	}
	root, err := p.peel(parsed.ID, object.TypeTree)
	if err != nil {
		return Rev{}, err
	}
	found, err := p.store.lookup(root, path)
	if err != nil {
		return Rev{}, err
	}
	if !found.found {
		return Rev{}, fmt.Errorf("%w: path %q in %q", ErrNotFound, path, base)
	}
	return Rev{ID: found.id, Type: found.mode.ObjectType()}, nil
}
