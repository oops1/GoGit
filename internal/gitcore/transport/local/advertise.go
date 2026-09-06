package local

import (
	"context"
	"errors"

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
	return adv, nil
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
