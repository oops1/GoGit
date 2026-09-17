package ops

import (
	"context"
	"path/filepath"

	"github.com/oops1/gogit/internal/gitcore/commitgraph"
	"github.com/oops1/gogit/internal/gitcore/diff"
	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/repo"
)

const (
	objectsInfoDir           = "info"
	correctedDateGenerations = 2
)

var (
	writeCommitGraphFile = commitgraph.WriteFile
	commitGraphOpen      = commitgraph.Open
)

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
	return writeCommitGraph(ctx, r, db, walk.commits)
}

func writeCommitGraph(ctx context.Context, r *repo.Repository, db *odb.DB, commits []commitgraph.Commit) (int, error) {
	if len(commits) == 0 {
		return 0, nil
	}
	settings, err := r.CommitGraphSettings()
	if err != nil || !settings.Enabled {
		return 0, err
	}
	opts := commitgraph.EncodeOptions{TopologicalLevelsOnly: settings.GenerationVersion != correctedDateGenerations}
	if existing, _ := commitGraphOpen(db.Dirs(), settings.Open); existing != nil && existing.ChangedPaths() {
		opts.ChangedPaths = true
		if err := fillChangedPaths(ctx, db, commits); err != nil {
			return 0, err
		}
	}
	if err := writeCommitGraphFile(filepath.Join(r.ObjectsDir(), objectsInfoDir), r.ObjectFormat, commits, opts); err != nil {
		return 0, err
	}
	return len(commits), nil
}

func fillChangedPaths(ctx context.Context, db *odb.DB, commits []commitgraph.Commit) error {
	trees := make(map[hash.ObjectID]hash.ObjectID, len(commits))
	for _, c := range commits {
		trees[c.ID] = c.Tree
	}
	opts := diff.Defaults()
	opts.DetectRenames = false
	for at, c := range commits {
		parent := hash.Zero
		if len(c.Parents) > 0 {
			parent = trees[c.Parents[0]]
		}
		files, err := diff.TreeChanges(ctx, db, parent, c.Tree, opts)
		if err != nil {
			return err
		}
		changed := &commitgraph.ChangedPaths{Paths: make([]string, 0, len(files))}
		for _, file := range files {
			changed.Paths = append(changed.Paths, file.NewPath)
		}
		commits[at].Changed = changed
	}
	return nil
}
