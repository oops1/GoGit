package local

import (
	"context"
	"errors"
	"slices"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/revision"
)

type wantKinds struct {
	commits []hash.ObjectID
	tags    []hash.ObjectID
	trees   []hash.ObjectID
	blobs   []hash.ObjectID
}

func splitWants(ctx context.Context, db *odb.DB, wants []hash.ObjectID) (wantKinds, error) {
	var out wantKinds
	for _, id := range wants {
		if err := ctx.Err(); err != nil {
			return wantKinds{}, err
		}
		kind, err := db.Type(id)
		if err != nil {
			return wantKinds{}, err
		}
		switch kind {
		case object.TypeCommit:
			out.commits = append(out.commits, id)
		case object.TypeTag:
			out.tags = append(out.tags, id)
			targetKind, target, err := db.Peel(id)
			if err != nil {
				return wantKinds{}, err
			}
			switch targetKind {
			case object.TypeCommit:
				out.commits = append(out.commits, target)
			case object.TypeTree:
				out.trees = append(out.trees, target)
			case object.TypeBlob:
				out.blobs = append(out.blobs, target)
			}
		case object.TypeTree:
			out.trees = append(out.trees, id)
		case object.TypeBlob:
			out.blobs = append(out.blobs, id)
		}
	}
	return out, nil
}

func commitOnly(ctx context.Context, db *odb.DB, ids []hash.ObjectID) ([]hash.ObjectID, error) {
	out := make([]hash.ObjectID, 0, len(ids))
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		kind, err := db.Type(id)
		if errors.Is(err, odb.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if kind != object.TypeCommit {
			continue
		}
		out = append(out, id)
	}
	return out, nil
}

type objectCollector struct {
	db      *odb.DB
	visited map[hash.ObjectID]struct{}
	ids     []hash.ObjectID
}

func newObjectCollector(db *odb.DB) *objectCollector {
	return &objectCollector{db: db, visited: make(map[hash.ObjectID]struct{})}
}

func (c *objectCollector) exclude(id hash.ObjectID) bool {
	if _, ok := c.visited[id]; ok {
		return false
	}
	c.visited[id] = struct{}{}
	return true
}

func (c *objectCollector) include(id hash.ObjectID) bool {
	if !c.exclude(id) {
		return false
	}
	c.ids = append(c.ids, id)
	return true
}

func (c *objectCollector) excludeTree(ctx context.Context, id hash.ObjectID) error {
	if !c.exclude(id) {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	tree, err := c.db.Tree(id)
	if err != nil {
		return err
	}
	for _, entry := range tree.Entries {
		if entry.Mode.IsSubmodule() {
			continue
		}
		if entry.Mode.IsTree() {
			if err := c.excludeTree(ctx, entry.ID); err != nil {
				return err
			}
			continue
		}
		c.exclude(entry.ID)
	}
	return nil
}

func (c *objectCollector) includeTree(ctx context.Context, id hash.ObjectID) error {
	if !c.include(id) {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	tree, err := c.db.Tree(id)
	if err != nil {
		return err
	}
	for _, entry := range tree.Entries {
		if entry.Mode.IsSubmodule() {
			continue
		}
		if entry.Mode.IsTree() {
			if err := c.includeTree(ctx, entry.ID); err != nil {
				return err
			}
			continue
		}
		c.include(entry.ID)
	}
	return nil
}

func (s *session) commitClosure(ctx context.Context, wants, haves []hash.ObjectID, depth int) (map[hash.ObjectID]*object.Commit, []hash.ObjectID, error) {
	if depth > 0 {
		return s.shallowClosure(ctx, wants, depth)
	}
	included := make(map[hash.ObjectID]*object.Commit)
	rctx := revision.Context{Objects: s.db}
	for commit, err := range revision.Walk(ctx, revision.Options{Context: rctx, Include: wants, Exclude: haves}) {
		if err != nil {
			return nil, nil, err
		}
		included[commit.ID] = commit.Commit
	}
	return included, nil, nil
}

func (s *session) shallowClosure(ctx context.Context, wants []hash.ObjectID, depth int) (map[hash.ObjectID]*object.Commit, []hash.ObjectID, error) {
	included := make(map[hash.ObjectID]*object.Commit)
	frontier := slices.Clone(wants)
	for level := 1; len(frontier) > 0; level++ {
		next := make([]hash.ObjectID, 0, len(frontier))
		for _, id := range frontier {
			if err := ctx.Err(); err != nil {
				return nil, nil, err
			}
			if _, ok := included[id]; ok {
				continue
			}
			commit, err := s.db.Commit(id)
			if err != nil {
				return nil, nil, err
			}
			included[id] = commit
			if level < depth {
				next = append(next, commit.Parents...)
			}
		}
		frontier = next
	}
	var shallow []hash.ObjectID
	for id, commit := range included {
		for _, parent := range commit.Parents {
			if _, ok := included[parent]; !ok {
				shallow = append(shallow, id)
				break
			}
		}
	}
	slices.SortFunc(shallow, func(a, b hash.ObjectID) int { return a.Compare(b) })
	return included, shallow, nil
}
