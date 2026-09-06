package remote

import (
	"context"
	"errors"
	"iter"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/revision"
)

const maxHaveCandidates = 256

type negotiator struct {
	candidates []hash.ObjectID
	common     map[hash.ObjectID]struct{}
}

func newNegotiator(ctx context.Context, store *refs.Store, db *odb.DB, shallow map[hash.ObjectID]struct{}) (*negotiator, error) {
	tips, err := localTips(store)
	if err != nil {
		return nil, err
	}
	neg := &negotiator{common: make(map[hash.ObjectID]struct{})}
	if len(tips) == 0 {
		return neg, nil
	}
	walked := revision.Walk(ctx, revision.Options{
		Context:  revision.Context{Objects: db, Shallow: shallow},
		Include:  tips,
		Order:    revision.DateOrder,
		MaxCount: maxHaveCandidates,
	})
	for commit, err := range walked {
		if err != nil {
			return nil, err
		}
		neg.candidates = append(neg.candidates, commit.ID)
	}
	return neg, nil
}

func localTips(store *refs.Store) ([]hash.ObjectID, error) {
	var tips []hash.ObjectID
	head, err := store.Resolve(refs.HEAD)
	switch {
	case err == nil:
		if !head.Target.IsZero() {
			tips = append(tips, head.Target)
		}
	case !errors.Is(err, refs.ErrNotFound):
		return nil, err
	}
	for ref, err := range store.Prefix(refs.HeadsPrefix) {
		if err != nil {
			return nil, err
		}
		if !ref.Target.IsZero() {
			tips = append(tips, ref.Target)
		}
	}
	return tips, nil
}

func (n *negotiator) Haves(ctx context.Context) iter.Seq2[hash.ObjectID, error] {
	return func(yield func(hash.ObjectID, error) bool) {
		for _, id := range n.candidates {
			if err := ctx.Err(); err != nil {
				yield(hash.Zero, err)
				return
			}
			if _, ok := n.common[id]; ok {
				continue
			}
			if !yield(id, nil) {
				return
			}
		}
	}
}

func (n *negotiator) Common(id hash.ObjectID) {
	n.common[id] = struct{}{}
}

func (n *negotiator) Enough() bool {
	return len(n.candidates) == 0 || len(n.common) > 0
}
