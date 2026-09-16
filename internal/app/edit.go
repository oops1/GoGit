package app

import (
	"context"
	"slices"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/hooks"
	"github.com/oops1/gogit/internal/gitcore/ops"
	"github.com/oops1/gogit/internal/gitcore/refs"
	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/gitcore/worktree"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/changes"
	"github.com/oops1/gogit/internal/ui/commit"
)

type writeFunc func(ctx context.Context, r *gitrepo.Repository) error

var newCommitView = commit.NewView

var stageForCommit = ops.Stage

func (a *App) selectedWorkingPaths() []string {
	a.filesMu.Lock()
	mode := a.filesMode
	a.filesMu.Unlock()
	if mode != filesModeWorking {
		return nil
	}
	items := a.filesGrid.Data().Grid.SelectedItems()
	paths := make([]string, 0, len(items))
	for _, it := range items {
		row, ok := it.(changes.Row)
		if !ok || row.RelPath == "" {
			continue
		}
		paths = append(paths, row.RelPath)
	}
	return paths
}

func (a *App) clearFilesSelection() {
	a.filesGrid.Data().Grid.SetSelectedIndex(-1)
	a.setFilesSelected(false)
}

func (a *App) startWrite(fn writeFunc, onDone func(error)) bool {
	o := a.opened()
	if o == nil {
		return false
	}
	a.writeRunMu.Lock()
	defer a.writeRunMu.Unlock()
	ctx, cancel, ok := a.reserveWrite()
	if !ok {
		a.reportBusy()
		return false
	}
	r := o.repo
	a.writeWG.Go(func() { a.runWrite(ctx, cancel, r, fn, onDone) })
	return true
}

func (a *App) reserveWrite() (context.Context, context.CancelFunc, bool) {
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	a.netMu.Lock()
	defer a.netMu.Unlock()
	if a.writeCancel != nil || a.netCancel != nil {
		return nil, nil, false
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.writeCancel = cancel
	return ctx, cancel, true
}

func (a *App) runWrite(ctx context.Context, cancel context.CancelFunc, r *gitrepo.Repository, fn writeFunc, onDone func(error)) {
	resume := a.holdWatch()
	err := fn(ctx, r)
	resume()
	a.writeMu.Lock()
	a.writeCancel = nil
	a.writeMu.Unlock()
	cancel()
	a.Post(func() {
		if err == nil {
			a.reloadWorktree()
		}
		a.refreshWorkingStatus()
		onDone(err)
	})
}

func (a *App) reloadWorktree() {
	o := a.opened()
	if o == nil || o.currentWorktree() == nil {
		return
	}
	fresh, err := openWorktree(o.repo, worktree.Options{DB: o.db, Refs: o.store, MaxFiles: worktreeMaxFiles, IncludeUnmodified: true})
	if err != nil {
		a.log.Warn("reload working tree failed", "error", err)
		return
	}
	stale := o.swapWorktree(fresh)
	if err := closeWorktree(stale); err != nil {
		a.log.Warn("close previous working tree failed", "error", err)
	}
}

func (a *App) stopWrite() {
	a.writeRunMu.Lock()
	defer a.writeRunMu.Unlock()
	a.writeMu.Lock()
	cancel := a.writeCancel
	a.writeMu.Unlock()
	if cancel != nil {
		cancel()
	}
	a.writeWG.Wait()
	a.cancelReads()
	a.readWG.Wait()
}

func (a *App) stageSelected() {
	paths := a.selectedWorkingPaths()
	if len(paths) == 0 {
		return
	}
	a.startWrite(func(ctx context.Context, r *gitrepo.Repository) error {
		return ops.Stage(ctx, r, paths, ops.StageOptions{})
	}, func(err error) {
		if err != nil {
			a.log.Warn("stage failed", "error", err)
			a.statusLabel.SetText(i18n.Tf("Status.StageFailed", err))
			return
		}
		a.clearFilesSelection()
	})
}

func (a *App) unstageSelected() {
	paths := a.selectedWorkingPaths()
	if len(paths) == 0 {
		return
	}
	a.startWrite(func(ctx context.Context, r *gitrepo.Repository) error {
		return ops.Unstage(ctx, r, paths)
	}, func(err error) {
		if err != nil {
			a.log.Warn("unstage failed", "error", err)
			a.statusLabel.SetText(i18n.Tf("Status.UnstageFailed", err))
			return
		}
		a.clearFilesSelection()
	})
}

func (a *App) discardSelected() {
	paths := a.selectedWorkingPaths()
	if len(paths) == 0 {
		return
	}
	title := i18n.T("Dialog.Discard.Title")
	message := i18n.Tf("Dialog.Discard.Message", len(paths))
	a.askConfirm(title, message, func(ok bool) {
		if !ok {
			return
		}
		a.startWrite(func(ctx context.Context, r *gitrepo.Repository) error {
			return ops.Discard(ctx, r, paths, ops.DiscardOptions{RemoveUntracked: true})
		}, func(err error) {
			if err != nil {
				a.log.Warn("discard failed", "error", err)
				a.statusLabel.SetText(i18n.Tf("Status.DiscardFailed", err))
				return
			}
			a.clearFilesSelection()
		})
	})
}

func (a *App) openCommit() {
	if a.opened() == nil {
		return
	}
	a.filesMu.Lock()
	staged := a.stagedCount
	entries := a.currentEntries
	a.filesMu.Unlock()
	initial := commit.Model{Staged: staged, LastMessage: a.lastCommitMessage()}
	state := a.workingMergeState()
	if state.Message != "" {
		initial.Message = state.Message
		initial.Merging = state.InProgress()
	}
	var paths []string
	if staged == 0 && !state.InProgress() {
		paths = a.commitPaths(entries)
		initial.Files = len(paths)
	}
	a.showCommit(initial, func(m commit.Model, ok bool) { a.commitFiles(m, ok, paths) })
}

func (a *App) commitPaths(entries []worktree.Entry) []string {
	if paths := a.selectedWorkingPaths(); len(paths) > 0 {
		return paths
	}
	paths := make([]string, 0, len(entries))
	for _, e := range entries {
		changed := e.Staged != worktree.StatusUnmodified || e.Unstaged != worktree.StatusUnmodified
		if changed && e.Unstaged != worktree.StatusIgnored {
			paths = append(paths, e.Path)
		}
	}
	return paths
}

func (a *App) lastCommitMessage() string {
	o := a.opened()
	if o == nil {
		return ""
	}
	ref, err := o.store.Resolve(refs.HEAD)
	if err != nil || ref.Target.IsZero() {
		return ""
	}
	c, err := loadCommitObject(o.db, ref.Target)
	if err != nil {
		return ""
	}
	return c.Message
}

func (a *App) applyCommit(m commit.Model, ok bool) {
	a.commitFiles(m, ok, nil)
}

var commitHookNames = []string{"pre-commit", "prepare-commit-msg", "commit-msg", "post-commit", "post-rewrite"}

func commitHasHooks(r *gitrepo.Repository) bool {
	return slices.ContainsFunc(commitHookNames, hooks.New(r, nil).Present)
}

func (a *App) commitFiles(m commit.Model, ok bool, paths []string) {
	o := a.opened()
	if !ok || o == nil {
		return
	}
	if commitHasHooks(o.repo) {
		a.commitInOperation(o, m, paths)
		return
	}
	var newID hash.ObjectID
	started := a.startWrite(func(ctx context.Context, r *gitrepo.Repository) error {
		id, err := commitChanges(ctx, r, m, paths, nil)
		newID = id
		return err
	}, func(err error) { a.finishCommit(newID, err) })
	if !started {
		a.log.Warn("commit skipped: another write operation is already running")
	}
}

func commitChanges(ctx context.Context, r *gitrepo.Repository, m commit.Model, paths []string, events hooks.Sink) (hash.ObjectID, error) {
	if len(paths) > 0 {
		if err := stageForCommit(ctx, r, paths, ops.StageOptions{}); err != nil {
			return hash.Zero, err
		}
	}
	return ops.Commit(ctx, r, ops.CommitOptions{
		Message: m.Message,
		Amend:   m.Amend,
		Hooks:   ops.HookOptions{NoVerify: m.NoVerify, Events: events},
	})
}

func (a *App) commitInOperation(o *openedRepository, m commit.Model, paths []string) {
	a.RunOperation(i18n.T("Operation.Title.Commit"), func(ctx context.Context, reporter OperationReporter) error {
		id, err := commitChanges(ctx, o.repo, m, paths, hookEvents(reporter))
		reportHookRejection(reporter, err)
		reporter.Then(func() {
			if err == nil {
				a.reloadWorktree()
			}
			a.refreshWorkingStatus()
			a.finishCommit(id, err)
		})
		return err
	})
}

func (a *App) finishCommit(id hash.ObjectID, err error) {
	if err != nil {
		a.log.Warn("commit failed", "error", err)
		text, hooked := hookFailureText(err)
		if !hooked {
			text = i18n.Tf("Status.CommitFailed", err)
		}
		a.statusLabel.SetText(text)
		return
	}
	a.clearFilesSelection()
	a.startJournal()
	a.statusLabel.SetText(i18n.Tf("Status.Committed", shortHash(id)))
}

func (a *App) defaultShowCommit(initial commit.Model, cb func(commit.Model, bool)) {
	view, err := newCommitView(a.eng, initial)
	if err != nil {
		a.log.Warn("open commit dialog failed", "error", err)
		return
	}
	a.wireCommitView(view, cb)
	a.showModal(view.Dialog(), view)
}

func (a *App) wireCommitView(view *commit.View, cb func(commit.Model, bool)) {
	view.OnOK = func(m commit.Model) {
		a.eng.CloseModal(view.Dialog())
		cb(m, true)
	}
	view.OnCancel = func() {
		a.eng.CloseModal(view.Dialog())
		cb(commit.Model{}, false)
	}
}
