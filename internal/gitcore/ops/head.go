package ops

import (
	"errors"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/refs"
)

type headTarget struct {
	ref      refs.Name
	detached bool
	old      hash.ObjectID
}

func resolveHeadTarget(store *refs.Store) (headTarget, error) {
	ref, err := store.Lookup(refs.HEAD)
	if err != nil {
		return headTarget{}, err
	}
	if !ref.IsSymbolic() {
		return headTarget{ref: refs.HEAD, detached: true, old: ref.Target}, nil
	}
	branch := ref.SymbolicTarget
	resolved, err := store.Lookup(branch)
	if errors.Is(err, refs.ErrNotFound) {
		return headTarget{ref: branch, detached: false, old: hash.Zero}, nil
	}
	if err != nil {
		return headTarget{}, err
	}
	return headTarget{ref: branch, detached: false, old: resolved.Target}, nil
}
