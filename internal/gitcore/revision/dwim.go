package revision

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/refs"
)

const checkoutPrefix = "checkout: moving from "

type expansion struct {
	prefix string
	suffix string
}

var revParseRules = []expansion{
	{"", ""},
	{refs.RefsPrefix, ""},
	{refs.TagsPrefix, ""},
	{refs.HeadsPrefix, ""},
	{refs.RemotesPrefix, ""},
	{refs.RemotesPrefix, "/HEAD"},
}

func (p *parser) dwim(short string) (refs.Name, refs.Ref, bool, error) {
	for _, rule := range revParseRules {
		full := refs.Name(rule.prefix + short + rule.suffix)
		if full.Validate() != nil {
			continue
		}
		ref, err := p.ctx.Refs.Resolve(full)
		if errors.Is(err, refs.ErrNotFound) {
			continue
		}
		if err != nil {
			return "", refs.Ref{}, false, err
		}
		if ref.Target.IsZero() {
			continue
		}
		return full, ref, true, nil
	}
	return "", refs.Ref{}, false, nil
}

func (p *parser) reflogFor(short string) (refs.Name, []refs.ReflogEntry, error) {
	for _, rule := range revParseRules {
		full := refs.Name(rule.prefix + short + rule.suffix)
		if full.Validate() != nil {
			continue
		}
		entries, err := readReflog(p.ctx.Refs, full)
		if err != nil {
			return "", nil, err
		}
		if len(entries) > 0 {
			return full, entries, nil
		}
	}
	return "", nil, fmt.Errorf("%w: no reflog for %q", ErrNotFound, short)
}

func readReflog(source Refs, name refs.Name) ([]refs.ReflogEntry, error) {
	var entries []refs.ReflogEntry
	for entry, err := range source.Reflog(name) {
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func (p *parser) reflogRev(name string, num int) (Rev, error) {
	short := name
	if short == "" || short == "@" {
		short = string(refs.HEAD)
	}
	full, entries, err := p.reflogFor(short)
	if err != nil {
		return Rev{}, err
	}
	id, err := reflogValue(full, entries, num)
	if err != nil {
		return Rev{}, err
	}
	rev, err := p.revFor(id)
	if err != nil {
		return Rev{}, err
	}
	rev.Ref = full
	return rev, nil
}

func reflogValue(name refs.Name, entries []refs.ReflogEntry, num int) (hash.ObjectID, error) {
	if num == 0 {
		return entries[len(entries)-1].New, nil
	}
	if num <= len(entries) {
		if old := entries[len(entries)-num].Old; !old.IsZero() {
			return old, nil
		}
	}
	return hash.Zero, fmt.Errorf("%w: reflog of %s has only %d entries", ErrNotFound, name, len(entries))
}

func (p *parser) priorRev(digits string) (Rev, error) {
	text, err := p.priorName(digits)
	if err != nil {
		return Rev{}, err
	}
	if id, err := hash.Parse(text); err == nil {
		return p.revFor(id)
	}
	rev, ok, err := p.namedRev(text)
	if err != nil {
		return Rev{}, err
	}
	if !ok {
		return Rev{}, fmt.Errorf("%w: previous checkout %q", ErrNotFound, text)
	}
	return rev, nil
}

func (p *parser) priorName(digits string) (string, error) {
	num, err := strconv.Atoi(digits)
	if err != nil || num <= 0 {
		return "", fmt.Errorf("%w: %q is not a checkout distance", ErrSyntax, digits)
	}
	entries, err := readReflog(p.ctx.Refs, refs.HEAD)
	if err != nil {
		return "", err
	}
	for index := len(entries) - 1; index >= 0; index-- {
		from, ok := checkoutSource(entries[index].Message)
		if !ok {
			continue
		}
		num--
		if num == 0 {
			return from, nil
		}
	}
	return "", fmt.Errorf("%w: HEAD does not have %s previous checkouts", ErrNotFound, digits)
}

func checkoutSource(message string) (string, bool) {
	rest, ok := strings.CutPrefix(message, checkoutPrefix)
	if !ok {
		return "", false
	}
	index := strings.Index(rest, " to ")
	if index < 0 {
		return "", false
	}
	return rest[:index], true
}

func priorDigits(name string) (string, bool) {
	inner, ok := strings.CutPrefix(name, markSeparator+"-")
	if !ok || !strings.HasSuffix(inner, "}") {
		return "", false
	}
	return strings.TrimSuffix(inner, "}"), true
}

func (p *parser) upstreamRev(name string, push bool) (Rev, error) {
	branch, err := p.branchName(name)
	if err != nil {
		return Rev{}, err
	}
	target, ok := p.upstreamName(branch.Short(), push)
	if !ok {
		return Rev{}, fmt.Errorf("%w: no upstream configured for %s", ErrNotFound, branch)
	}
	full := refs.Name(target)
	if err := full.Validate(); err != nil {
		return Rev{}, fmt.Errorf("%w: upstream of %s: %w", ErrNotFound, branch, err)
	}
	ref, err := p.ctx.Refs.Resolve(full)
	if err != nil {
		return Rev{}, fmt.Errorf("%w: upstream %s: %w", ErrNotFound, full, err)
	}
	rev, err := p.revFor(ref.Target)
	if err != nil {
		return Rev{}, err
	}
	rev.Ref = full
	return rev, nil
}

func (p *parser) upstreamName(branch string, push bool) (string, bool) {
	if p.ctx.Config == nil {
		return "", false
	}
	if push {
		if remotes, ok := p.ctx.Config.(PushRemotes); ok {
			if target, ok := remotes.Push(branch); ok {
				return target, true
			}
		}
	}
	return p.ctx.Config.Upstream(branch)
}

func (p *parser) branchName(name string) (refs.Name, error) {
	if name == "" || name == "@" || name == string(refs.HEAD) {
		return p.headBranch()
	}
	short := name
	if digits, ok := priorDigits(name); ok {
		text, err := p.priorName(digits)
		if err != nil {
			return "", err
		}
		short = text
	}
	full, _, ok, err := p.dwim(short)
	if err != nil {
		return "", err
	}
	if !ok || !full.IsBranch() {
		return "", fmt.Errorf("%w: %q does not name a local branch", ErrNotFound, short)
	}
	return full, nil
}

func (p *parser) headBranch() (refs.Name, error) {
	head := p.ctx.Head
	if head == "" {
		resolved, err := p.ctx.Refs.ResolveName(refs.HEAD)
		if err != nil {
			return "", err
		}
		head = resolved
	}
	if !head.IsBranch() {
		return "", fmt.Errorf("%w: HEAD does not point to a branch", ErrNotFound)
	}
	return head, nil
}
