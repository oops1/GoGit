package refs

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/oops1/gogit/internal/gitcore/hash"
)

func (s *Store) Lookup(name Name) (Ref, error) {
	if err := name.Validate(); err != nil {
		return Ref{}, err
	}
	ref, err := s.looseRef(name)
	if err == nil || !errors.Is(err, ErrNotFound) {
		return ref, err
	}
	snapshot, err := s.loadPacked()
	if err != nil {
		return Ref{}, err
	}
	return s.packedRef(name, snapshot)
}

func (s *Store) packedRef(name Name, snapshot *packedSnapshot) (Ref, error) {
	if name.IsPerWorktree() {
		return Ref{}, fmt.Errorf("%w: %s", ErrNotFound, name)
	}
	ref, ok := snapshot.find(name)
	if !ok {
		return Ref{}, fmt.Errorf("%w: %s", ErrNotFound, name)
	}
	return ref, nil
}

func (s *Store) looseRef(name Name) (Ref, error) {
	data, err := s.treeFor(name).read(string(name))
	if err != nil {
		return Ref{}, err
	}
	return parseRefContent(name, data)
}

func parseRefContent(name Name, data []byte) (Ref, error) {
	line := data
	if index := bytes.IndexByte(line, '\n'); index >= 0 {
		line = line[:index]
	}
	line = bytes.TrimRight(line, " \t\r")
	if rest, ok := bytes.CutPrefix(line, []byte(symbolicPrefix)); ok {
		target := Name(bytes.TrimLeft(rest, " \t"))
		if err := target.Validate(); err != nil {
			return Ref{}, fmt.Errorf("%w: %s points at %s: %w", ErrMalformedRef, name, target, err)
		}
		return Ref{Name: name, SymbolicTarget: target}, nil
	}
	if len(line) < hash.HexSize {
		return Ref{}, fmt.Errorf("%w: %s holds %q", ErrMalformedRef, name, line)
	}
	id, err := hash.FromHex(line[:hash.HexSize])
	if err != nil {
		return Ref{}, fmt.Errorf("%w: %s: %w", ErrMalformedRef, name, err)
	}
	if len(line) > hash.HexSize && !isSpaceByte(line[hash.HexSize]) {
		return Ref{}, fmt.Errorf("%w: %s holds %q", ErrMalformedRef, name, line)
	}
	return Ref{Name: name, Target: id}, nil
}

func (s *Store) ResolveName(name Name) (Name, error) {
	current := name
	for range maxSymbolicDepth {
		ref, err := s.Lookup(current)
		if errors.Is(err, ErrNotFound) {
			return current, nil
		}
		if err != nil {
			return "", err
		}
		if !ref.IsSymbolic() {
			return current, nil
		}
		current = ref.SymbolicTarget
	}
	return "", fmt.Errorf("%w: %s", ErrTooManySymlinks, name)
}

func (s *Store) Resolve(name Name) (Ref, error) {
	final, err := s.ResolveName(name)
	if err != nil {
		return Ref{}, err
	}
	return s.Lookup(final)
}

func (s *Store) resolveTarget(name Name) (hash.ObjectID, error) {
	ref, err := s.Resolve(name)
	if errors.Is(err, ErrNotFound) {
		return hash.Zero, nil
	}
	if err != nil {
		return hash.Zero, err
	}
	return ref.Target, nil
}

func (s *Store) Peel(name Name) (hash.ObjectID, error) {
	ref, err := s.Resolve(name)
	if err != nil {
		return hash.Zero, err
	}
	if !ref.Peeled.IsZero() {
		return ref.Peeled, nil
	}
	if s.opts.Peeler == nil {
		return hash.Zero, fmt.Errorf("%w: %s", ErrNoPeeler, name)
	}
	target, isTag, err := s.opts.Peeler.PeelTag(ref.Target)
	if err != nil {
		return hash.Zero, err
	}
	if !isTag {
		return hash.Zero, fmt.Errorf("%w: %s", ErrNotTag, name)
	}
	return target, nil
}

func (s *Store) fillPeeled(ref Ref, known bool) (Ref, error) {
	if known || !ref.Peeled.IsZero() || s.opts.Peeler == nil || !ref.Name.IsTag() || ref.Target.IsZero() {
		return ref, nil
	}
	target, isTag, err := s.opts.Peeler.PeelTag(ref.Target)
	if err != nil {
		return Ref{}, err
	}
	if isTag {
		ref.Peeled = target
	}
	return ref, nil
}
