package worktree

import (
	"errors"
	"fmt"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/refs"
)

const maxHeadHops = 5

func (w *Worktree) resolveHead() (branch string, detached bool, headCommit hash.ObjectID, err error) {
	name := refs.HEAD
	for range maxHeadHops {
		ref, err := w.refs.Lookup(name)
		switch {
		case errors.Is(err, refs.ErrNotFound):
			if name == refs.HEAD {
				return "", true, hash.Zero, nil
			}
			return name.Short(), false, hash.Zero, nil
		case err != nil:
			return "", false, hash.Zero, err
		case !ref.IsSymbolic():
			if name == refs.HEAD {
				return "", true, ref.Target, nil
			}
			return name.Short(), false, ref.Target, nil
		default:
			name = ref.SymbolicTarget
		}
	}
	return "", false, hash.Zero, fmt.Errorf("%w: HEAD", refs.ErrTooManySymlinks)
}
