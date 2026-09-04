package app

import (
	"github.com/oops1/headless-gui/v3/widget"
	"github.com/oops1/headless-gui/v3/widget/datagrid"

	"github.com/oops1/gogit/internal/i18n"
)

const repositoryMenuIndex = 0
const editMenuIndex = 1
const viewMenuIndex = 2

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

var menuBarDefs = []menuDef{
	{TitleKey: "Menu.Repository", Tree: repositoryMenuTree, LeafText: plainLeafText},
	{TitleKey: "Menu.Edit", Tree: editMenuTree, LeafText: plainLeafText},
	{TitleKey: "Menu.View", Tree: viewMenuTree, LeafText: (*App).viewLeafText},
}

func buildRepositoryMenuTree() []menuTreeEntry {
	leaf := func(key string, cmd CommandID) menuTreeEntry {
		return menuTreeEntry{Leaf: &menuLeafEntry{Key: key, Command: cmd}}
	}
	separator := menuTreeEntry{Separator: true}
	return []menuTreeEntry{
		leaf("Menu.Repository.AddOrCreate", CmdAddOrCreate),
		leaf("Menu.Repository.AddGroup", CmdAddGroup),
		leaf("Menu.Repository.Search", CmdSearch),
		leaf("Menu.Repository.CloseRepository", CmdCloseRepository),
		separator,
		leaf("Menu.Repository.AddWorktree", CmdAddWorktree),
		leaf("Menu.Repository.RemoveWorktree", CmdRemoveWorktree),
		leaf("Menu.Repository.PruneWorktrees", CmdPruneWorktrees),
		separator,
		leaf("Menu.Repository.Settings", CmdSettings),
		leaf("Menu.Repository.Close", CmdClose),
	}
}

func buildEditMenuTree() []menuTreeEntry {
	leaf := func(key string, cmd CommandID) menuTreeEntry {
		return menuTreeEntry{Leaf: &menuLeafEntry{Key: key, Command: cmd}}
	}
	return []menuTreeEntry{
		leaf("Menu.Edit.Stage", CmdStage),
		leaf("Menu.Edit.Unstage", CmdUnstage),
		leaf("Menu.Edit.Discard", CmdDiscard),
		{Separator: true},
		leaf("Menu.Edit.Commit", CmdCommit),
	}
}

func buildViewMenuTree() []menuTreeEntry {
	panes := buildViewLeafGroup("Menu.View.Panes", viewPaneIDs, viewPaneKeys, cmdPane)
	theme := buildViewLeafGroup("Menu.View.Theme", viewThemeOrder, viewThemeKeys, cmdTheme)
	language := buildViewLeafGroup("Menu.View.Language", viewLanguageOrder, nil, cmdLanguage)
	return []menuTreeEntry{
		{Group: &panes},
		{Leaf: &menuLeafEntry{Key: "Menu.View.ResetLayout", Command: CmdResetLayout}},
		{Separator: true},
		{Group: &theme},
		{Group: &language},
		{Separator: true},
		{Leaf: &menuLeafEntry{Key: "Menu.View.Refresh", Command: CmdRefresh}},
	}
}

func buildViewLeafGroup(headerKey string, ids []string, keys map[string]string, cmd func(string) CommandID) menuGroupEntry {
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
			item.SubItems[i].OnClick = func() { dispatch(cmd) }
		}
	}
}

func (a *App) wireViewHandlers() {
	for _, id := range viewPaneIDs {
		paneID := id
		a.handlers[cmdPane(paneID)] = func() {
			a.SetPaneVisible(paneID, !a.PaneVisible(paneID))
			a.applyMenuTexts(viewMenuIndex)
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
	})
}

func (a *App) wireToolbar() {
	for id, name := range toolbarButtons {
		btn := a.named[name].(*widget.Button)
		cmd := id
		btn.OnClick = func() { a.Dispatch(cmd) }
	}
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
	for id, name := range toolbarButtons {
		a.named[name].(*widget.Button).SetEnabled(state.Enabled(id))
	}
}

func applyTreeEnabled(subs []widget.MenuItem, tree []menuTreeEntry, state State) {
	for i, entry := range tree {
		if i >= len(subs) || entry.Leaf == nil {
			continue
		}
		subs[i].Disabled = !state.Enabled(entry.Leaf.Command)
	}
}

func (a *App) retranslate() {
	for i, def := range menuBarDefs {
		a.menu.SetMenuText(i, i18n.T(def.TitleKey))
		a.applyMenuTexts(i)
	}
	a.retranslateGrids()
	a.retranslateFilesStatusButtons()
	a.root.Title = i18n.T("App.Title")
	a.updateStatusText()
	a.applyFilesFilter()
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

func (a *App) viewLeafText(leaf menuLeafEntry) string {
	label, checked := a.viewLeafLabelChecked(leaf)
	if checked {
		return checkedPrefix + label
	}
	return label
}

func (a *App) viewLeafLabelChecked(leaf menuLeafEntry) (string, bool) {
	if paneID, ok := paneIDFromCommand(leaf.Command); ok {
		return i18n.T(leaf.Key), a.PaneVisible(paneID)
	}
	if theme, ok := themeFromCommand(leaf.Command); ok {
		return i18n.T(leaf.Key), a.cfg.Theme == theme
	}
	if code, ok := languageFromCommand(leaf.Command); ok {
		return i18n.T(leaf.Key), i18n.Current() == code
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
			if i < len(columns) {
				columns[i].SetHeader(i18n.T(key))
			}
		}
	}
	a.filesGrid.Retranslate()
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
