package ops

import (
	"context"
	"path/filepath"

	"github.com/oops1/gogit/internal/gitcore/commitgraph"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/repo"
)

const objectsInfoDir = "info"

var writeCommitGraphFile = commitgraph.WriteFile

func WriteCommitGraph(ctx context.Context, r *repo.Repository) (int, error) {
	shallow, err := r.IsShallow()
	if err != nil || shallow {
		return 0, err
	}
	db, err := odbOpen(r.ObjectsDir(), odb.Options{Format: r.ObjectFormat})
	if err != nil {
		return 0, err
	}
	defer func() { _ = db.Close() }()
	walk, err := reachableObjects(ctx, r, db)
	if err != nil {
		return 0, err
	}
	return writeCommitGraph(r, walk.commits)
}

func writeCommitGraph(r *repo.Repository, commits []commitgraph.Commit) (int, error) {
	if len(commits) == 0 {
		return 0, nil
	}
	if err := writeCommitGraphFile(filepath.Join(r.ObjectsDir(), objectsInfoDir), r.ObjectFormat, commits); err != nil {
		return 0, err
	}
	return len(commits), nil
}
