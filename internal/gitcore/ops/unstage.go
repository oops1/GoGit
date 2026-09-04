package ops

import (
	"context"

	"github.com/oops1/gogit/internal/gitcore/index"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/repo"
)

func Unstage(ctx context.Context, r *repo.Repository, paths []string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
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
		unstagePath(lock.idx, headTree, clean)
	}
	return lock.commit()
}

func unstagePath(idx *index.Index, headTree map[string]treeEntry, rel string) {
	if resetSingle(idx, headTree, rel) {
		return
	}
	prefix := rel + "/"
	for path, he := range headTree {
		if !hasPrefix(path, prefix) {
			continue
		}
		idx.Add(index.Entry{Path: path, Mode: he.mode, ID: he.id, Stage: index.StageMerged})
	}
	for _, tracked := range collectPaths(idx, prefix) {
		if _, ok := headTree[tracked]; !ok {
			idx.Remove(tracked)
		}
	}
}

func resetSingle(idx *index.Index, headTree map[string]treeEntry, rel string) bool {
	he, existsInHead := headTree[rel]
	_, existsInIndex := idx.Get(rel, index.StageMerged)
	if !existsInHead && !existsInIndex {
		return false
	}
	if existsInHead {
		idx.Add(index.Entry{Path: rel, Mode: he.mode, ID: he.id, Stage: index.StageMerged})
	} else {
		idx.Remove(rel)
	}
	return true
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
