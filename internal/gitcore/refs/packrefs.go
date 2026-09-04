package refs

import (
	"errors"
	"slices"
	"strings"
)

func (s *Store) PackRefs(prune bool) error {
	snapshot, err := s.loadPacked()
	if err != nil {
		return err
	}
	packed := slices.Clone(snapshot.refs)
	position := make(map[Name]int, len(packed))
	for index, ref := range packed {
		position[ref.Name] = index
	}
	for index, ref := range packed {
		peeled, err := s.fillPeeled(ref, snapshot.fullyPeeled)
		if err != nil {
			return err
		}
		packed[index] = peeled
	}
	loose, err := s.looseForPacking()
	if err != nil {
		return err
	}
	for _, ref := range loose {
		if index, ok := position[ref.Name]; ok {
			packed[index] = ref
			continue
		}
		position[ref.Name] = len(packed)
		packed = append(packed, ref)
	}
	slices.SortFunc(packed, func(a, b Ref) int {
		return strings.Compare(string(a.Name), string(b.Name))
	})
	if err := s.writePacked(packed, s.opts.Peeler != nil); err != nil {
		return err
	}
	if !prune {
		return nil
	}
	for _, ref := range loose {
		if err := s.pruneLoose(ref); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) looseForPacking() ([]Ref, error) {
	var names []Name
	err := s.common.walkLoose(strings.TrimSuffix(RefsPrefix, "/"), func(name Name) {
		if !name.IsPerWorktree() {
			names = append(names, name)
		}
	})
	if err != nil {
		return nil, err
	}
	slices.Sort(names)
	refs := make([]Ref, 0, len(names))
	for _, name := range names {
		ref, err := s.looseRef(name)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if ref.IsSymbolic() {
			continue
		}
		ref, err = s.fillPeeled(ref, false)
		if err != nil {
			return nil, err
		}
		refs = append(refs, ref)
	}
	return refs, nil
}

func (s *Store) pruneLoose(ref Ref) error {
	lock, err := newLock(s.common, string(ref.Name))
	if errors.Is(err, ErrLocked) {
		return nil
	}
	if err != nil {
		return err
	}
	defer lock.release()
	current, err := s.looseRef(ref.Name)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if current.Target != ref.Target {
		return nil
	}
	if err := s.common.remove(string(ref.Name)); err != nil {
		return err
	}
	lock.release()
	s.common.removeEmptyDirs(string(ref.Name), keepRefDirs)
	return nil
}
