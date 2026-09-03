package refs

import (
	"errors"
	"iter"
	"slices"
	"strings"
)

type listing struct {
	names  []Name
	loose  map[Name]bool
	packed *packedSnapshot
}

func (s *Store) All() iter.Seq2[Ref, error] { return s.Prefix(RefsPrefix) }

func (s *Store) Prefix(prefix string) iter.Seq2[Ref, error] {
	return func(yield func(Ref, error) bool) {
		list, err := s.listNames(prefix)
		if err != nil {
			yield(Ref{}, err)
			return
		}
		for _, name := range list.names {
			ref, ok, err := s.listedRef(name, list)
			if err != nil {
				yield(Ref{}, err)
				return
			}
			if !ok {
				continue
			}
			if !yield(ref, nil) {
				return
			}
		}
	}
}

func (s *Store) listNames(prefix string) (*listing, error) {
	snapshot, err := s.loadPacked()
	if err != nil {
		return nil, err
	}
	list := &listing{loose: make(map[Name]bool), packed: snapshot}
	collect := func(from tree, perWorktree bool) error {
		return from.walkLoose(listRoot(prefix), func(name Name) {
			if !strings.HasPrefix(string(name), prefix) {
				return
			}
			if s.split() && name.IsPerWorktree() != perWorktree {
				return
			}
			list.loose[name] = true
			list.names = append(list.names, name)
		})
	}
	if err := collect(s.common, false); err != nil {
		return nil, err
	}
	if s.split() {
		if err := collect(s.git, true); err != nil {
			return nil, err
		}
	}
	for _, ref := range snapshot.refs {
		if strings.HasPrefix(string(ref.Name), prefix) && !list.loose[ref.Name] {
			list.names = append(list.names, ref.Name)
		}
	}
	slices.Sort(list.names)
	return list, nil
}

func listRoot(prefix string) string {
	root := strings.TrimSuffix(RefsPrefix, "/")
	if !strings.HasPrefix(prefix, RefsPrefix) {
		return root
	}
	if index := strings.LastIndexByte(prefix, '/'); index > len(root) {
		return prefix[:index]
	}
	return root
}

func (s *Store) listedRef(name Name, list *listing) (Ref, bool, error) {
	ref := Ref{}
	known := false
	err := error(nil)
	if list.loose[name] {
		ref, err = s.looseRef(name)
	} else {
		err = ErrNotFound
	}
	if errors.Is(err, ErrNotFound) {
		ref, err = s.packedRef(name, list.packed)
		known = list.packed.fullyPeeled
		if errors.Is(err, ErrNotFound) {
			return Ref{}, false, nil
		}
	}
	if err != nil {
		return Ref{}, false, err
	}
	if ref.IsSymbolic() {
		target, err := s.resolveTarget(ref.SymbolicTarget)
		if err != nil {
			return Ref{}, false, err
		}
		ref.Target = target
		known = false
	}
	ref, err = s.fillPeeled(ref, known)
	if err != nil {
		return Ref{}, false, err
	}
	return ref, true, nil
}
