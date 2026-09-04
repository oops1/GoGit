package app

import (
	"context"

	"github.com/oops1/gogit/internal/gitcore/hash"
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
	a.writeMu.Lock()
	if a.writeCancel != nil {
		a.writeMu.Unlock()
		return false
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.writeCancel = cancel
	a.writeMu.Unlock()
	r := o.repo
	a.writeWG.Go(func() { a.runWrite(ctx, cancel, r, fn, onDone) })
	return true
}

func (a *App) runWrite(ctx context.Context, cancel context.CancelFunc, r *gitrepo.Repository, fn writeFunc, onDone func(error)) {
	a.pauseWatch()
	err := fn(ctx, r)
	a.resumeWatch()
	a.pokeWatch()
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
	fresh, err := openWorktree(o.repo, worktree.Options{DB: o.db, Refs: o.store, MaxFiles: worktreeMaxFiles})
	if err != nil {
		a.log.Warn("reload working tree failed", "error", err)
		return
	}
	stale := o.swapWorktree(fresh)
	if err := closeWorktree(stale); err != nil {
		a.log.Warn("close previous working tree failed", "error", err)
	}
}

func (a *App) pokeWatch() {
	a.watchMu.Lock()
	w := a.watcher
	a.watchMu.Unlock()
	if w != nil {
		w.Poke()
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
	a.filesMu.Unlock()
	initial := commit.Model{Staged: staged, LastMessage: a.lastCommitMessage()}
	a.showCommit(initial, a.applyCommit)
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
	if !ok || a.opened() == nil {
		return
	}
	var newID hash.ObjectID
	started := a.startWrite(func(ctx context.Context, r *gitrepo.Repository) error {
		id, err := ops.Commit(ctx, r, ops.CommitOptions{Message: m.Message, Amend: m.Amend})
		newID = id
		return err
	}, func(err error) {
		if err != nil {
			a.log.Warn("commit failed", "error", err)
			a.statusLabel.SetText(i18n.Tf("Status.CommitFailed", err))
			return
		}
		a.clearFilesSelection()
		a.startJournal()
		if !newID.IsZero() {
			a.statusLabel.SetText(i18n.Tf("Status.Committed", shortHash(newID)))
		}
	})
	if !started {
		a.log.Warn("commit skipped: another write operation is already running")
	}
}

func (a *App) defaultShowCommit(initial commit.Model, cb func(commit.Model, bool)) {
	view, err := newCommitView(a.eng, initial)
	if err != nil {
		a.log.Warn("open commit dialog failed", "error", err)
		return
	}
	a.wireCommitView(view, cb)
	a.eng.ShowModal(view.Dialog())
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
