package app

import (
	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/config"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/branches"
)

const branchesSortRadioGroup = "branches.sort"

var branchSortChoices = []struct {
	Key   string
	Value string
	Mode  branches.SortMode
}{
	{Key: "Menu.Branches.Sort.Name", Value: config.BranchSortName, Mode: branches.SortByName},
	{
		Key:   "Menu.Branches.Sort.ReverseNumbers",
		Value: config.BranchSortNameReverseNumbers,
		Mode:  branches.SortByNameReverseNumbers,
	},
	{Key: "Menu.Branches.Sort.CommitTime", Value: config.BranchSortCommitTime, Mode: branches.SortByCommitTime},
}

var branchGroupToggles = []struct {
	Key   string
	Value func(config.BranchesPane) bool
	Flip  func(*config.BranchesPane)
}{
	{
		Key:   "Menu.Branches.Group.ExceptSingles",
		Value: func(p config.BranchesPane) bool { return p.GroupExceptSingles },
		Flip:  func(p *config.BranchesPane) { p.GroupExceptSingles = !p.GroupExceptSingles },
	},
	{
		Key:   "Menu.Branches.Group.GroupsFirst",
		Value: func(p config.BranchesPane) bool { return p.GroupsFirst },
		Flip:  func(p *config.BranchesPane) { p.GroupsFirst = !p.GroupsFirst },
	},
	{
		Key:   "Menu.Branches.Group.AfterLastSlash",
		Value: func(p config.BranchesPane) bool { return p.GroupAfterLastSlash },
		Flip:  func(p *config.BranchesPane) { p.GroupAfterLastSlash = !p.GroupAfterLastSlash },
	},
}

func branchesPaneOptions(pane config.BranchesPane) branches.Options {
	options := branches.Options{
		FlowSections: pane.FlowSections,
		Grouping: branches.Grouping{
			ByPath:         pane.GroupByPath,
			ExceptSingles:  pane.GroupExceptSingles,
			GroupsFirst:    pane.GroupsFirst,
			AfterLastSlash: pane.GroupAfterLastSlash,
		},
	}
	for _, choice := range branchSortChoices {
		if choice.Value == pane.Sort {
			options.Sort = choice.Mode
		}
	}
	return options
}

func (a *App) wireBranchesPaneButtons() {
	pane := a.Dock().FindPane(paneBranches)
	if pane == nil {
		return
	}
	pane.SetTitleButtons([]widget.DockPaneButton{{
		Tooltip:  i18n.T("Pane.Branches.ViewMenu"),
		MenuFunc: a.branchesPaneMenuItems,
	}})
}

func (a *App) branchesPaneMenuItems() []widget.MenuItem {
	pane := a.cfg.UI.Branches
	items := []widget.MenuItem{
		{Text: i18n.T("Menu.Branches.Sort"), SubItems: a.branchesSortItems(pane)},
		menuSeparator(),
		a.branchesToggleItem("Menu.Branches.FlowSections", pane.FlowSections, true, func(p *config.BranchesPane) {
			p.FlowSections = !p.FlowSections
		}),
		menuSeparator(),
		a.branchesToggleItem("Menu.Branches.Group", pane.GroupByPath, true, func(p *config.BranchesPane) {
			p.GroupByPath = !p.GroupByPath
		}),
	}
	for _, toggle := range branchGroupToggles {
		items = append(items, a.branchesToggleItem(toggle.Key, toggle.Value(pane), pane.GroupByPath, toggle.Flip))
	}
	return append(items,
		menuSeparator(),
		enabledItem("Menu.Branches.SelectObsolete", a.selectObsoleteBranches, a.State().ActiveRepository != ""),
	)
}

func (a *App) branchesSortItems(pane config.BranchesPane) []widget.MenuItem {
	items := make([]widget.MenuItem, 0, len(branchSortChoices))
	for _, choice := range branchSortChoices {
		value := choice.Value
		items = append(items, widget.MenuItem{
			Text:       i18n.T(choice.Key),
			Checkable:  true,
			Checked:    pane.Sort == value,
			RadioGroup: branchesSortRadioGroup,
			OnClick:    func() { a.setBranchesSort(value) },
		})
	}
	return items
}

func (a *App) branchesToggleItem(key string, checked, enabled bool, flip func(*config.BranchesPane)) widget.MenuItem {
	return widget.MenuItem{
		Text:      i18n.T(key),
		Checkable: true,
		Checked:   checked,
		Disabled:  !enabled,
		OnClick:   func() { a.toggleBranchesOption(flip) },
	}
}

func (a *App) setBranchesSort(value string) {
	if a.cfg.UI.Branches.Sort == value {
		return
	}
	a.cfg.UI.Branches.Sort = value
	a.applyBranchesPaneOptions()
	a.saveBranchesPaneOptions()
	if value == config.BranchSortCommitTime {
		a.RefreshRepository()
	}
}

func (a *App) toggleBranchesOption(flip func(*config.BranchesPane)) {
	flip(&a.cfg.UI.Branches)
	a.applyBranchesPaneOptions()
	a.saveBranchesPaneOptions()
}

func (a *App) applyBranchesPaneOptions() {
	a.branchesView.SetOptions(branchesPaneOptions(a.cfg.UI.Branches))
}

func (a *App) saveBranchesPaneOptions() {
	if err := a.cfg.Save(a.paths.ConfigFile()); err != nil {
		a.log.Warn("save config failed", "error", err)
	}
}

func (a *App) selectObsoleteBranches() {
	stale := a.branchesView.SelectObsolete()
	if len(stale) == 0 {
		a.statusLabel.SetText(i18n.T("Status.ObsoleteBranchesNone"))
		return
	}
	a.statusLabel.SetText(i18n.Tf("Status.ObsoleteBranches", len(stale)))
}

func (a *App) enrichBranchSnapshot(o *openedRepository, snap *branches.Snapshot) {
	if o == nil {
		return
	}
	if r, err := a.freshRepo(o); err == nil {
		branches.LoadUpstreams(r.Config(), snap)
		_ = closeGitRepository(r)
	}
	if a.cfg.UI.Branches.Sort == config.BranchSortCommitTime {
		branches.LoadTimes(o.db, snap)
	}
}
