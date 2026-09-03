package app

import (
	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/i18n"
)

const repositoryMenuIndex = 0
const viewMenuIndex = 1

func (a *App) wireMenu() {
	a.menu.OnSelect = func(top, sub int, _ string) {
		if id, ok := a.menuCommand(top, sub); ok {
			a.Dispatch(id)
		}
	}
}

func (a *App) menuCommand(top, sub int) (CommandID, bool) {
	switch top {
	case repositoryMenuIndex:
		return lookupCommand(repositoryMenu, sub)
	case viewMenuIndex:
		return lookupCommand(a.viewMenu, sub)
	}
	return "", false
}

func lookupCommand(ids []CommandID, sub int) (CommandID, bool) {
	if sub < 0 || sub >= len(ids) {
		return "", false
	}
	id := ids[sub]
	return id, id != cmdSeparator
}

func (a *App) buildViewEntries() []CommandID {
	entries := make([]CommandID, 0, len(viewPaneIDs)+len(viewThemeOrder)+len(a.languages)+6)
	for _, id := range viewPaneIDs {
		entries = append(entries, cmdPane(id))
	}
	entries = append(entries, cmdSeparator, CmdResetLayout, cmdSeparator)
	for _, name := range viewThemeOrder {
		entries = append(entries, cmdTheme(name))
	}
	entries = append(entries, cmdSeparator)
	for _, code := range a.languages {
		entries = append(entries, cmdLanguage(code))
	}
	entries = append(entries, cmdSeparator, CmdRefresh)
	return entries
}

func (a *App) buildViewMenu() {
	items := make([]widget.MenuItem, len(a.viewMenu))
	for i, id := range a.viewMenu {
		if id == cmdSeparator {
			items[i] = widget.MenuItem{Separator: true}
			continue
		}
		items[i] = widget.MenuItem{Text: a.viewItemText(id)}
	}
	a.menu.AddMenu(i18n.T("Menu.View"), items...)
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
	for _, code := range a.languages {
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
	applyMenuDisabled(items, viewMenuIndex, a.viewMenu, state)
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
}

func (a *App) applyViewTexts() {
	for i, id := range a.viewMenu {
		if id == cmdSeparator {
			continue
		}
		a.menu.SetSubItemText(viewMenuIndex, i, a.viewItemText(id))
	}
}

func (a *App) viewItemText(id CommandID) string {
	label, checked := a.viewItemLabelChecked(id)
	if checked {
		return checkedPrefix + label
	}
	return label
}

func (a *App) viewItemLabelChecked(id CommandID) (string, bool) {
	if paneID, ok := paneIDFromCommand(id); ok {
		label := i18n.T("Menu.View.PanesPrefix") + i18n.T(viewPaneKeys[paneID])
		return label, a.PaneVisible(paneID)
	}
	if theme, ok := themeFromCommand(id); ok {
		return i18n.T(viewThemeKeys[theme]), a.cfg.Theme == theme
	}
	if code, ok := languageFromCommand(id); ok {
		return languageLabel(code), i18n.Current() == code
	}
	return i18n.T(viewStaticKeys[id]), false
}

func languageLabel(code string) string {
	key := languageKey(code)
	if label := i18n.T(key); label != key {
		return label
	}
	return code
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

func (a *App) ViewItemEnabled(sub int) bool {
	return a.subItemEnabled(viewMenuIndex, sub)
}

func (a *App) ViewItemText(sub int) string {
	return a.subItemText(viewMenuIndex, sub)
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
