package app

import (
	"context"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/gitcore/ops"
	"github.com/oops1/gogit/internal/gitcore/refs"
	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/switchbranch"
)

var runDeleteBranch = ops.DeleteBranch

var currentFlowItems = map[string][]menuLeafEntry{
	ops.FlowKindFeature: {
		{Key: "Menu.Branch.GitFlow.IntegrateDevelop", Command: CmdFlowIntegrateDevelop},
		{Key: "Menu.Branch.GitFlow.FinishFeature", Command: CmdFlowFinishFeature},
	},
	ops.FlowKindRelease: {{Key: "Menu.Branch.GitFlow.FinishRelease", Command: CmdFlowFinishRelease}},
	ops.FlowKindHotfix:  {{Key: "Menu.Branch.GitFlow.FinishHotfix", Command: CmdFlowFinishHotfix}},
}

func (a *App) branchMenu(ref refs.Name) []widget.MenuItem {
	if !ref.IsBranch() && !ref.IsRemote() && !ref.IsTag() {
		return nil
	}
	state := a.State()
	current := ref == refs.BranchName(a.currentBranchName())
	items := []widget.MenuItem{
		enabledItem("Menu.Ref.CheckOut", func() { a.checkOutRef(ref) }, !current && state.Enabled(CmdSwitch)),
		menuSeparator(),
	}
	items = append(items, a.integrationItems(ref, current, state)...)
	items = append(items,
		menuSeparator(),
		enabledItem("Menu.Ref.Push", func() { a.Dispatch(CmdPush) }, current && state.Enabled(CmdPush)),
		laterItem("Menu.Ref.PushTo"),
		menuSeparator(),
		laterItem("Menu.Ref.Log"),
	)
	if ref.IsBranch() {
		items = append(items, laterItem("Menu.Ref.Rename"))
	}
	items = append(items,
		menuSeparator(),
		enabledItem("Menu.Ref.Reset", func() { a.resetToRef(ref) }, !ref.IsTag() && state.Enabled(CmdMerge)),
		laterItem("Menu.Ref.ResetAdvanced"),
		a.deleteRefItem(ref, current),
	)
	if ref.IsBranch() {
		items = append(items, menuSeparator(), laterItem("Menu.Ref.SetTracked"), laterItem("Menu.Ref.StopTracking"))
	}
	items = append(items, menuSeparator(), menuItem("Menu.Ref.Copy", func() { a.copyToClipboard(ref.Short()) }))
	if ref.IsTag() {
		items = append(items, laterItem("Menu.Ref.CopyMessage"))
	}
	items = append(items, menuSeparator(), laterItem("Menu.Ref.FormatPatch"), laterItem("Menu.Ref.FastForward"))
	if extras := append(a.compareItems(ref), a.reflogItems(ref)...); len(extras) > 0 {
		items = append(append(items, menuSeparator()), extras...)
	}
	return items
}

func (a *App) integrationItems(ref refs.Name, current bool, state State) []widget.MenuItem {
	if flow, ok := currentFlowItems[state.FlowCurrent.Kind]; ok && current {
		items := make([]widget.MenuItem, 0, len(flow))
		for _, entry := range flow {
			cmd := entry.Command
			items = append(items, enabledItem(entry.Key, func() { a.Dispatch(cmd) }, state.Enabled(cmd)))
		}
		return items
	}
	items := []widget.MenuItem{
		enabledItem("Menu.Ref.Merge", func() { a.openMerge(ref.Short()) }, !current && state.Enabled(CmdMerge)),
	}
	if ref.IsTag() {
		return items
	}
	return append(items, enabledItem("Menu.Ref.RebaseOnto", func() { a.openRebase(ref.Short()) }, !current && state.Enabled(CmdRebase)))
}

func (a *App) deleteRefItem(ref refs.Name, current bool) widget.MenuItem {
	switch {
	case ref.IsTag():
		return menuItem("Menu.Ref.Delete", func() { a.confirmDeleteTag(ref.Short()) })
	case ref.IsBranch() && !current:
		return menuItem("Menu.Ref.Delete", func() { a.confirmDeleteBranch(ref.Short()) })
	}
	return laterItem("Menu.Ref.Delete")
}

func (a *App) checkOutRef(ref refs.Name) {
	if ref == refs.BranchName(a.currentBranchName()) || !a.State().Enabled(CmdSwitch) {
		return
	}
	if ref.IsBranch() {
		a.startSwitch(switchbranch.Choice{Source: ref.Short()})
		return
	}
	a.openSwitch(ref.Short())
}

func (a *App) resetToRef(ref refs.Name) {
	o := a.opened()
	if o == nil {
		return
	}
	resolved, err := o.store.Resolve(ref)
	if err != nil {
		a.log.Warn("resolve branch for reset failed", "ref", ref.String(), "error", err)
		return
	}
	a.openReset(resolved.Target)
}

func (a *App) confirmDeleteTag(name string) {
	a.askConfirm(i18n.T("Dialog.DeleteTag.Title"), i18n.Tf("Dialog.DeleteTag.Message", name), func(ok bool) {
		if ok {
			a.deleteTag(name)
		}
	})
}

func (a *App) confirmDeleteBranch(name string) {
	a.askConfirm(i18n.T("Dialog.DeleteBranch.Title"), i18n.Tf("Dialog.DeleteBranch.Message", name), func(ok bool) {
		if !ok {
			return
		}
		a.startWrite(func(ctx context.Context, r *gitrepo.Repository) error {
			return runDeleteBranch(ctx, r, name, false)
		}, func(err error) {
			if err != nil {
				a.log.Warn("delete branch failed", "branch", name, "error", err)
				a.statusLabel.SetText(i18n.Tf("Status.BranchDeleteFailed", err))
			} else {
				a.statusLabel.SetText(i18n.Tf("Status.BranchDeleted", name))
			}
			a.RefreshRepository()
		})
	})
}
