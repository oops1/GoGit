package app

import (
	"cmp"
	"context"
	"errors"

	"github.com/oops1/gogit/internal/gitcore/attributes"
	"github.com/oops1/gogit/internal/gitcore/diff"
	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/index"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/worktree"
	"github.com/oops1/gogit/internal/ui/changes"
)

type filesMode int

const (
	filesModeWorking filesMode = iota
	filesModeCommit
)

func (a *App) requestWorking() {
	a.workingFlagMu.Lock()
	if a.workingBusy {
		a.workingAgain = true
		a.workingFlagMu.Unlock()
		return
	}
	a.workingFlagMu.Unlock()
	a.startWorking()
}

func (a *App) finishWorking() {
	a.workingFlagMu.Lock()
	a.workingBusy = false
	again := a.workingAgain
	a.workingAgain = false
	a.workingFlagMu.Unlock()
	if again {
		a.Post(a.requestWorking)
	}
}

func (a *App) startWorking() {
	a.workingRunMu.Lock()
	defer a.workingRunMu.Unlock()
	a.stopWorkingLocked()
	o := a.opened()
	if o == nil || o.currentWorktree() == nil {
		a.filesMu.Lock()
		a.filesMode = filesModeWorking
		a.currentEntries = nil
		a.activeModified = false
		a.stagedCount = 0
		a.filesMu.Unlock()
		a.setFilesRows(nil)
		a.reposView.Render(a.registry, a.repoTreeState())
		a.setHasStagedChanges(false)
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.workingMu.Lock()
	a.workingCancel = cancel
	a.workingMu.Unlock()
	wt := o.currentWorktree()
	a.workingFlagMu.Lock()
	a.workingBusy = true
	a.workingFlagMu.Unlock()
	a.workingWG.Go(func() { a.runWorking(ctx, wt) })
}

func (a *App) stopWorking() {
	a.workingRunMu.Lock()
	defer a.workingRunMu.Unlock()
	a.stopWorkingLocked()
}

func (a *App) stopWorkingLocked() {
	a.workingMu.Lock()
	cancel := a.workingCancel
	a.workingCancel = nil
	a.workingMu.Unlock()
	if cancel != nil {
		cancel()
	}
	a.workingWG.Wait()
}

func (a *App) runWorking(ctx context.Context, wt *worktree.Worktree) {
	defer a.finishWorking()
	status, err := wt.Status(ctx)
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			a.log.Warn("load working status failed", "error", err)
		}
		return
	}
	rows := changes.WorkingRows(status)
	entries := changes.SortEntries(status.Entries)
	if len(entries) > changes.MaxFiles {
		entries = entries[:changes.MaxFiles]
	}
	modified := hasWorkingChanges(status.Entries)
	staged := stagedEntryCount(status.Entries)
	a.filesMu.Lock()
	a.filesMode = filesModeWorking
	a.currentEntries = entries
	a.currentFiles = nil
	a.activeModified = modified
	a.stagedCount = staged
	a.mutedDirs = mutedDirectories(entries)
	a.filesMu.Unlock()
	a.Post(func() {
		if ctx.Err() != nil {
			return
		}
		a.setFilesRows(rows)
		a.reposView.Render(a.registry, a.repoTreeState())
		a.setHasStagedChanges(staged > 0)
		a.syncWatcherSkips()
	})
}

func hasWorkingChanges(entries []worktree.Entry) bool {
	for _, e := range entries {
		if e.Staged != worktree.StatusUnmodified || e.Unstaged != worktree.StatusUnmodified {
			return true
		}
	}
	return false
}

func stagedEntryCount(entries []worktree.Entry) int {
	count := 0
	for _, e := range entries {
		if e.Conflict == worktree.ConflictNone && e.Staged != worktree.StatusUnmodified {
			count++
		}
	}
	return count
}

func (a *App) refreshWorkingStatus() {
	if a.commitIsSelected() {
		return
	}
	a.requestWorking()
}

func (a *App) showWorkingDiff(entry worktree.Entry) {
	a.diffRunMu.Lock()
	defer a.diffRunMu.Unlock()
	a.stopDiffLocked()
	o := a.opened()
	if o == nil || entry.IsDir {
		a.diffView.Clear()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.diffMu.Lock()
	a.diffCancel = cancel
	a.diffMu.Unlock()
	a.diffWG.Go(func() { a.runWorkingDiff(ctx, o, entry) })
}

func (a *App) runWorkingDiff(ctx context.Context, o *openedRepository, entry worktree.Entry) {
	file, err := buildWorkingDiff(ctx, o, entry)
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			a.log.Warn("load working diff failed", "error", err)
		}
		return
	}
	a.Post(func() { a.diffView.SetDocument(changes.FromFile(file)) })
}

func buildWorkingDiff(ctx context.Context, o *openedRepository, entry worktree.Entry) (diff.File, error) {
	if entry.Conflict != worktree.ConflictNone {
		return conflictDiff(ctx, o, entry)
	}
	if entry.Unstaged != worktree.StatusUnmodified {
		return workTreeDiff(ctx, o, entry)
	}
	return indexDiff(ctx, o, entry)
}

func indexDiff(ctx context.Context, o *openedRepository, entry worktree.Entry) (diff.File, error) {
	oldPath := cmp.Or(entry.OrigPath, entry.Path)
	_, oldID, err := o.worktree.HeadBlob(ctx, oldPath)
	if err != nil {
		return diff.File{}, err
	}
	oldData, oldPresent, err := blobData(o.db, oldID)
	if err != nil {
		return diff.File{}, err
	}
	var newID hash.ObjectID
	if ie, ok := o.worktree.Index().Get(entry.Path, index.StageMerged); ok {
		newID = ie.ID
	}
	newData, newPresent, err := blobData(o.db, newID)
	if err != nil {
		return diff.File{}, err
	}
	return buildDiffFile(oldPath, entry.Path, oldData, newData, oldPresent, newPresent), nil
}

func workTreeDiff(ctx context.Context, o *openedRepository, entry worktree.Entry) (diff.File, error) {
	var oldID hash.ObjectID
	if ie, ok := o.worktree.Index().Get(entry.Path, index.StageMerged); ok {
		oldID = ie.ID
	}
	return blobVsWorktreeDiff(ctx, o, entry.Path, oldID)
}

func conflictDiff(ctx context.Context, o *openedRepository, entry worktree.Entry) (diff.File, error) {
	var oldID hash.ObjectID
	for _, stage := range []index.Stage{index.StageOurs, index.StageTheirs, index.StageAncestor} {
		if ie, ok := o.worktree.Index().Get(entry.Path, stage); ok {
			oldID = ie.ID
			break
		}
	}
	return blobVsWorktreeDiff(ctx, o, entry.Path, oldID)
}

func blobVsWorktreeDiff(ctx context.Context, o *openedRepository, path string, oldID hash.ObjectID) (diff.File, error) {
	oldData, oldPresent, err := blobData(o.db, oldID)
	if err != nil {
		return diff.File{}, err
	}
	if err := ctx.Err(); err != nil {
		return diff.File{}, err
	}
	newData, newPresent, err := o.worktree.WorkingFile(path)
	if err != nil {
		return diff.File{}, err
	}
	return buildDiffFile(path, path, oldData, newData, oldPresent, newPresent), nil
}

func blobData(db *odb.DB, id hash.ObjectID) ([]byte, bool, error) {
	if id.IsZero() {
		return nil, false, nil
	}
	_, data, err := db.Get(id)
	if err != nil {
		return nil, false, err
	}
	return data, true, nil
}

func buildDiffFile(oldPath, newPath string, oldData, newData []byte, oldPresent, newPresent bool) diff.File {
	file := diff.File{OldPath: oldPath, NewPath: newPath, OldSize: len(oldData), NewSize: len(newData)}
	if !oldPresent && !newPresent {
		return file
	}
	if attributes.IsBinaryContent(oldData) || attributes.IsBinaryContent(newData) {
		file.Binary = true
		return file
	}
	file.Hunks = diff.Blobs(oldData, newData, diffOptions)
	return file
}
