package app

import (
	"context"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/gitcore/diff"
	"github.com/oops1/gogit/internal/gitcore/ops"
	"github.com/oops1/gogit/internal/gitcore/refs"
	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/branches"
	"github.com/oops1/gogit/internal/ui/compareview"
)

var newCompareRefsView = compareview.NewView

var readCompare = ops.Compare

func (a *App) registerCompareHandlers() {
	a.handlers[CmdCompareRefs] = func() { a.openCompareRefs("") }
}

func (a *App) compareItems(ref refs.Name) []widget.MenuItem {
	if !ref.IsBranch() && !ref.IsRemote() {
		return nil
	}
	if ref == refs.BranchName(a.currentBranchName()) {
		return nil
	}
	item := menuItem("Menu.Context.CompareWithCurrent", func() { a.openCompareRefs(ref.Short()) })
	item.Disabled = a.State().ActiveRepository == ""
	return []widget.MenuItem{item}
}

func (a *App) openCompareRefs(other string) {
	o := a.opened()
	if o == nil {
		return
	}
	snap, err := loadBranchSnapshot(o.store)
	if err != nil {
		a.log.Warn("read branches for the compare dialog failed", "error", err)
		return
	}
	view, err := newCompareRefsView()
	if err != nil {
		a.log.Warn("open compare dialog failed", "error", err)
		return
	}
	sides := compareSides(snap)
	right := other
	if right == "" {
		right = firstOther(sides, snap.Current)
	}
	view.OnCompare = func(left, other string) { a.compareRefs(view, left, other) }
	view.OnClose = func() { a.eng.CloseModal(view.Dialog()) }
	view.SetSides(sides, snap.Current, right)
	a.showModal(view.Dialog(), view)
	a.compareRefs(view, snap.Current, right)
}

func compareSides(snap branches.Snapshot) []string {
	var sides []string
	for _, branch := range everyBranch(snap) {
		sides = append(sides, branch.Name.Short())
	}
	return sides
}

func firstOther(sides []string, current string) string {
	for _, side := range sides {
		if side != current {
			return side
		}
	}
	return current
}

func (a *App) compareRefs(view *compareview.View, left, right string) {
	var result ops.CompareResult
	a.startRead(func(ctx context.Context, r *gitrepo.Repository) error {
		read, err := readCompare(ctx, r, left, right, ops.CompareOptions{})
		result = read
		return err
	}, func(err error) {
		if err != nil {
			a.log.Warn("compare failed", "left", left, "right", right, "error", err)
			a.statusLabel.SetText(i18n.Tf("Status.CompareFailed", err))
			return
		}
		view.SetSummary(compareSummary(left, right, result))
	})
}

func compareSummary(left, right string, result ops.CompareResult) compareview.Summary {
	summary := compareview.Summary{Left: left, Right: right, Ahead: result.Ahead, Behind: result.Behind, Same: result.Same()}
	for _, file := range result.Changes {
		summary.Changes = append(summary.Changes, compareview.Change{
			Status:  statusName(file.Status),
			Path:    file.NewPath,
			Old:     file.OldPath,
			Added:   file.Added(),
			Deleted: file.Deleted(),
		})
	}
	return summary
}

func statusName(status diff.Status) string {
	switch status {
	case diff.StatusAdded:
		return i18n.T("Files.State.Added")
	case diff.StatusDeleted:
		return i18n.T("Files.State.Deleted")
	case diff.StatusRenamed:
		return i18n.T("Files.State.Renamed")
	}
	return i18n.T("Files.State.Modified")
}
