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
	if err != nil {
		return 0, err
	}
	if shallow {
		return 0, nil
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
	if len(walk.commits) == 0 {
		return 0, nil
	}
	if err := writeCommitGraphFile(filepath.Join(r.ObjectsDir(), objectsInfoDir), r.ObjectFormat, walk.commits); err != nil {
		return 0, err
	}
	return len(walk.commits), nil
}
