package revision

import (
	"fmt"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/refs"
)

type ranger struct {
	parser *parser
	opts   Options
	negate bool
}

func Ranges(specs []string, ctx Context) (Options, error) {
	collector := &ranger{parser: newParser(ctx), opts: Options{Context: ctx}}
	for index, spec := range specs {
		if spec == "--" {
			collector.opts.Paths = append(collector.opts.Paths, specs[index+1:]...)
			break
		}
		if err := collector.arg(spec); err != nil {
			return Options{}, err
		}
	}
	return collector.opts, nil
}

func (r *ranger) arg(spec string) error {
	if strings.HasPrefix(spec, "--") {
		return r.option(spec)
	}
	if rest, ok := strings.CutPrefix(spec, "^"); ok {
		return r.single(rest, !r.negate)
	}
	if left, right, ok := strings.Cut(spec, "..."); ok {
		return r.symmetric(left, right)
	}
	if left, right, ok := strings.Cut(spec, ".."); ok {
		return r.between(left, right)
	}
	return r.single(spec, r.negate)
}

func (r *ranger) add(id hash.ObjectID, exclude bool) {
	if exclude {
		r.opts.Exclude = append(r.opts.Exclude, id)
		return
	}
	r.opts.Include = append(r.opts.Include, id)
}

func (r *ranger) commitID(spec string) (hash.ObjectID, error) {
	if spec == "" {
		spec = string(refs.HEAD)
	}
	parsed, err := r.parser.parse(spec)
	if err != nil {
		return hash.Zero, err
	}
	return r.parser.peel(parsed.ID, object.TypeCommit)
}

func (r *ranger) single(spec string, exclude bool) error {
	id, err := r.commitID(spec)
	if err != nil {
		return err
	}
	r.add(id, exclude)
	return nil
}

func (r *ranger) between(left, right string) error {
	from, err := r.commitID(left)
	if err != nil {
		return err
	}
	to, err := r.commitID(right)
	if err != nil {
		return err
	}
	r.add(from, !r.negate)
	r.add(to, r.negate)
	return nil
}

func (r *ranger) symmetric(left, right string) error {
	from, err := r.commitID(left)
	if err != nil {
		return err
	}
	to, err := r.commitID(right)
	if err != nil {
		return err
	}
	bases, err := MergeBase(r.parser.ctx, from, to)
	if err != nil {
		return err
	}
	r.add(from, r.negate)
	r.add(to, r.negate)
	for _, base := range bases {
		r.add(base, !r.negate)
	}
	return nil
}

func (r *ranger) option(spec string) error {
	if spec == "--not" {
		r.negate = !r.negate
		return nil
	}
	if spec == "--all" {
		return r.all()
	}
	name, value, hasValue := strings.Cut(spec, "=")
	switch name {
	case "--branches":
		return r.glob(refs.HeadsPrefix, value, hasValue)
	case "--tags":
		return r.glob(refs.TagsPrefix, value, hasValue)
	case "--remotes":
		return r.glob(refs.RemotesPrefix, value, hasValue)
	case "--glob":
		if !hasValue {
			return fmt.Errorf("%w: %q needs a pattern", ErrSyntax, spec)
		}
		return r.glob(refs.RefsPrefix, value, true)
	}
	return fmt.Errorf("%w: unknown option %q", ErrSyntax, spec)
}

func (r *ranger) all() error {
	if err := r.matching(refs.RefsPrefix, ""); err != nil {
		return err
	}
	head, ok, err := r.parser.headCommit()
	if err != nil || !ok {
		return err
	}
	r.add(head, r.negate)
	return nil
}

func (r *ranger) glob(prefix, value string, hasValue bool) error {
	if !hasValue {
		return r.matching(prefix, "")
	}
	pattern := value
	if strings.HasPrefix(pattern, refs.RefsPrefix) {
		prefix = refs.RefsPrefix
		pattern = strings.TrimPrefix(pattern, refs.RefsPrefix)
	}
	if !strings.ContainsAny(pattern, "*?[") {
		pattern += "/*"
	}
	return r.matching(prefix, pattern)
}

func (r *ranger) matching(prefix, pattern string) error {
	for ref, err := range r.parser.ctx.Refs.Prefix(prefix) {
		if err != nil {
			return err
		}
		short := strings.TrimPrefix(string(ref.Name), prefix)
		if pattern != "" && !globMatch(pattern, short) {
			continue
		}
		id, ok, err := r.parser.peelCommit(ref.Target)
		if err != nil {
			return err
		}
		if ok {
			r.add(id, r.negate)
		}
	}
	return nil
}

func globMatch(pattern, text string) bool {
	switch {
	case pattern == "":
		return text == ""
	case pattern[0] == '*':
		return globStar(pattern, text)
	case text == "":
		return false
	case pattern[0] == '?':
		return text[0] != '/' && globMatch(pattern[1:], text[1:])
	default:
		return pattern[0] == text[0] && globMatch(pattern[1:], text[1:])
	}
}

func globStar(pattern, text string) bool {
	crossing := strings.HasPrefix(pattern, "**")
	rest := strings.TrimLeft(pattern, "*")
	for index := 0; index <= len(text); index++ {
		if globMatch(rest, text[index:]) {
			return true
		}
		if !crossing && index < len(text) && text[index] == '/' {
			return false
		}
	}
	return false
}
