package remote

import (
	"context"
	"slices"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/progress"
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/revision"
	"github.com/oops1/gogit/internal/gitcore/transport"
)

type pushTargets struct {
	commits []hash.ObjectID
	tags    []hash.ObjectID
	trees   []hash.ObjectID
	blobs   []hash.ObjectID
}

func classifyPushTargets(db *odb.DB, ids []hash.ObjectID) (pushTargets, error) {
	var out pushTargets
	for _, id := range ids {
		if id.IsZero() {
			continue
		}
		kind, err := db.Type(id)
		if err != nil {
			return pushTargets{}, err
		}
		switch kind {
		case object.TypeCommit:
			out.commits = append(out.commits, id)
		case object.TypeTag:
			out.tags = append(out.tags, id)
			targetKind, target, err := db.Peel(id)
			if err != nil {
				return pushTargets{}, err
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

func knownLocally(db *odb.DB, ids []hash.ObjectID) []hash.ObjectID {
	out := make([]hash.ObjectID, 0, len(ids))
	for _, id := range ids {
		if id.IsZero() {
			continue
		}
		has, err := db.Has(id)
		if err != nil || !has {
			continue
		}
		out = append(out, id)
	}
	return out
}

func gatherHaveIDs(adv transport.Advertisement, store *refs.Store, rem Remote) ([]hash.ObjectID, error) {
	var ids []hash.ObjectID
	for _, ref := range adv.Refs {
		if !ref.ID.IsZero() {
			ids = append(ids, ref.ID)
		}
		if !ref.Peeled.IsZero() {
			ids = append(ids, ref.Peeled)
		}
	}
	prefix := refs.RemotesPrefix + rem.Name + "/"
	for ref, err := range store.Prefix(prefix) {
		if err != nil {
			return nil, err
		}
		if !ref.Target.IsZero() {
			ids = append(ids, ref.Target)
		}
	}
	return ids, nil
}

type pushCollector struct {
	db      *odb.DB
	visited map[hash.ObjectID]struct{}
	ids     []hash.ObjectID
	thin    map[hash.ObjectID]struct{}
	prog    progress.Func
}

func newPushCollector(db *odb.DB, prog progress.Func) *pushCollector {
	return &pushCollector{
		db:      db,
		visited: make(map[hash.ObjectID]struct{}),
		thin:    make(map[hash.ObjectID]struct{}),
		prog:    prog,
	}
}

func (c *pushCollector) markVisited(id hash.ObjectID) bool {
	if _, ok := c.visited[id]; ok {
		return false
	}
	c.visited[id] = struct{}{}
	return true
}

func (c *pushCollector) haveTree(ctx context.Context, id hash.ObjectID) error {
	if !c.markVisited(id) {
		return nil
	}
	c.thin[id] = struct{}{}
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
			if err := c.haveTree(ctx, entry.ID); err != nil {
				return err
			}
			continue
		}
		if c.markVisited(entry.ID) {
			c.thin[entry.ID] = struct{}{}
		}
	}
	return nil
}

func (c *pushCollector) haveBlob(id hash.ObjectID) {
	if c.markVisited(id) {
		c.thin[id] = struct{}{}
	}
}

func (c *pushCollector) include(id hash.ObjectID) bool {
	if !c.markVisited(id) {
		return false
	}
	c.ids = append(c.ids, id)
	c.prog.Count(progress.PhaseCounting, int64(len(c.ids)), 0)
	return true
}

func (c *pushCollector) includeTree(ctx context.Context, id hash.ObjectID) error {
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

func collectPushObjects(ctx context.Context, db *odb.DB, haveIDs, newIDs []hash.ObjectID, prog progress.Func) ([]hash.ObjectID, map[hash.ObjectID]struct{}, error) {
	haveKinds, err := classifyPushTargets(db, knownLocally(db, haveIDs))
	if err != nil {
		return nil, nil, err
	}
	newKinds, err := classifyPushTargets(db, newIDs)
	if err != nil {
		return nil, nil, err
	}

	treeByCommit := make(map[hash.ObjectID]hash.ObjectID)
	rctx := revision.Context{Objects: db}
	for commit, err := range revision.Walk(ctx, revision.Options{Context: rctx, Include: newKinds.commits, Exclude: haveKinds.commits}) {
		if err != nil {
			return nil, nil, err
		}
		treeByCommit[commit.ID] = commit.Tree
	}

	collector := newPushCollector(db, prog)
	for _, id := range haveKinds.commits {
		commit, err := db.Commit(id)
		if err != nil {
			return nil, nil, err
		}
		if err := collector.haveTree(ctx, commit.Tree); err != nil {
			return nil, nil, err
		}
	}
	for _, id := range haveKinds.trees {
		if err := collector.haveTree(ctx, id); err != nil {
			return nil, nil, err
		}
	}
	for _, id := range haveKinds.blobs {
		collector.haveBlob(id)
	}
	for _, id := range haveKinds.tags {
		collector.markVisited(id)
	}

	orderedNew := sortedIDs(treeByCommit)
	for _, id := range orderedNew {
		collector.include(id)
		if err := collector.includeTree(ctx, treeByCommit[id]); err != nil {
			return nil, nil, err
		}
	}
	for _, id := range newKinds.tags {
		collector.include(id)
	}
	for _, id := range newKinds.trees {
		if err := collector.includeTree(ctx, id); err != nil {
			return nil, nil, err
		}
	}
	for _, id := range newKinds.blobs {
		collector.include(id)
	}

	return collector.ids, collector.thin, nil
}

func sortedIDs[V any](set map[hash.ObjectID]V) []hash.ObjectID {
	out := make([]hash.ObjectID, 0, len(set))
	for id := range set {
		out = append(out, id)
	}
	slices.SortFunc(out, func(a, b hash.ObjectID) int { return a.Compare(b) })
	return out
}
