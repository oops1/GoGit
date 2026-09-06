package refspec

import (
	"fmt"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/refs"
)

type RefSpec struct {
	Force bool
	Src   string
	Dst   string
}

func Parse(text string) (RefSpec, error) {
	rest, force := strings.CutPrefix(text, "+")
	parts := strings.Split(rest, ":")
	if len(parts) > 2 {
		return RefSpec{}, fmt.Errorf("%w: %q has more than one colon", ErrInvalid, text)
	}
	src := parts[0]
	dst := ""
	if len(parts) == 2 {
		dst = parts[1]
	}
	r := RefSpec{Force: force, Src: src, Dst: dst}
	if err := validate(r, text); err != nil {
		return RefSpec{}, err
	}
	return r, nil
}

func ParseAll(texts []string) ([]RefSpec, error) {
	specs := make([]RefSpec, 0, len(texts))
	for _, text := range texts {
		spec, err := Parse(text)
		if err != nil {
			return nil, err
		}
		specs = append(specs, spec)
	}
	return specs, nil
}

func DefaultFetch(remote string) RefSpec {
	return RefSpec{Force: true, Src: "refs/heads/*", Dst: "refs/remotes/" + remote + "/*"}
}

func (r RefSpec) IsWildcard() bool {
	return strings.Contains(r.Src, "*") || strings.Contains(r.Dst, "*")
}

func (r RefSpec) IsDelete() bool {
	return r.Src == "" && r.Dst != ""
}

func (r RefSpec) MatchSrc(name string) (string, bool) {
	return match(r.Src, r.Dst, name)
}

func (r RefSpec) MatchDst(name string) (string, bool) {
	return match(r.Dst, r.Src, name)
}

func (r RefSpec) String() string {
	var b strings.Builder
	if r.Force {
		b.WriteByte('+')
	}
	b.WriteString(r.Src)
	if r.Dst != "" || r.Src == "" {
		b.WriteByte(':')
		b.WriteString(r.Dst)
	}
	return b.String()
}

func match(from, to, name string) (string, bool) {
	if from == "" || to == "" {
		return "", false
	}
	if strings.Contains(from, "*") {
		mid, ok := matchWildcard(from, name)
		if !ok {
			return "", false
		}
		return substituteWildcard(to, mid), true
	}
	if name != from {
		return "", false
	}
	return to, true
}

func matchWildcard(pattern, name string) (string, bool) {
	prefix, suffix := wildcardParts(pattern)
	if len(name) < len(prefix)+len(suffix) {
		return "", false
	}
	if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, suffix) {
		return "", false
	}
	return name[len(prefix) : len(name)-len(suffix)], true
}

func substituteWildcard(pattern, mid string) string {
	prefix, suffix := wildcardParts(pattern)
	return prefix + mid + suffix
}

func wildcardParts(pattern string) (string, string) {
	star := strings.IndexByte(pattern, '*')
	return pattern[:star], pattern[star+1:]
}

func validate(r RefSpec, text string) error {
	switch {
	case r.Src == "" && r.Dst == "":
		return fmt.Errorf("%w: %q has neither a source nor a destination", ErrInvalid, text)
	case r.Src == "" && r.Dst != "":
		if strings.Contains(r.Dst, "*") {
			return fmt.Errorf("%w: %q deletes a wildcard destination", ErrInvalid, text)
		}
		if err := refs.CheckFormat(r.Dst, refs.AllowOneLevel); err != nil {
			return fmt.Errorf("%w: %q: %w", ErrInvalid, text, err)
		}
		return nil
	case r.Dst == "":
		return checkSide(r.Src, text)
	default:
		srcWild, dstWild := strings.Contains(r.Src, "*"), strings.Contains(r.Dst, "*")
		if srcWild != dstWild {
			return fmt.Errorf("%w: %q has a wildcard on only one side", ErrInvalid, text)
		}
		if err := checkSide(r.Src, text); err != nil {
			return err
		}
		return checkSide(r.Dst, text)
	}
}

func checkSide(name, text string) error {
	if err := refs.CheckFormat(name, refs.AllowOneLevel|refs.RefspecPattern); err != nil {
		return fmt.Errorf("%w: %q: %w", ErrInvalid, text, err)
	}
	return nil
}
