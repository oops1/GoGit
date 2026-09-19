package app

import (
	"cmp"
	"slices"

	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/dialogs/toolbar"
)

var newToolbarView = toolbar.NewView

func (a *App) registerToolbarHandlers() {
	a.handlers[CmdConfigureToolbar] = a.openConfigureToolbar
}

func toolbarCatalogEntries() []toolbar.Entry {
	entries := []toolbar.Entry{
		{ID: toolbar.SeparatorID, Label: i18n.T("Dialog.Toolbar.Separator")},
		{ID: toolbar.StretchID, Label: i18n.T("Dialog.Toolbar.Stretch")},
	}
	commands := make([]toolbar.Entry, 0, len(toolbarCatalog()))
	for _, entry := range toolbarCatalog() {
		commands = append(commands, toolbar.Entry{ID: entry.ID, Label: i18n.T(entry.LabelKey)})
	}
	slices.SortFunc(commands, func(a, b toolbar.Entry) int {
		return cmp.Or(cmp.Compare(a.Label, b.Label), cmp.Compare(a.ID, b.ID))
	})
	return append(entries, commands...)
}

func (a *App) toolbarModel() *toolbar.Model {
	return toolbar.NewModel(toolbarCatalogEntries(), a.configuredToolbarItems(), defaultToolbarItems(), a.cfg.UI.ToolbarCaptions)
}

func (a *App) openConfigureToolbar() {
	view, err := newToolbarView(a.toolbarModel())
	if err != nil {
		a.log.Warn("open configure toolbar dialog failed", "error", err)
		return
	}
	view.OnCancel = func() { a.eng.CloseModal(view.Dialog()) }
	view.OnOK = func(result toolbar.Result) {
		a.eng.CloseModal(view.Dialog())
		a.applyToolbarConfiguration(result)
	}
	a.showModal(view.Dialog(), view)
}

func (a *App) applyToolbarConfiguration(result toolbar.Result) {
	a.cfg.UI.ToolbarItems = result.Items
	a.cfg.UI.ToolbarCaptions = result.Captions
	a.buildToolbar()
	if err := a.cfg.Save(a.paths.ConfigFile()); err != nil {
		a.log.Warn("save config failed", "error", err)
	}
}
