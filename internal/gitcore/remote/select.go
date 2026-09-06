package remote

import (
	"strings"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/refspec"
	"github.com/oops1/gogit/internal/gitcore/transport"
)

type objectStore interface {
	Get(id hash.ObjectID) (object.Type, []byte, error)
	Has(id hash.ObjectID) (bool, error)
	Peel(id hash.ObjectID) (object.Type, hash.ObjectID, error)
}

type matchedRef struct {
	ref      transport.Ref
	dst      refs.Name
	force    bool
	wildcard bool
	optional bool
}

func selectMatches(adv transport.Advertisement, specs []refspec.RefSpec, mode TagMode, db objectStore) ([]matchedRef, error) {
	seen := make(map[refs.Name]bool)
	tips := make(map[hash.ObjectID]struct{})
	var matched []matchedRef
	for _, ref := range adv.Refs {
		if ref.Name == refs.HEAD.String() {
			continue
		}
		spec, dst, ok := matchRefSpec(specs, ref.Name)
		if !ok {
			continue
		}
		name := refs.Name(dst)
		if seen[name] {
			continue
		}
		seen[name] = true
		matched = append(matched, matchedRef{ref: ref, dst: name, force: spec.Force, wildcard: spec.IsWildcard()})
		tips[ref.ID] = struct{}{}
	}
	if mode == TagsNone {
		return matched, nil
	}
	tagMatches, err := selectTags(adv, mode, db, seen, tips)
	if err != nil {
		return nil, err
	}
	return append(matched, tagMatches...), nil
}

func selectTags(adv transport.Advertisement, mode TagMode, db objectStore, seen map[refs.Name]bool, tips map[hash.ObjectID]struct{}) ([]matchedRef, error) {
	var matched []matchedRef
	for _, ref := range adv.Refs {
		name := refs.Name(ref.Name)
		if !name.IsTag() || seen[name] {
			continue
		}
		if mode == TagsAll {
			matched = append(matched, matchedRef{ref: ref, dst: name, wildcard: true})
			seen[name] = true
			continue
		}
		reached, err := tagReachesFetchedCommit(ref, db, tips)
		if err != nil {
			return nil, err
		}
		if !reached {
			continue
		}
		matched = append(matched, matchedRef{ref: ref, dst: name, wildcard: true, optional: true})
		seen[name] = true
	}
	return matched, nil
}

func tagReachesFetchedCommit(ref transport.Ref, db objectStore, tips map[hash.ObjectID]struct{}) (bool, error) {
	target := ref.Peeled
	if target.IsZero() {
		target = ref.ID
	}
	if _, ok := tips[target]; ok {
		return true, nil
	}
	return db.Has(target)
}

func matchRefSpec(specs []refspec.RefSpec, name string) (refspec.RefSpec, string, bool) {
	for _, spec := range specs {
		if dst, ok := spec.MatchSrc(name); ok {
			return spec, dst, true
		}
	}
	return refspec.RefSpec{}, "", false
}

func wildcardPrefix(pattern string) string {
	if idx := strings.IndexByte(pattern, '*'); idx >= 0 {
		return pattern[:idx]
	}
	return pattern
}

func pruneStale(store *refs.Store, specs []refspec.RefSpec, matched []matchedRef) ([]refs.Ref, error) {
	keep := make(map[refs.Name]bool, len(matched))
	for _, m := range matched {
		keep[m.dst] = true
	}
	seenSpec := make(map[string]bool)
	seenStale := make(map[refs.Name]bool)
	var stale []refs.Ref
	for _, spec := range specs {
		if !spec.IsWildcard() || seenSpec[spec.Dst] {
			continue
		}
		seenSpec[spec.Dst] = true
		prefix := wildcardPrefix(spec.Dst)
		if prefix == "" {
			continue
		}
		for ref, err := range store.Prefix(prefix) {
			if err != nil {
				return nil, err
			}
			if _, ok := spec.MatchDst(ref.Name.String()); !ok {
				continue
			}
			if keep[ref.Name] || seenStale[ref.Name] {
				continue
			}
			seenStale[ref.Name] = true
			stale = append(stale, ref)
		}
	}
	return stale, nil
}
