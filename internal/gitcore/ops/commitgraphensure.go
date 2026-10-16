package ops

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/oops1/gogit/internal/gitcore/commitgraph"
	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/repo"
)

var commitGraphStat = os.Stat

func EnsureCommitGraph(ctx context.Context, r *repo.Repository) (int, error) {
	settings, err := r.CommitGraphSettings()
	if err != nil || !settings.Enabled {
		return 0, err
	}
	wanted, err := gcWritesCommitGraph(r)
	if err != nil || !wanted {
		return 0, err
	}
	db, err := odbOpen(r.ObjectsDir(), odbOptions(r))
	if err != nil {
		return 0, err
	}
	defer func() { _ = db.Close() }()
	if commitGraphPresent(db) {
		return 0, nil
	}
	commits, err := reachableCommits(ctx, r, db)
	if err != nil {
		return 0, err
	}
	return writeCommitGraph(ctx, r, db, commits)
}

func commitGraphPresent(db *odb.DB) bool {
	for _, dir := range db.Dirs() {
		info := filepath.Join(dir, commitgraph.InfoDir)
		for _, path := range []string{
			filepath.Join(info, commitgraph.FileName),
			filepath.Join(info, commitgraph.GraphsDir, commitgraph.ChainFileName),
		} {
			if _, err := commitGraphStat(path); err == nil {
				return true
			}
		}
	}
	return false
}

func reachableCommits(ctx context.Context, r *repo.Repository, db *odb.DB) ([]commitgraph.Commit, error) {
	store, err := refsOpen(refs.Options{GitDir: r.GitDir(), CommonDir: r.CommonDir(), Bare: r.IsBare(), Peeler: db})
	if err != nil {
		return nil, err
	}
	defer func() { _ = store.Close() }()
	pending, err := commitGraphRoots(store)
	if err != nil {
		return nil, err
	}
	seen := make(map[hash.ObjectID]struct{}, len(pending))
	var commits []commitgraph.Commit
	for len(pending) > 0 {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		id := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if _, done := seen[id]; done || id.IsZero() {
			continue
		}
		seen[id] = struct{}{}
		kind, target, err := db.Peel(id)
		if err != nil {
			return nil, err
		}
		if kind != object.TypeCommit {
			continue
		}
		if target != id {
			if _, done := seen[target]; done {
				continue
			}
			seen[target] = struct{}{}
		}
		commit, err := db.Commit(target)
		if err != nil {
			return nil, err
		}
		commits = append(commits, commitgraph.Commit{ID: target, Tree: commit.Tree, Parents: commit.Parents, Time: commit.Committer.When.Unix()})
		pending = append(pending, commit.Parents...)
	}
	return commits, nil
}

func commitGraphRoots(store *refs.Store) ([]hash.ObjectID, error) {
	var roots []hash.ObjectID
	for ref, err := range store.All() {
		if err != nil {
			return nil, err
		}
		roots = append(roots, ref.Target)
	}
	head, err := store.Resolve(refs.HEAD)
	if errors.Is(err, refs.ErrNotFound) {
		return roots, nil
	}
	if err != nil {
		return nil, err
	}
	return append(roots, head.Target), nil
}
