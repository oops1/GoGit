package app

import (
	"github.com/oops1/headless-gui/v3/widget"
	"github.com/oops1/headless-gui/v3/widget/datagrid"

	"github.com/oops1/gogit/internal/i18n"
)

const (
	repositoryMenuIndex = iota
	editMenuIndex
	viewMenuIndex
	remoteMenuIndex
	localMenuIndex
	branchMenuIndex
	queryMenuIndex
	toolsMenuIndex
	windowMenuIndex
	helpMenuIndex
)

type menuLeafEntry struct {
	Key     string
	Command CommandID
}

type menuGroupEntry struct {
	Key   string
	Items []menuLeafEntry
}

type menuTreeEntry struct {
	Separator bool
	Leaf      *menuLeafEntry
	Group     *menuGroupEntry
}

type menuDef struct {
	TitleKey string
	Tree     []menuTreeEntry
	LeafText func(a *App, leaf menuLeafEntry) string
}

var repositoryMenuTree = buildRepositoryMenuTree()
var editMenuTree = buildEditMenuTree()
var viewMenuTree = buildViewMenuTree()
var remoteMenuTree = buildRemoteMenuTree()
var localMenuTree = buildLocalMenuTree()
var branchMenuTree = buildBranchMenuTree()
var queryMenuTree = buildQueryMenuTree()
var toolsMenuTree = buildToolsMenuTree()
var windowMenuTree = buildWindowMenuTree()
var helpMenuTree = buildHelpMenuTree()

var menuBarDefs = []menuDef{
	{TitleKey: "Menu.Repository", Tree: repositoryMenuTree, LeafText: plainLeafText},
	{TitleKey: "Menu.Edit", Tree: editMenuTree, LeafText: plainLeafText},
	{TitleKey: "Menu.View", Tree: viewMenuTree, LeafText: (*App).checkableLeafText},
	{TitleKey: "Menu.Remote", Tree: remoteMenuTree, LeafText: plainLeafText},
	{TitleKey: "Menu.Local", Tree: localMenuTree, LeafText: plainLeafText},
	{TitleKey: "Menu.Branch", Tree: branchMenuTree, LeafText: plainLeafText},
	{TitleKey: "Menu.Query", Tree: queryMenuTree, LeafText: plainLeafText},
	{TitleKey: "Menu.Tools", Tree: toolsMenuTree, LeafText: plainLeafText},
	{TitleKey: "Menu.Window", Tree: windowMenuTree, LeafText: (*App).checkableLeafText},
	{TitleKey: "Menu.Help", Tree: helpMenuTree, LeafText: plainLeafText},
}

func menuLeaf(key string, cmd CommandID) menuTreeEntry {
	return menuTreeEntry{Leaf: &menuLeafEntry{Key: key, Command: cmd}}
}

var menuSeparatorEntry = menuTreeEntry{Separator: true}

func buildRepositoryMenuTree() []menuTreeEntry {
	return []menuTreeEntry{
		menuLeaf("Menu.Repository.AddOrCreate", CmdAddOrCreate),
		menuLeaf("Menu.Repository.AddGroup", CmdAddGroup),
		menuLeaf("Menu.Repository.Clone", CmdClone),
		menuLeaf("Menu.Repository.Search", CmdSearch),
		menuLeaf("Menu.Repository.CloseRepository", CmdCloseRepository),
		menuSeparatorEntry,
		menuLeaf("Menu.Repository.AddWorktree", CmdAddWorktree),
		menuLeaf("Menu.Repository.RemoveWorktree", CmdRemoveWorktree),
		menuLeaf("Menu.Repository.PruneWorktrees", CmdPruneWorktrees),
		menuSeparatorEntry,
		menuLeaf("Menu.Repository.SparseCheckout", CmdSparseCheckout),
		menuSeparatorEntry,
		menuLeaf("Menu.Repository.RepoSettings", CmdRepoSettings),
		menuLeaf("Menu.Repository.Close", CmdClose),
	}
}

func buildRemoteMenuTree() []menuTreeEntry {
	return []menuTreeEntry{
		menuLeaf("Menu.Remote.Fetch", CmdFetch),
		menuLeaf("Menu.Remote.Pull", CmdPull),
		menuLeaf("Menu.Remote.Push", CmdPush),
		menuLeaf("Menu.Remote.Sync", CmdSync),
		menuSeparatorEntry,
		menuLeaf("Menu.Remote.Prune", CmdPrune),
		menuSeparatorEntry,
		menuLeaf("Menu.Remote.Manage", CmdManageRemotes),
		menuSeparatorEntry,
		{Group: &menuGroupEntry{Key: "Menu.Remote.Submodule", Items: submoduleMenuLeaves}},
	}
}

var submoduleMenuLeaves = []menuLeafEntry{
	{Key: "Menu.Remote.Submodule.Update", Command: CmdSubmoduleUpdate},
	{Key: "Menu.Remote.Submodule.Initialize", Command: CmdSubmoduleInitialize},
	{Key: "Menu.Remote.Submodule.Synchronize", Command: CmdSubmoduleSync},
	{},
	{Key: "Menu.Remote.Submodule.Add", Command: CmdSubmoduleAdd},
	{Key: "Menu.Remote.Submodule.Remove", Command: CmdSubmoduleRemove},
	{Key: "Menu.Remote.Submodule.Unregister", Command: CmdSubmoduleUnregister},
	{Key: "Menu.Remote.Submodule.Reset", Command: CmdSubmoduleReset},
}

func buildEditMenuTree() []menuTreeEntry {
	return []menuTreeEntry{
		menuLeaf("Menu.Edit.Copy", CmdCopy),
		menuLeaf("Menu.Edit.SelectAll", CmdSelectAll),
		menuSeparatorEntry,
		menuLeaf("Menu.Edit.Preferences", CmdSettings),
	}
}

func buildLocalMenuTree() []menuTreeEntry {
	return []menuTreeEntry{
		menuLeaf("Menu.Local.Commit", CmdCommit),
		menuSeparatorEntry,
		menuLeaf("Menu.Local.Stage", CmdStage),
		menuLeaf("Menu.Local.Unstage", CmdUnstage),
		menuLeaf("Menu.Local.Discard", CmdDiscard),
		menuLeaf("Menu.Files.IndexEditor", CmdIndexEditor),
		menuSeparatorEntry,
		menuLeaf("Menu.Local.SaveStash", CmdStashSave),
		menuLeaf("Menu.Local.ApplyStash", CmdStashApply),
		menuLeaf("Menu.Local.DropStash", CmdStashDrop),
		menuSeparatorEntry,
		menuLeaf("Menu.Files.Ignore", CmdIgnore),
		menuLeaf("Menu.Files.Remove", CmdRemove),
	}
}

func buildBranchMenuTree() []menuTreeEntry {
	return []menuTreeEntry{
		menuLeaf("Menu.Branch.Switch", CmdSwitch),
		menuSeparatorEntry,
		menuLeaf("Menu.Branch.Merge", CmdMerge),
		menuLeaf("Menu.Branch.Rebase", CmdRebase),
		menuLeaf("Menu.Branch.RebaseInteractive", CmdRebaseSteps),
		menuSeparatorEntry,
		menuLeaf("Menu.Branch.Continue", CmdContinue),
		menuLeaf("Menu.Branch.Skip", CmdSkip),
		menuLeaf("Menu.Branch.AbortMerge", CmdAbortMerge),
	}
}

func buildQueryMenuTree() []menuTreeEntry {
	return []menuTreeEntry{
		menuLeaf("Menu.Query.Log", CmdLog),
		menuLeaf("Menu.Context.Blame", CmdBlame),
		menuLeaf("Menu.Files.Investigate", CmdInvestigate),
		menuSeparatorEntry,
		menuLeaf("Menu.Query.CompareFiles", CmdCompareFiles),
		menuLeaf("Menu.Query.CompareBranches", CmdCompareRefs),
		menuSeparatorEntry,
		menuLeaf("Menu.Query.Reflog", CmdReflog),
	}
}

var maintenanceMenuLeaves = []menuLeafEntry{
	{Key: "Menu.Tools.Maintenance.Gc", Command: CmdGc},
	{Key: "Menu.Tools.Maintenance.Fsck", Command: CmdFsck},
}

func buildToolsMenuTree() []menuTreeEntry {
	return []menuTreeEntry{
		{Group: &menuGroupEntry{Key: "Menu.Tools.GitFlow", Items: flowMenuLeaves}},
		menuSeparatorEntry,
		{Group: &menuGroupEntry{Key: "Menu.Tools.Maintenance", Items: maintenanceMenuLeaves}},
		menuSeparatorEntry,
		menuLeaf("Menu.Context.Reveal", CmdRevealRepository),
		menuLeaf("Menu.Context.Terminal", CmdOpenTerminal),
	}
}

func buildViewMenuTree() []menuTreeEntry {
	theme := buildCheckableLeafGroup("Menu.View.Theme", viewThemeOrder, viewThemeKeys, cmdTheme)
	language := buildCheckableLeafGroup("Menu.View.Language", viewLanguageOrder, nil, cmdLanguage)
	return []menuTreeEntry{
		{Group: &theme},
		{Group: &language},
		menuSeparatorEntry,
		menuLeaf("Menu.View.Refresh", CmdRefresh),
	}
}

func buildWindowMenuTree() []menuTreeEntry {
	panes := buildCheckableLeafGroup("Menu.Window.Panes", viewPaneIDs, viewPaneKeys, cmdPane)
	layout := buildCheckableLeafGroup("Menu.Window.Layout", layoutModeOrder, layoutModeKeys, cmdLayout)
	return []menuTreeEntry{
		{Group: &panes},
		menuLeaf("Menu.Window.ResetLayout", CmdResetLayout),
		menuSeparatorEntry,
		{Group: &layout},
	}
}

func buildHelpMenuTree() []menuTreeEntry {
	return []menuTreeEntry{
		menuLeaf("Menu.Help.CheckUpdates", CmdCheckUpdates),
		menuSeparatorEntry,
		menuLeaf("Menu.Help.About", CmdAbout),
	}
}

func buildCheckableLeafGroup(headerKey string, ids []string, keys map[string]string, cmd func(string) CommandID) menuGroupEntry {
	items := make([]menuLeafEntry, 0, len(ids))
	for _, id := range ids {
		key := keys[id]
		if key == "" {
			key = languageKey(id)
		}
		items = append(items, menuLeafEntry{Key: key, Command: cmd(id)})
	}
	return menuGroupEntry{Key: headerKey, Items: items}
}

func plainLeafText(_ *App, leaf menuLeafEntry) string {
	return i18n.T(leaf.Key)
}

func (a *App) wireMenuBar() {
	items := a.menu.Items()
	for i, def := range menuBarDefs {
		if i >= len(items) {
			return
		}
		wireMenuTreeEntries(items[i].Items, def.Tree, a.Dispatch)
	}
}

func wireMenuTreeEntries(subs []widget.MenuItem, tree []menuTreeEntry, dispatch func(CommandID) bool) {
	for i, entry := range tree {
		if i >= len(subs) {
			return
		}
		wireMenuTreeEntry(&subs[i], entry, dispatch)
	}
}

func wireMenuTreeEntry(item *widget.MenuItem, entry menuTreeEntry, dispatch func(CommandID) bool) {
	switch {
	case entry.Leaf != nil:
		cmd := entry.Leaf.Command
		item.OnClick = func() { dispatch(cmd) }
	case entry.Group != nil:
		for i := range min(len(entry.Group.Items), len(item.SubItems)) {
			cmd := entry.Group.Items[i].Command
			if cmd == "" {
				continue
			}
			item.SubItems[i].OnClick = func() { dispatch(cmd) }
		}
	}
}

func (a *App) wireCheckableHandlers() {
	for _, id := range viewPaneIDs {
		paneID := id
		a.handlers[cmdPane(paneID)] = func() {
			a.SetPaneVisible(paneID, !a.PaneVisible(paneID))
			a.applyMenuTexts(windowMenuIndex)
		}
	}
	for _, name := range layoutModeOrder {
		mode := name
		a.handlers[cmdLayout(mode)] = func() {
			a.SetLayout(mode)
			a.applyMenuTexts(windowMenuIndex)
		}
	}
	for _, name := range viewThemeOrder {
		theme := name
		a.handlers[cmdTheme(theme)] = func() {
			a.SetTheme(theme)
			a.applyMenuTexts(viewMenuIndex)
		}
	}
	for _, code := range viewLanguageOrder {
		lang := code
		a.handlers[cmdLanguage(lang)] = func() { a.SetLanguage(lang) }
	}
}

func (a *App) wireHotkeys() {
	a.root.InputBindings = append(a.root.InputBindings, widget.InputBinding{
		Key:     widget.KeyF5,
		Command: widget.NewRelayCommand(func() { a.Dispatch(CmdRefresh) }),
	}, widget.InputBinding{
		Key:     widget.KeyEscape,
		Command: widget.NewRelayCommand(a.leaveCommitView),
	})
}

func (a *App) wireToolbar() {
	a.buildToolbar()
}

func (a *App) refreshCommands() {
	state := a.State()
	items := a.menu.Items()
	for i, def := range menuBarDefs {
		if i >= len(items) {
			continue
		}
		applyTreeEnabled(items[i].Items, def.Tree, state)
	}
	a.refreshToolbarButtons(state)
	a.applyMenuIcons()
}

func applyTreeEnabled(subs []widget.MenuItem, tree []menuTreeEntry, state State) {
	for i := range min(len(subs), len(tree)) {
		entry := tree[i]
		switch {
		case entry.Leaf != nil:
			subs[i].Disabled = !state.Enabled(entry.Leaf.Command)
		case entry.Group != nil:
			for j := range min(len(entry.Group.Items), len(subs[i].SubItems)) {
				subs[i].SubItems[j].Disabled = !state.Enabled(entry.Group.Items[j].Command)
			}
		}
	}
}

func (a *App) retranslate() {
	for i, def := range menuBarDefs {
		a.menu.SetMenuText(i, i18n.T(def.TitleKey))
		a.applyMenuTexts(i)
	}
	a.retranslateGrids()
	a.retranslateFilesStatusButtons()
	a.retranslateRepoTrees()
	a.retranslateFilesState()
	a.retranslateToolbar()
	a.root.Title = i18n.T("App.Title")
	a.updateStatusText()
	a.applyFilesFilter()
}

func (a *App) retranslateRepoTrees() {
	a.reposView.Render(a.registry, a.repoTreeState())
	o := a.opened()
	if o == nil {
		return
	}
	snap, err := loadBranchSnapshot(o.store)
	if err != nil {
		a.log.Warn("retranslate branches failed", "path", o.path, "error", err)
		return
	}
	a.branchesView.Render(snap)
	a.showJournalBranches(snap)
	a.statusBranchLabel.SetText(a.branchStatusTextWithDivergence(snap))
}

func (a *App) retranslateFilesState() {
	if a.opened() == nil || a.commitIsSelected() {
		return
	}
	a.requestWorking()
}

func (a *App) applyMenuTexts(idx int) {
	if idx < 0 || idx >= len(menuBarDefs) {
		return
	}
	items := a.menu.Items()
	if idx >= len(items) {
		return
	}
	def := menuBarDefs[idx]
	applyTreeTexts(a, items[idx].Items, def.Tree, def.LeafText)
}

func applyTreeTexts(a *App, subs []widget.MenuItem, tree []menuTreeEntry, leafText func(*App, menuLeafEntry) string) {
	for i, entry := range tree {
		if i >= len(subs) {
			return
		}
		applyTreeEntryText(a, &subs[i], entry, leafText)
	}
}

func applyTreeEntryText(a *App, item *widget.MenuItem, entry menuTreeEntry, leafText func(*App, menuLeafEntry) string) {
	switch {
	case entry.Leaf != nil:
		item.Text = leafText(a, *entry.Leaf)
	case entry.Group != nil:
		item.Text = i18n.T(entry.Group.Key)
		for i := range min(len(entry.Group.Items), len(item.SubItems)) {
			item.SubItems[i].Text = leafText(a, entry.Group.Items[i])
		}
	}
}

func (a *App) checkableLeafText(leaf menuLeafEntry) string {
	label, checked := a.checkableLeafLabel(leaf)
	if checked {
		return checkedPrefix + label
	}
	return label
}

func (a *App) checkableLeafLabel(leaf menuLeafEntry) (string, bool) {
	if paneID, ok := paneIDFromCommand(leaf.Command); ok {
		return i18n.T(leaf.Key), a.PaneVisible(paneID)
	}
	if theme, ok := themeFromCommand(leaf.Command); ok {
		return i18n.T(leaf.Key), a.cfg.Theme == theme
	}
	if code, ok := languageFromCommand(leaf.Command); ok {
		return i18n.T(leaf.Key), i18n.Current() == code
	}
	if mode, ok := layoutFromCommand(leaf.Command); ok {
		return i18n.T(leaf.Key), a.cfg.UI.Layout == mode
	}
	return i18n.T(leaf.Key), false
}

func (a *App) logLanguageMenuLimit() {
	if len(a.languages) <= len(viewLanguageOrder) {
		return
	}
	a.log.Debug("view menu shows only built-in languages", "builtin", viewLanguageOrder, "installed", a.languages)
}

func (a *App) retranslateGrids() {
	for name, keys := range gridColumnKeys {
		columns := a.named[name].(*widget.DataGridWidget).Grid.Columns()
		for i, key := range keys {
			if i >= len(columns) {
				continue
			}
			if key == "" {
				columns[i].SetHeader("")
				continue
			}
			columns[i].SetHeader(i18n.T(key))
		}
	}
	a.filesGrid.Retranslate()
	a.journalView.SetFullAuthorName(a.cfg.UI.JournalFullAuthorName)
}

func (a *App) ColumnHeaders(grid string) []string {
	if grid == "filesGrid" {
		return columnHeaders(a.filesGrid.Data().Grid.Columns())
	}
	return columnHeaders(a.named[grid].(*widget.DataGridWidget).Grid.Columns())
}

func columnHeaders(columns []datagrid.Column) []string {
	headers := make([]string, 0, len(columns))
	for _, c := range columns {
		headers = append(headers, c.Header())
	}
	return headers
}

func (a *App) MenuItemByCommand(id CommandID) (text string, enabled bool, ok bool) {
	items := a.menu.Items()
	for i, def := range menuBarDefs {
		if i >= len(items) {
			continue
		}
		if text, enabled, ok = findTreeItem(items[i].Items, def.Tree, id); ok {
			return text, enabled, true
		}
	}
	return "", false, false
}

func findTreeItem(subs []widget.MenuItem, tree []menuTreeEntry, id CommandID) (text string, enabled bool, ok bool) {
	for i, entry := range tree {
		if i >= len(subs) {
			return "", false, false
		}
		switch {
		case entry.Leaf != nil:
			if entry.Leaf.Command == id {
				return subs[i].Text, !subs[i].Disabled, true
			}
		case entry.Group != nil:
			for j, leaf := range entry.Group.Items {
				if leaf.Command != id || j >= len(subs[i].SubItems) {
					continue
				}
				sub := subs[i].SubItems[j]
				return sub.Text, !sub.Disabled, true
			}
		}
	}
	return "", false, false
}
