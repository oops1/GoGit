package app

import (
	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/i18n"
)

const repositoryMenuIndex = 0
const viewMenuIndex = 1

type viewLeafEntry struct {
	Key     string
	Command CommandID
}

type viewGroupEntry struct {
	Key   string
	Items []viewLeafEntry
}

type viewTreeEntry struct {
	Separator bool
	Leaf      *viewLeafEntry
	Group     *viewGroupEntry
}

var viewMenuTree = buildViewMenuTree()

func buildViewMenuTree() []viewTreeEntry {
	panes := buildViewLeafGroup("Menu.View.Panes", viewPaneIDs, viewPaneKeys, cmdPane)
	theme := buildViewLeafGroup("Menu.View.Theme", viewThemeOrder, viewThemeKeys, cmdTheme)
	language := buildViewLeafGroup("Menu.View.Language", viewLanguageOrder, nil, cmdLanguage)
	return []viewTreeEntry{
		{Group: &panes},
		{Leaf: &viewLeafEntry{Key: "Menu.View.ResetLayout", Command: CmdResetLayout}},
		{Separator: true},
		{Group: &theme},
		{Group: &language},
		{Separator: true},
		{Leaf: &viewLeafEntry{Key: "Menu.View.Refresh", Command: CmdRefresh}},
	}
}

func buildViewLeafGroup(headerKey string, ids []string, keys map[string]string, cmd func(string) CommandID) viewGroupEntry {
	items := make([]viewLeafEntry, 0, len(ids))
	for _, id := range ids {
		key := keys[id]
		if key == "" {
			key = languageKey(id)
		}
		items = append(items, viewLeafEntry{Key: key, Command: cmd(id)})
	}
	return viewGroupEntry{Key: headerKey, Items: items}
}

func (a *App) wireMenu() {
	a.menu.OnSelect = func(top, sub int, _ string) {
		if id, ok := a.menuCommand(top, sub); ok {
			a.Dispatch(id)
		}
	}
}

func (a *App) menuCommand(top, sub int) (CommandID, bool) {
	if top != repositoryMenuIndex {
		return "", false
	}
	return lookupCommand(repositoryMenu, sub)
}

func lookupCommand(ids []CommandID, sub int) (CommandID, bool) {
	if sub < 0 || sub >= len(ids) {
		return "", false
	}
	id := ids[sub]
	return id, id != cmdSeparator
}

func (a *App) wireViewMenu() {
	subs := a.viewMenuItems()
	for i, entry := range viewMenuTree {
		if i >= len(subs) {
			return
		}
		wireViewTreeEntry(&subs[i], entry, a.Dispatch)
	}
}

func wireViewTreeEntry(item *widget.MenuItem, entry viewTreeEntry, dispatch func(CommandID) bool) {
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
			a.applyViewTexts()
		}
	}
	for _, name := range viewThemeOrder {
		theme := name
		a.handlers[cmdTheme(theme)] = func() {
			a.SetTheme(theme)
			a.applyViewTexts()
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
	applyMenuDisabled(items, repositoryMenuIndex, repositoryMenu, state)
	a.applyViewEnabled(items, state)
	for id, name := range toolbarButtons {
		a.named[name].(*widget.Button).SetEnabled(state.Enabled(id))
	}
}

func applyMenuDisabled(items []widget.MenuBarItem, topIdx int, ids []CommandID, state State) {
	if len(items) <= topIdx {
		return
	}
	subs := items[topIdx].Items
	for i, id := range ids {
		if id == cmdSeparator || i >= len(subs) {
			continue
		}
		subs[i].Disabled = !state.Enabled(id)
	}
}

func (a *App) applyViewEnabled(items []widget.MenuBarItem, state State) {
	if len(items) <= viewMenuIndex {
		return
	}
	subs := items[viewMenuIndex].Items
	for i, entry := range viewMenuTree {
		if i >= len(subs) || entry.Leaf == nil {
			continue
		}
		subs[i].Disabled = !state.Enabled(entry.Leaf.Command)
	}
}

func (a *App) retranslate() {
	a.menu.SetMenuText(repositoryMenuIndex, i18n.T("Menu.Repository"))
	for i, id := range repositoryMenu {
		if key, ok := menuKeys[id]; ok {
			a.menu.SetSubItemText(repositoryMenuIndex, i, i18n.T(key))
		}
	}
	a.menu.SetMenuText(viewMenuIndex, i18n.T("Menu.View"))
	a.applyViewTexts()
	a.retranslateGrids()
	a.root.Title = i18n.T("App.Title")
	a.updateStatusText()
}

func (a *App) viewMenuItems() []widget.MenuItem {
	items := a.menu.Items()
	if len(items) <= viewMenuIndex {
		return nil
	}
	return items[viewMenuIndex].Items
}

func (a *App) applyViewTexts() {
	subs := a.viewMenuItems()
	for i, entry := range viewMenuTree {
		if i >= len(subs) {
			return
		}
		a.applyViewTreeEntryText(&subs[i], entry)
	}
}

func (a *App) applyViewTreeEntryText(item *widget.MenuItem, entry viewTreeEntry) {
	switch {
	case entry.Leaf != nil:
		item.Text = a.viewLeafText(*entry.Leaf)
	case entry.Group != nil:
		item.Text = i18n.T(entry.Group.Key)
		for i := range min(len(entry.Group.Items), len(item.SubItems)) {
			item.SubItems[i].Text = a.viewLeafText(entry.Group.Items[i])
		}
	}
}

func (a *App) viewLeafText(leaf viewLeafEntry) string {
	label, checked := a.viewLeafLabelChecked(leaf)
	if checked {
		return checkedPrefix + label
	}
	return label
}

func (a *App) viewLeafLabelChecked(leaf viewLeafEntry) (string, bool) {
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
}

func (a *App) ColumnHeaders(grid string) []string {
	columns := a.named[grid].(*widget.DataGridWidget).Grid.Columns()
	headers := make([]string, 0, len(columns))
	for _, c := range columns {
		headers = append(headers, c.Header())
	}
	return headers
}

func (a *App) MenuItemEnabled(sub int) bool {
	return a.subItemEnabled(repositoryMenuIndex, sub)
}

func (a *App) MenuItemText(sub int) string {
	return a.subItemText(repositoryMenuIndex, sub)
}

func (a *App) subItemEnabled(top, sub int) bool {
	items := a.menu.Items()
	if len(items) <= top || sub < 0 || sub >= len(items[top].Items) {
		return false
	}
	return !items[top].Items[sub].Disabled
}

func (a *App) subItemText(top, sub int) string {
	items := a.menu.Items()
	if len(items) <= top || sub < 0 || sub >= len(items[top].Items) {
		return ""
	}
	return items[top].Items[sub].Text
}
