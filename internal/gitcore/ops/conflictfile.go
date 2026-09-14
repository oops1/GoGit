package ops

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/oops1/gogit/internal/gitcore/attributes"
	"github.com/oops1/gogit/internal/gitcore/index"
	"github.com/oops1/gogit/internal/gitcore/merge"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/repo"
)

const conflictStyleKey = "merge.conflictStyle"

type ConflictFile struct {
	Path       string
	Base       []byte
	Ours       []byte
	Theirs     []byte
	HasBase    bool
	HasOurs    bool
	HasTheirs  bool
	Binary     bool
	MarkerSize int
	Style      merge.Style
	Blocks     []merge.Chunk
}

type ResolutionOptions struct {
	MarkResolved bool
}

func ReadConflict(ctx context.Context, r *repo.Repository, path string) (ConflictFile, error) {
	if err := ctx.Err(); err != nil {
		return ConflictFile{}, err
	}
	rel, err := cleanRepoPath(path)
	if err != nil {
		return ConflictFile{}, err
	}
	wt, err := openWorkingTree(r)
	if err != nil {
		return ConflictFile{}, err
	}
	defer func() { _ = wt.close() }()
	idx, err := readIndex(r)
	if err != nil {
		return ConflictFile{}, err
	}
	stages := idx.Conflicts(rel)
	if len(stages) == 0 {
		return ConflictFile{}, fmt.Errorf("%w: %s", ErrNotConflicted, rel)
	}
	db, err := odbOpen(r.ObjectsDir(), odb.Options{Format: r.ObjectFormat})
	if err != nil {
		return ConflictFile{}, err
	}
	defer func() { _ = db.Close() }()

	file := ConflictFile{Path: rel, MarkerSize: wt.markerSizeOf(rel), Style: conflictStyle(r)}
	for _, entry := range stages {
		if err := file.takeStage(db, entry); err != nil {
			return ConflictFile{}, err
		}
	}
	file.Binary = attributes.IsBinaryContent(file.Ours) || attributes.IsBinaryContent(file.Theirs) ||
		attributes.IsBinaryContent(file.Base)
	if !file.Binary {
		file.Blocks = merge.Chunks(file.Base, file.Ours, file.Theirs, merge.Options{
			Style:      file.Style,
			MarkerSize: file.MarkerSize,
		})
	}
	return file, nil
}

func (f *ConflictFile) takeStage(db *odb.DB, entry index.Entry) error {
	kind, data, err := dbGet(db, entry.ID)
	if err != nil {
		return err
	}
	if kind != object.TypeBlob {
		return nil
	}
	switch entry.Stage {
	case index.StageAncestor:
		f.Base, f.HasBase = data, true
	case index.StageOurs:
		f.Ours, f.HasOurs = data, true
	case index.StageTheirs:
		f.Theirs, f.HasTheirs = data, true
	}
	return nil
}

func conflictStyle(r *repo.Repository) merge.Style {
	style, _ := r.Config().Get(conflictStyleKey)
	switch style {
	case "diff3":
		return merge.StyleDiff3
	case "zdiff3":
		return merge.StyleZDiff3
	}
	return merge.StyleMerge
}

func SaveResolution(ctx context.Context, r *repo.Repository, path string, content []byte, opts ResolutionOptions) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	rel, err := cleanRepoPath(path)
	if err != nil {
		return err
	}
	if err := writeWorkingFile(r, rel, content); err != nil {
		return err
	}
	if !opts.MarkResolved {
		return nil
	}
	return Stage(ctx, r, []string{rel}, StageOptions{})
}

func writeWorkingFile(r *repo.Repository, rel string, content []byte) error {
	wt, err := openWorkingTree(r)
	if err != nil {
		return err
	}
	defer func() { _ = wt.close() }()
	if dir := parentOf(rel); dir != "" {
		if err := fsRootMkdirAll(wt.root, filepath.FromSlash(dir), 0o777); err != nil {
			return err
		}
	}
	perm := fs.FileMode(0o666)
	file, err := fsRootOpenFile(wt.root, filepath.FromSlash(rel), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	if _, err := file.Write(wt.checkoutConvert(rel, content)); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}
