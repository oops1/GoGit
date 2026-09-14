package app

import (
	"cmp"
	"context"
	"errors"
	"fmt"

	"github.com/oops1/headless-gui/v3/widget/datagrid"

	"github.com/oops1/gogit/internal/gitcore/diff"
	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/worktree"
	"github.com/oops1/gogit/internal/ui/changes"
	"github.com/oops1/gogit/internal/ui/filesgrid"
)

var ErrNotACommit = errors.New("app: object is not a commit")

var diffTreesFunc = diff.Trees

var diffOptions = diff.Defaults()

var loadCommitObject = func(db *odb.DB, id hash.ObjectID) (*object.Commit, error) {
	kind, data, err := db.Get(id)
	if err != nil {
		return nil, err
	}
	if kind != object.TypeCommit {
		return nil, fmt.Errorf("%w: %s is a %s", ErrNotACommit, id, kind)
	}
	return object.ParseCommit(data)
}

func (a *App) onFilesRowSelected(e datagrid.SelectionChangedEvent) {
	row, ok := e.SelectedItem.(changes.Row)
	if !ok || e.SelectedIndex < 0 {
		a.setFilesSelected(false)
		return
	}
	a.filesMu.Lock()
	mode := a.filesMode
	files := a.currentFiles
	db := a.currentFilesDB
	entries := a.currentEntries
	a.filesMu.Unlock()
	a.setFilesSelected(mode == filesModeWorking && row.RelPath != "")
	if mode == filesModeCommit {
		if file, ok := fileOf(files, row); ok {
			a.showDiff(a.commitTarget(db, file))
		}
		return
	}
	if entry, ok := entryOf(entries, row); ok {
		a.showWorkingDiff(entry)
	}
}

func fileOf(files []diff.File, row changes.Row) (diff.File, bool) {
	for _, file := range files {
		if cmp.Or(file.NewPath, file.OldPath) == row.RelPath {
			return file, true
		}
	}
	return diff.File{}, false
}

func entryOf(entries []worktree.Entry, row changes.Row) (worktree.Entry, bool) {
	for _, entry := range entries {
		if entry.Path == row.RelPath {
			return entry, true
		}
	}
	return worktree.Entry{}, false
}

func (a *App) startDiff(id hash.ObjectID) {
	a.stopWorking()
	a.diffRunMu.Lock()
	defer a.diffRunMu.Unlock()
	a.stopDiffLocked()
	o := a.opened()
	if o == nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.diffMu.Lock()
	a.diffCancel = cancel
	a.diffMu.Unlock()
	db := o.db
	a.diffWG.Go(func() { a.runDiff(ctx, db, id) })
}

func (a *App) stopDiff() {
	a.diffRunMu.Lock()
	defer a.diffRunMu.Unlock()
	a.stopDiffLocked()
}

func (a *App) stopDiffLocked() {
	a.diffMu.Lock()
	cancel := a.diffCancel
	a.diffCancel = nil
	a.diffMu.Unlock()
	if cancel != nil {
		cancel()
	}
	a.diffWG.Wait()
}

func (a *App) runDiff(ctx context.Context, db *odb.DB, id hash.ObjectID) {
	files, err := a.loadDiffFiles(ctx, db, id)
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			a.log.Warn("load diff failed", "error", err)
		}
		return
	}
	rows := changes.Files(files)
	if len(files) > changes.MaxFiles {
		files = files[:changes.MaxFiles]
	}
	a.filesMu.Lock()
	a.filesMode = filesModeCommit
	a.currentFiles = files
	a.currentFilesDB = db
	a.currentEntries = nil
	a.filesMu.Unlock()
	var first diffTarget
	hasFirst := len(files) > 0
	if hasFirst {
		first = a.commitTarget(db, files[0])
	}
	a.Post(func() {
		if ctx.Err() != nil {
			return
		}
		a.setFilesRows(rows)
		if hasFirst {
			a.showDiff(first)
		} else {
			a.clearDiff()
		}
	})
}

func (a *App) loadDiffFiles(ctx context.Context, db *odb.DB, id hash.ObjectID) ([]diff.File, error) {
	commit, err := loadCommitObject(db, id)
	if err != nil {
		return nil, err
	}
	parentTree := hash.Zero
	if len(commit.Parents) > 0 {
		parent, err := loadCommitObject(db, commit.Parents[0])
		if err != nil {
			return nil, err
		}
		parentTree = parent.Tree
	}
	return diffTreesFunc(ctx, db, parentTree, commit.Tree, diffOptions)
}

func (a *App) restoreFilesColumns() {
	order := stringsToColumnIDs(a.cfg.UI.FilesColumns)
	visible := stringsToColumnIDs(a.cfg.UI.FilesVisibleColumns)
	a.filesGrid.SetColumns(order, visible)
}

func (a *App) saveFilesColumns(order, visible []filesgrid.ColumnID) {
	a.cfg.UI.FilesColumns = columnIDsToStrings(order)
	a.cfg.UI.FilesVisibleColumns = columnIDsToStrings(visible)
	if err := a.cfg.Save(a.paths.ConfigFile()); err != nil {
		a.log.Warn("save config failed", "error", err)
	}
}

func stringsToColumnIDs(names []string) []filesgrid.ColumnID {
	ids := make([]filesgrid.ColumnID, len(names))
	for i, name := range names {
		ids[i] = filesgrid.ColumnID(name)
	}
	return ids
}

func columnIDsToStrings(ids []filesgrid.ColumnID) []string {
	names := make([]string, len(ids))
	for i, id := range ids {
		names[i] = string(id)
	}
	return names
}

func (a *App) clearChangesPanels() {
	a.stopDiff()
	a.stopWorking()
	a.filesMu.Lock()
	a.filesMode = filesModeWorking
	a.currentFiles = nil
	a.currentFilesDB = nil
	a.currentEntries = nil
	a.activeModified = false
	a.filesDirFilter = ""
	a.filesMu.Unlock()
	a.selectedCommit = hash.ObjectID{}
	a.setCommitSelected(false)
	a.setFilesRows(nil)
	a.clearDiff()
	a.setFilesSelected(false)
	a.setHasStagedChanges(false)
	a.setHasChanges(false)
}

func (a *App) commitTarget(db *odb.DB, file diff.File) diffTarget {
	target, err := commitDiffTarget(db, file)
	if err != nil {
		a.log.Warn("load commit diff failed", "error", err)
	}
	return target
}

func commitDiffTarget(db *odb.DB, file diff.File) (diffTarget, error) {
	target := diffTarget{file: file}
	if db == nil || file.Binary {
		return target, nil
	}
	var err error
	if target.oldData, err = commitSideData(db, file.OldMode, file.OldID); err != nil {
		return target, err
	}
	target.newData, err = commitSideData(db, file.NewMode, file.NewID)
	return target, err
}

func commitSideData(db *odb.DB, mode object.Mode, id hash.ObjectID) ([]byte, error) {
	if mode == object.ModeSubmodule {
		return []byte("Subproject commit " + id.String() + "\n"), nil
	}
	data, _, err := blobData(db, id)
	return data, err
}
