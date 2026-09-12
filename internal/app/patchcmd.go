package app

import (
	"context"
	"errors"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/gitcore/diff"
	"github.com/oops1/gogit/internal/gitcore/ops"
	"github.com/oops1/gogit/internal/gitcore/patch"
	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/gitcore/worktree"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/changes"
	"github.com/oops1/gogit/internal/ui/diffview"
)

type diffKind int

const (
	diffKindNone diffKind = iota
	diffKindWorktree
	diffKindIndex
)

type diffTarget struct {
	kind diffKind
	path string
	file diff.File
}

type patchFunc func(context.Context, *gitrepo.Repository, string, []diff.Hunk) error

var patchIndex patchFunc = ops.PatchIndex

var patchWorkingTree patchFunc = ops.PatchWorkingTree

func (a *App) showDiff(target diffTarget) {
	a.shownMu.Lock()
	a.shownDiff = target
	a.shownMu.Unlock()
	a.diffView.SetDocument(changes.FromFile(target.file))
}

func (a *App) clearDiff() {
	a.shownMu.Lock()
	a.shownDiff = diffTarget{}
	a.shownMu.Unlock()
	a.diffView.Clear()
}

func (a *App) currentShownDiff() diffTarget {
	a.shownMu.Lock()
	defer a.shownMu.Unlock()
	return a.shownDiff
}

func (a *App) diffMenu() []widget.MenuItem {
	return a.diffMenuFor(a.currentShownDiff(), a.diffView.SelectedLines(), a.diffView.SelectedHunks())
}

func (a *App) diffMenuFor(target diffTarget, lines []diffview.LineRef, hunks []int) []widget.MenuItem {
	byLines := changes.Picked(target.file, lines)
	byHunks := changes.PickedHunks(hunks)
	switch target.kind {
	case diffKindWorktree:
		return []widget.MenuItem{
			patchItem("Menu.Diff.StageLines", len(lines) > 0, func() { a.stageLines(target, byLines) }),
			patchItem("Menu.Diff.StageHunk", len(hunks) > 0, func() { a.stageLines(target, byHunks) }),
			menuSeparator(),
			patchItem("Menu.Diff.DiscardLines", len(lines) > 0, func() { a.discardLines(target, byLines) }),
			patchItem("Menu.Diff.DiscardHunk", len(hunks) > 0, func() { a.discardLines(target, byHunks) }),
		}
	case diffKindIndex:
		return []widget.MenuItem{
			patchItem("Menu.Diff.UnstageLines", len(lines) > 0, func() { a.unstageLines(target, byLines) }),
			patchItem("Menu.Diff.UnstageHunk", len(hunks) > 0, func() { a.unstageLines(target, byHunks) }),
		}
	}
	return nil
}

func patchItem(key string, enabled bool, run func()) widget.MenuItem {
	item := menuItem(key, run)
	item.Disabled = !enabled
	return item
}

func (a *App) stageLines(target diffTarget, pick patch.Picked) {
	hunks, err := patch.Select(target.file.Hunks, pick)
	a.applyPatch(target, hunks, err, patchIndex)
}

func (a *App) unstageLines(target diffTarget, pick patch.Picked) {
	hunks, err := patch.Select(patch.Reverse(target.file.Hunks), pick)
	a.applyPatch(target, hunks, err, patchIndex)
}

func (a *App) discardLines(target diffTarget, pick patch.Picked) {
	hunks, err := patch.Select(patch.Reverse(target.file.Hunks), pick)
	if err != nil {
		a.reportPatch(err)
		return
	}
	a.askConfirm(i18n.T("Dialog.DiscardLines.Title"), i18n.Tf("Dialog.DiscardLines.Message", target.path), func(ok bool) {
		if ok {
			a.applyPatch(target, hunks, nil, patchWorkingTree)
		}
	})
}

func (a *App) applyPatch(target diffTarget, hunks []diff.Hunk, err error, apply patchFunc) {
	if err != nil {
		a.reportPatch(err)
		return
	}
	a.startWrite(func(ctx context.Context, r *gitrepo.Repository) error {
		return apply(ctx, r, target.path, hunks)
	}, func(err error) {
		if err != nil {
			a.reportPatch(err)
			return
		}
		a.redrawDiff(target)
	})
}

func (a *App) reportPatch(err error) {
	a.log.Warn("patch lines failed", "error", err)
	if errors.Is(err, patch.ErrUnsplittable) {
		a.statusLabel.SetText(i18n.T("Status.PatchUnsplittable"))
		return
	}
	a.statusLabel.SetText(i18n.Tf("Status.PatchFailed", err))
}

func (a *App) redrawDiff(target diffTarget) {
	entry := worktree.Entry{Path: target.path, Unstaged: worktree.StatusUnmodified}
	if target.kind == diffKindWorktree {
		entry.Unstaged = worktree.StatusModified
	}
	a.showWorkingDiff(entry)
}
