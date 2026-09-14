package local

import (
	"context"
	"errors"
	"fmt"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/transport"
)

func (s *session) Advertise(ctx context.Context) (transport.Advertisement, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.checkOpen(ctx); err != nil {
		return transport.Advertisement{}, err
	}
	head, err := s.headRef()
	if err != nil {
		return transport.Advertisement{}, err
	}
	adv := transport.Advertisement{Version: 2}
	if head.Symref != "" {
		adv.Head = head.Symref
	}
	if !head.ID.IsZero() {
		adv.Refs = append(adv.Refs, head)
	}
	for ref, err := range s.refs.All() {
		if err != nil {
			return transport.Advertisement{}, err
		}
		adv.Refs = append(adv.Refs, transport.Ref{Name: string(ref.Name), ID: ref.Target, Peeled: ref.Peeled})
	}
	for i := range adv.Refs {
		peeled, err := s.peeledTarget(adv.Refs[i])
		if err != nil {
			return transport.Advertisement{}, err
		}
		adv.Refs[i].Peeled = peeled
	}
	return adv, nil
}

func (s *session) peeledTarget(ref transport.Ref) (hash.ObjectID, error) {
	if !ref.Peeled.IsZero() || ref.ID.IsZero() {
		return ref.Peeled, nil
	}
	target, isTag, err := s.db.PeelTag(ref.ID)
	if errors.Is(err, odb.ErrNotFound) || err == nil && !isTag {
		return hash.Zero, nil
	}
	if err != nil {
		return hash.Zero, fmt.Errorf("local: peel %s: %w", ref.Name, err)
	}
	return target, nil
}

func (s *session) headRef() (transport.Ref, error) {
	ref, err := s.refs.Lookup(refs.HEAD)
	if errors.Is(err, refs.ErrNotFound) {
		return transport.Ref{}, nil
	}
	if err != nil {
		return transport.Ref{}, err
	}
	if !ref.IsSymbolic() {
		return transport.Ref{Name: string(refs.HEAD), ID: ref.Target}, nil
	}
	out := transport.Ref{Name: string(refs.HEAD), Symref: string(ref.SymbolicTarget)}
	resolved, err := s.refs.Resolve(refs.HEAD)
	if errors.Is(err, refs.ErrNotFound) {
		return out, nil
	}
	if err != nil {
		return transport.Ref{}, err
	}
	out.ID = resolved.Target
	out.Peeled = resolved.Peeled
	return out, nil
}
