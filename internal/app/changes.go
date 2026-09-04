package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/oops1/headless-gui/v3/widget/datagrid"

	"github.com/oops1/gogit/internal/gitcore/diff"
	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/odb"
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
	entries := a.currentEntries
	a.filesMu.Unlock()
	a.setFilesSelected(mode == filesModeWorking && row.RelPath != "")
	if mode == filesModeCommit {
		if e.SelectedIndex >= len(files) {
			return
		}
		a.diffView.SetDocument(changes.FromFile(files[e.SelectedIndex]))
		return
	}
	if e.SelectedIndex >= len(entries) {
		return
	}
	a.showWorkingDiff(entries[e.SelectedIndex])
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
	a.currentEntries = nil
	a.filesMu.Unlock()
	var first diff.File
	hasFirst := len(files) > 0
	if hasFirst {
		first = files[0]
	}
	a.Post(func() {
		a.setFilesRows(rows)
		if hasFirst {
			a.diffView.SetDocument(changes.FromFile(first))
		} else {
			a.diffView.Clear()
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
	a.currentEntries = nil
	a.activeModified = false
	a.filesMu.Unlock()
	a.selectedCommit = hash.ObjectID{}
	a.setCommitSelected(false)
	a.setFilesRows(nil)
	a.diffView.Clear()
	a.setFilesSelected(false)
	a.setHasStagedChanges(false)
}
