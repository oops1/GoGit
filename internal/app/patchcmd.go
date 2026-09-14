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
	"github.com/oops1/gogit/internal/ui/gitdiff"
)

type diffKind int

const (
	diffKindNone diffKind = iota
	diffKindWorktree
	diffKindIndex
)

type diffTarget struct {
	kind    diffKind
	path    string
	file    diff.File
	oldData []byte
	newData []byte
}

type patchFunc func(context.Context, *gitrepo.Repository, string, []diff.Hunk) error

var patchIndex patchFunc = ops.PatchIndex

var patchWorkingTree patchFunc = ops.PatchWorkingTree

func (a *App) showDiff(target diffTarget) {
	a.shownMu.Lock()
	a.shownDiff = target
	a.shownMu.Unlock()
	a.diffView.Show(diffSides(target))
}

func diffSides(target diffTarget) (gitdiff.Side, gitdiff.Side) {
	left := gitdiff.Side{Title: target.file.OldPath, Text: string(target.oldData)}
	right := gitdiff.Side{Title: target.file.NewPath, Text: string(target.newData)}
	if target.file.Binary {
		left.Text = i18n.T("Diff.Binary")
		right.Text = left.Text
	}
	return left, right
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

func (a *App) diffMenu(spot gitdiff.Spot) []widget.MenuItem {
	return a.diffMenuFor(a.currentShownDiff(), spot)
}

func (a *App) diffMenuFor(target diffTarget, spot gitdiff.Spot) []widget.MenuItem {
	lines, hunks := locateDiffLines(target.file, spot)
	onLines, onHunks := len(lines) > 0, len(hunks) > 0
	byLines, byHunks := pickLines(lines), pickHunks(hunks)
	switch target.kind {
	case diffKindWorktree:
		return []widget.MenuItem{
			patchItem("Menu.Diff.StageLines", onLines, func() { a.stageLines(target, byLines) }),
			patchItem("Menu.Diff.StageHunk", onHunks, func() { a.stageLines(target, byHunks) }),
			menuSeparator(),
			patchItem("Menu.Diff.DiscardLines", onLines, func() { a.discardLines(target, byLines) }),
			patchItem("Menu.Diff.DiscardHunk", onHunks, func() { a.discardLines(target, byHunks) }),
		}
	case diffKindIndex:
		return []widget.MenuItem{
			patchItem("Menu.Diff.UnstageLines", onLines, func() { a.unstageLines(target, byLines) }),
			patchItem("Menu.Diff.UnstageHunk", onHunks, func() { a.unstageLines(target, byHunks) }),
		}
	}
	return nil
}

func locateDiffLines(f diff.File, spot gitdiff.Spot) (map[[2]int]bool, map[int]bool) {
	left := spot.Side == widget.DiffLeft
	lines, hunks := map[[2]int]bool{}, map[int]bool{}
	for hunk, h := range f.Hunks {
		oldAt, newAt := h.OldStart-1, h.NewStart-1
		for line, l := range h.Lines {
			at := newAt
			if left {
				at = oldAt
			}
			shown := l.Kind == diff.KindContext || (l.Kind == diff.KindDel) == left
			if shown && at >= spot.From && at < spot.To {
				hunks[hunk] = true
				if l.Kind != diff.KindContext {
					lines[[2]int{hunk, line}] = true
				}
			}
			if l.Kind != diff.KindAdd {
				oldAt++
			}
			if l.Kind != diff.KindDel {
				newAt++
			}
		}
	}
	return lines, hunks
}

func pickHunks(hunks map[int]bool) patch.Picked {
	return func(h, _ int) bool { return hunks[h] }
}

func pickLines(lines map[[2]int]bool) patch.Picked {
	return func(h, l int) bool { return lines[[2]int{h, l}] }
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
