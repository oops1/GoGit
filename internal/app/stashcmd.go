package app

import (
	"context"
	"errors"
	"slices"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/gitcore/diff"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/ops"
	"github.com/oops1/gogit/internal/gitcore/refs"
	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/branches"
	"github.com/oops1/gogit/internal/ui/changes"
	"github.com/oops1/gogit/internal/ui/stash"
	"github.com/oops1/gogit/internal/ui/switchchanges"
)

var newStashView = stash.NewView

var newSaveStashView = stash.NewSaveView

var runStashApply = ops.StashApply

var runStashPop = ops.StashPop

var runStashDrop = ops.StashDrop

var runStashShow = ops.StashShow

func (a *App) registerStashHandlers() {
	a.handlers[CmdStashSave] = a.openSaveStash
	a.handlers[CmdStashSelection] = a.openStashSelection
	a.handlers[CmdStashApply] = func() { a.openStashDialog(stash.ModeApply, 0) }
	a.handlers[CmdStashDrop] = func() { a.openStashDialog(stash.ModeDrop, 0) }
	a.branchesView.OnSelect = a.onBranchRefSelected
}

func (a *App) refMenu(ref refs.Name) []widget.MenuItem {
	if index, ok := branches.StashIndex(ref); ok {
		return a.stashMenu(index)
	}
	return a.branchMenu(ref)
}

func (a *App) activateRef(ref refs.Name) {
	if index, ok := branches.StashIndex(ref); ok {
		a.openStashDialog(stash.ModeApply, index)
		return
	}
	a.checkOutRef(ref)
}

func (a *App) stashMenu(index int) []widget.MenuItem {
	return []widget.MenuItem{
		menuItem("Menu.Stash.Apply", func() { a.applyStash(index, false, ops.StashApplyOptions{}) }),
		menuItem("Menu.Stash.ApplyDrop", func() { a.applyStash(index, true, ops.StashApplyOptions{}) }),
		menuSeparator(),
		menuItem("Menu.Stash.Drop", func() { a.confirmDropStash(index) }),
	}
}

func stashSelector(index int) string {
	return branches.StashRef(index).String()
}

func stashLabel(entry branches.Stash) string {
	return stashSelector(entry.Index) + ": " + entry.Message
}

func stashLabels(entries []branches.Stash) []string {
	labels := make([]string, 0, len(entries))
	for _, entry := range entries {
		labels = append(labels, stashLabel(entry))
	}
	return labels
}

func (a *App) saveStashMenuItems() []widget.MenuItem {
	state := a.State()
	return []widget.MenuItem{
		enabledItem("Menu.Local.SaveStash", func() { a.Dispatch(CmdStashSave) }, state.Enabled(CmdStashSave)),
		enabledItem("Menu.Files.StashSelection", func() { a.Dispatch(CmdStashSelection) }, state.Enabled(CmdStashSelection)),
	}
}

func (a *App) applyStashMenuItems() []widget.MenuItem {
	o := a.opened()
	if o == nil {
		return nil
	}
	snap, err := loadBranchSnapshot(o.store)
	if err != nil {
		a.log.Warn("read stashes failed", "error", err)
		return nil
	}
	items := make([]widget.MenuItem, 0, len(snap.Stashes))
	for _, entry := range snap.Stashes {
		index := entry.Index
		items = append(items, widget.MenuItem{
			Text:    stashLabel(entry),
			Icon:    menuKeyIcon("Menu.Stash.Apply"),
			OnClick: func() { a.openStashDialog(stash.ModeApply, index) },
		})
	}
	return items
}

func (a *App) openSaveStash() {
	a.showSaveStash(nil, false)
}

func (a *App) openStashSelection() {
	rows := a.selectedWorkingRows()
	if len(rows) == 0 {
		return
	}
	paths := make([]string, 0, len(rows))
	untracked := false
	for _, row := range rows {
		paths = append(paths, row.RelPath)
		untracked = untracked || row.Status == changes.RowUntracked
	}
	a.showSaveStash(paths, untracked)
}

func (a *App) showSaveStash(paths []string, untracked bool) {
	if a.opened() == nil {
		return
	}
	view, err := newSaveStashView(len(paths))
	if err != nil {
		a.log.Warn("open save stash dialog failed", "error", err)
		return
	}
	view.OnOK = func(req stash.SaveRequest) {
		a.eng.CloseModal(view.Dialog())
		a.saveStash(ops.StashOptions{
			Message:          req.Message,
			IncludeUntracked: req.IncludeUntracked || untracked,
			KeepIndex:        req.KeepIndex,
			Paths:            paths,
		})
	}
	view.OnCancel = func() { a.eng.CloseModal(view.Dialog()) }
	a.showModal(view.Dialog(), view)
}

func (a *App) saveStash(opts ops.StashOptions) {
	o := a.opened()
	if o == nil {
		return
	}
	a.RunOperation(i18n.T("Operation.Title.SaveStash"), func(ctx context.Context, reporter OperationReporter) error {
		defer a.Post(a.finishMerge)
		r, err := a.freshRepo(o)
		if err != nil {
			return err
		}
		defer func() { _ = r.Close() }()
		_, err = runStashPush(ctx, r, opts)
		return reportStashSave(reporter, err)
	})
}

func reportStashSave(reporter OperationReporter, err error) error {
	var pathspec *ops.PathspecError
	switch {
	case errors.Is(err, ops.ErrNothingToStash):
		reporter.Log(i18n.T("Operation.Log.NothingToStash"))
		return nil
	case errors.As(err, &pathspec):
		reporter.Log(i18n.Tf("Operation.Log.StashPathspec", switchchanges.ListPaths(pathspec.Specs)))
	case errors.Is(err, ops.ErrStashWorktreeKept):
		reporter.Log(i18n.Tf("Operation.Log.StashWorktreeKept", stashSelector(0)))
	case errors.Is(err, ops.ErrUnmergedPaths):
		reporter.Log(i18n.T("Operation.Log.StashUnmerged"))
	case err == nil:
		reporter.Log(i18n.Tf("Operation.Log.StashSaved", stashSelector(0)))
	}
	return err
}

func (a *App) openStashDialog(mode stash.Mode, selected int) {
	o := a.opened()
	if o == nil {
		return
	}
	snap, err := loadBranchSnapshot(o.store)
	if err != nil {
		a.log.Warn("read stashes failed", "error", err)
		return
	}
	view, err := newStashView(mode)
	if err != nil {
		a.log.Warn("open stash dialog failed", "error", err)
		return
	}
	view.SetEntries(stashLabels(snap.Stashes), selected)
	view.OnOK = func(req stash.Request) {
		a.eng.CloseModal(view.Dialog())
		if mode == stash.ModeDrop {
			a.dropStash(req.Index)
			return
		}
		a.applyStash(req.Index, req.Drop, ops.StashApplyOptions{Index: req.RestoreIndex})
	}
	view.OnCancel = func() { a.eng.CloseModal(view.Dialog()) }
	a.showModal(view.Dialog(), view)
}

func (a *App) applyStash(index int, drop bool, opts ops.StashApplyOptions) {
	o := a.opened()
	if o == nil {
		return
	}
	apply := runStashApply
	if drop {
		apply = runStashPop
	}
	a.RunOperation(i18n.T("Operation.Title.ApplyStash"), func(ctx context.Context, reporter OperationReporter) error {
		defer a.Post(a.finishMerge)
		r, err := a.freshRepo(o)
		if err != nil {
			return err
		}
		defer func() { _ = r.Close() }()
		result, err := apply(ctx, r, index, opts)
		reportStashApply(reporter, stashSelector(index), result, err)
		return err
	})
}

func reportStashApply(reporter OperationReporter, selector string, result ops.StashApplyResult, err error) {
	var overwrite *ops.OverwriteError
	var untracked *ops.UntrackedRestoreError
	switch {
	case errors.As(err, &overwrite):
		reporter.Log(i18n.Tf("Operation.Log.StashBlocked", overwriteList(overwrite)))
	case errors.Is(err, ops.ErrStashIndexConflicts):
		reporter.Log(i18n.T("Operation.Log.StashIndexConflicts"))
	case errors.Is(err, ops.ErrUnmergedPaths):
		reporter.Log(i18n.T("Operation.Log.StashUnmerged"))
	case errors.As(err, &untracked):
		reporter.Log(untrackedRestoreText(selector, untracked))
	case err != nil:
	case !result.Clean():
		reporter.Log(i18n.Tf("Operation.Log.StashConflicts", switchchanges.ListPaths(result.Conflicts)))
	default:
		reporter.Log(i18n.Tf("Operation.Log.StashApplied", selector))
		if result.Dropped {
			reporter.Log(i18n.Tf("Operation.Log.StashDropped", selector))
		}
	}
}

func untrackedRestoreText(selector string, untracked *ops.UntrackedRestoreError) string {
	if untracked.Blocked != "" {
		return i18n.Tf("Operation.Log.StashUntrackedBlocked", selector, untracked.Blocked)
	}
	return i18n.Tf("Operation.Log.StashUntrackedExists", selector, switchchanges.ListPaths(untracked.Existing))
}

func (a *App) confirmDropStash(index int) {
	a.askConfirm(i18n.T("Dialog.DropStash.Title"), i18n.Tf("Dialog.DropStash.Message", stashSelector(index)), func(ok bool) {
		if ok {
			a.dropStash(index)
		}
	})
}

func (a *App) dropStash(index int) {
	selector := stashSelector(index)
	a.startWrite(func(ctx context.Context, r *gitrepo.Repository) error {
		return runStashDrop(ctx, r, index)
	}, func(err error) {
		if err != nil {
			a.log.Warn("drop stash failed", "stash", selector, "error", err)
			a.statusLabel.SetText(i18n.Tf("Status.StashDropFailed", err))
		} else {
			a.statusLabel.SetText(i18n.Tf("Status.StashDropped", selector))
		}
		a.RefreshRepository()
	})
}

func (a *App) onBranchRefSelected(ref refs.Name) {
	if index, ok := branches.StashIndex(ref); ok {
		a.showStashChanges(index)
	}
}

func (a *App) showStashChanges(index int) {
	o := a.opened()
	if o == nil {
		return
	}
	snap, err := loadBranchSnapshot(o.store)
	if err != nil {
		a.log.Warn("read stashes failed", "error", err)
		return
	}
	at := slices.IndexFunc(snap.Stashes, func(entry branches.Stash) bool { return entry.Index == index })
	if at < 0 {
		return
	}
	entry := snap.Stashes[at]
	a.journalView.ClearSelection()
	a.setSelectedCommit(entry.Commit)
	a.setCommitSelected(true)
	a.setFilesSelected(false)
	a.statusLabel.SetText(i18n.Tf("Status.StashSelected", stashSelector(index), entry.Message))
	a.startFilesDiff(stashFilesLoader(o.repo, index))
	a.showCommitDetails(entry.Commit)
}

func stashFilesLoader(r *gitrepo.Repository, index int) diffFilesLoader {
	return func(ctx context.Context, _ *odb.DB) ([]diff.File, error) {
		shown, err := runStashShow(ctx, r, index, diff.Options{})
		if err != nil {
			return nil, err
		}
		return shown.Files(), nil
	}
}
