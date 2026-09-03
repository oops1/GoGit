package app

import (
	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/i18n"
)

const repositoryMenuIndex = 0

func (a *App) wireMenu() {
	a.menu.OnSelect = func(top, sub int, _ string) {
		if id, ok := menuCommand(top, sub); ok {
			a.Dispatch(id)
		}
	}
}

func menuCommand(top, sub int) (CommandID, bool) {
	if top != repositoryMenuIndex || sub < 0 || sub >= len(repositoryMenu) {
		return "", false
	}
	id := repositoryMenu[sub]
	return id, id != cmdSeparator
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
	if len(items) > repositoryMenuIndex {
		subs := items[repositoryMenuIndex].Items
		for i, id := range repositoryMenu {
			if id == cmdSeparator || i >= len(subs) {
				continue
			}
			subs[i].Disabled = !state.Enabled(id)
		}
	}
	for id, name := range toolbarButtons {
		a.named[name].(*widget.Button).SetEnabled(state.Enabled(id))
	}
}

func (a *App) retranslate() {
	a.menu.SetMenuText(repositoryMenuIndex, i18n.T("Menu.Repository"))
	for i, id := range repositoryMenu {
		if key, ok := menuKeys[id]; ok {
			a.menu.SetSubItemText(repositoryMenuIndex, i, i18n.T(key))
		}
	}
	a.retranslateGrids()
	a.root.Title = i18n.T("App.Title")
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
	items := a.menu.Items()
	if len(items) <= repositoryMenuIndex || sub < 0 || sub >= len(items[repositoryMenuIndex].Items) {
		return false
	}
	return !items[repositoryMenuIndex].Items[sub].Disabled
}

func (a *App) MenuItemText(sub int) string {
	items := a.menu.Items()
	if len(items) <= repositoryMenuIndex || sub < 0 || sub >= len(items[repositoryMenuIndex].Items) {
		return ""
	}
	return items[repositoryMenuIndex].Items[sub].Text
}
