package local

import (
	"context"
	"fmt"
	"io"
	"slices"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/pack"
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/transport"
)

func (s *session) Fetch(ctx context.Context, req transport.FetchRequest, neg transport.Negotiator) (*transport.FetchResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.checkOpen(ctx); err != nil {
		return nil, err
	}

	wantedRefs, err := s.resolveWantRefs(ctx, req.WantRefs)
	if err != nil {
		return nil, err
	}
	wants := make([]hash.ObjectID, 0, len(req.Wants)+len(wantedRefs))
	wants = append(wants, req.Wants...)
	for _, ref := range wantedRefs {
		wants = append(wants, ref.ID)
	}
	if len(wants) == 0 {
		return nil, fmt.Errorf("%w: fetch request has no wants", transport.ErrProtocol)
	}

	haves, commonList, err := s.collectHaves(ctx, neg)
	if err != nil {
		return nil, err
	}
	haveCommits, err := commitOnly(ctx, s.db, haves)
	if err != nil {
		return nil, err
	}

	kinds, err := splitWants(ctx, s.db, wants)
	if err != nil {
		return nil, err
	}

	included, shallow, err := s.commitClosure(ctx, kinds.commits, haveCommits, req.Depth)
	if err != nil {
		return nil, err
	}

	collector := newObjectCollector(s.db)
	for _, id := range haveCommits {
		commit, err := s.db.Commit(id)
		if err != nil {
			return nil, err
		}
		if err := collector.excludeTree(ctx, commit.Tree); err != nil {
			return nil, err
		}
	}

	commitIDs := make([]hash.ObjectID, 0, len(included))
	for id := range included {
		commitIDs = append(commitIDs, id)
	}
	slices.SortFunc(commitIDs, func(a, b hash.ObjectID) int { return a.Compare(b) })
	for _, id := range commitIDs {
		collector.include(id)
		if err := collector.includeTree(ctx, included[id].Tree); err != nil {
			return nil, err
		}
	}
	for _, id := range kinds.tags {
		collector.include(id)
	}
	for _, id := range kinds.trees {
		if err := collector.includeTree(ctx, id); err != nil {
			return nil, err
		}
	}
	for _, id := range kinds.blobs {
		collector.include(id)
	}

	pr, pw := io.Pipe()
	ids := collector.ids
	progress := req.Progress
	go func() {
		_, writeErr := pack.WritePack(ctx, pw, s.db, ids, pack.WriteOptions{Progress: progress})
		_ = pw.CloseWithError(writeErr)
	}()

	return &transport.FetchResponse{
		Common:     commonList,
		Shallow:    shallow,
		WantedRefs: wantedRefs,
		Pack:       pr,
	}, nil
}

func (s *session) resolveWantRefs(ctx context.Context, names []string) ([]transport.Ref, error) {
	if len(names) == 0 {
		return nil, nil
	}
	out := make([]transport.Ref, 0, len(names))
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		ref, err := s.refs.Resolve(refs.Name(name))
		if err != nil {
			return nil, fmt.Errorf("%w: want-ref %s: %w", transport.ErrProtocol, name, err)
		}
		out = append(out, transport.Ref{Name: name, ID: ref.Target})
	}
	return out, nil
}

func (s *session) collectHaves(ctx context.Context, neg transport.Negotiator) ([]hash.ObjectID, []hash.ObjectID, error) {
	if neg == nil {
		return nil, nil, nil
	}
	var haves, common []hash.ObjectID
	for id, err := range neg.Haves(ctx) {
		if err != nil {
			return nil, nil, err
		}
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		haves = append(haves, id)
		known, err := s.db.Has(id)
		if err != nil {
			return nil, nil, err
		}
		if known {
			neg.Common(id)
			common = append(common, id)
		}
	}
	return haves, common, nil
}
