package revision

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/refs"
)

func (p *parser) onelineFrom(base, pattern string) (Rev, error) {
	parsed, err := p.rev(base)
	if err != nil {
		return Rev{}, err
	}
	start, err := p.peel(parsed.ID, typeAny)
	if err != nil {
		return Rev{}, err
	}
	return p.oneline(pattern, []hash.ObjectID{start})
}

func (p *parser) onelineFromRefs(pattern string) (Rev, error) {
	starts, err := p.refCommits()
	if err != nil {
		return Rev{}, err
	}
	return p.oneline(pattern, starts)
}

func (p *parser) oneline(pattern string, starts []hash.ObjectID) (Rev, error) {
	matcher, negate, err := compileOneline(pattern)
	if err != nil {
		return Rev{}, err
	}
	searched := newGraph(p.store)
	pending := newQueue(byCommitDate)
	for _, id := range starts {
		start, err := searched.commit(id)
		if err != nil {
			return Rev{}, err
		}
		if start.flags&flagSeen != 0 {
			continue
		}
		start.flags |= flagSeen
		pending.push(start)
	}
	for pending.Len() > 0 {
		current := pending.pop()
		if matcher.MatchString(current.commit.Message) != negate {
			return Rev{ID: current.id, Type: object.TypeCommit}, nil
		}
		for _, id := range current.parents {
			parent := searched.node(id)
			if parent.flags&flagSeen != 0 {
				continue
			}
			parent.flags |= flagSeen
			if err := searched.load(parent); err != nil {
				return Rev{}, err
			}
			pending.push(parent)
		}
	}
	return Rev{}, fmt.Errorf("%w: no commit message matches %q", ErrNotFound, pattern)
}

func compileOneline(pattern string) (*regexp.Regexp, bool, error) {
	text, negate := pattern, false
	if rest, ok := strings.CutPrefix(pattern, "!"); ok {
		switch {
		case strings.HasPrefix(rest, "-"):
			text, negate = rest[1:], true
		case strings.HasPrefix(rest, "!"):
			text = rest
		default:
			return nil, false, fmt.Errorf("%w: %q starts with a single exclamation mark", ErrSyntax, pattern)
		}
	}
	compiled, err := regexp.Compile(text)
	if err != nil {
		return nil, false, fmt.Errorf("%w: %w", ErrSyntax, err)
	}
	return compiled, negate, nil
}

func (p *parser) refCommits() ([]hash.ObjectID, error) {
	var starts []hash.ObjectID
	for ref, err := range p.ctx.Refs.Prefix(refs.RefsPrefix) {
		if err != nil {
			return nil, err
		}
		id, ok, err := p.peelCommit(ref.Target)
		if err != nil {
			return nil, err
		}
		if ok {
			starts = append(starts, id)
		}
	}
	head, ok, err := p.headCommit()
	if err != nil {
		return nil, err
	}
	if ok {
		starts = append(starts, head)
	}
	return starts, nil
}

func (p *parser) headCommit() (hash.ObjectID, bool, error) {
	head, err := p.ctx.Refs.Resolve(refs.HEAD)
	if errors.Is(err, refs.ErrNotFound) {
		return hash.Zero, false, nil
	}
	if err != nil {
		return hash.Zero, false, err
	}
	return p.peelCommit(head.Target)
}

func (p *parser) peelCommit(id hash.ObjectID) (hash.ObjectID, bool, error) {
	target, err := p.peel(id, object.TypeCommit)
	if errors.Is(err, ErrNotCommit) {
		return hash.Zero, false, nil
	}
	if err != nil {
		return hash.Zero, false, err
	}
	return target, true, nil
}
