package ops

import (
	"context"
	"maps"
	"slices"

	"github.com/oops1/gogit/internal/gitcore/index"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/repo"
)

func Unstage(ctx context.Context, r *repo.Repository, paths []string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	wt, err := openWorkingTree(r)
	if err != nil {
		return err
	}
	defer func() { _ = wt.close() }()

	db, err := odbOpen(r.ObjectsDir(), odb.Options{Format: r.ObjectFormat})
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	store, err := refsOpen(refs.Options{GitDir: r.GitDir(), CommonDir: r.CommonDir(), Bare: r.IsBare(), Peeler: db})
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()

	headCommit, err := resolveHeadCommit(store)
	if err != nil {
		return err
	}
	headTree, err := commitTreeEntries(db, headCommit)
	if err != nil {
		return err
	}

	lock, err := lockIndex(r)
	if err != nil {
		return err
	}
	sw := &switcher{ctx: ctx, wt: wt, db: db, format: db.Format()}
	for _, p := range paths {
		if err := ctx.Err(); err != nil {
			lock.abort()
			return err
		}
		clean, err := cleanRepoPath(p)
		if err != nil {
			lock.abort()
			return err
		}
		if err := unstagePath(sw, lock.idx, headTree, clean); err != nil {
			lock.abort()
			return err
		}
	}
	return lock.commit()
}

func unstagePath(sw *switcher, idx *index.Index, headTree map[string]treeEntry, rel string) error {
	if done, err := resetSingle(sw, idx, headTree, rel); done || err != nil {
		return err
	}
	prefix := rel + "/"
	var paths []string
	var entries []index.Entry
	for _, path := range slices.Sorted(maps.Keys(headTree)) {
		if !hasPrefix(path, prefix) {
			continue
		}
		entry, err := headEntry(sw, idx, path, headTree[path])
		if err != nil {
			return err
		}
		paths = append(paths, path)
		entries = append(entries, entry)
	}
	for _, tracked := range collectPaths(idx, prefix) {
		if _, ok := headTree[tracked]; !ok {
			paths = append(paths, tracked)
		}
	}
	idx.Replace(paths, entries)
	return nil
}

func resetSingle(sw *switcher, idx *index.Index, headTree map[string]treeEntry, rel string) (bool, error) {
	he, existsInHead := headTree[rel]
	_, existsInIndex := idx.Get(rel, index.StageMerged)
	if !existsInHead && !existsInIndex && len(idx.Conflicts(rel)) == 0 {
		return false, nil
	}
	if !existsInHead {
		idx.Remove(rel)
		return true, nil
	}
	entry, err := headEntry(sw, idx, rel, he)
	if err == nil {
		idx.Replace([]string{rel}, []index.Entry{entry})
	}
	return true, err
}

func headEntry(sw *switcher, idx *index.Index, rel string, he treeEntry) (index.Entry, error) {
	previous, _ := idx.Get(rel, index.StageMerged)
	return sw.mergedEntry(rel, he.mode, he.id, previous)
}

func hasPrefix(path, prefix string) bool {
	return len(path) > len(prefix) && path[:len(prefix)] == prefix
}

func collectPaths(idx *index.Index, prefix string) []string {
	var out []string
	for p := range idx.Paths(prefix) {
		out = append(out, p)
	}
	return out
}
