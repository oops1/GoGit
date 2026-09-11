package app

import (
	"path/filepath"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/repo"
	"github.com/oops1/gogit/internal/ui/changes"
	"github.com/oops1/gogit/internal/ui/journal"
	"github.com/oops1/gogit/internal/ui/repos"
)

func menuItem(key string, action func()) widget.MenuItem {
	return widget.MenuItem{Text: i18n.T(key), Icon: menuKeyIcon(key), OnClick: action}
}

func menuSeparator() widget.MenuItem {
	return widget.MenuItem{Separator: true}
}

func (a *App) treeMenu(target repos.MenuTarget) []widget.MenuItem {
	if target.Directory != "" {
		return a.pathMenu(target.Directory)
	}
	node, ok := a.registry.Find(target.ID)
	if !ok {
		return a.emptyTreeMenu()
	}
	if node.Kind == repo.KindGroup {
		return a.emptyTreeMenu()
	}
	return a.repositoryMenu(node)
}

func (a *App) emptyTreeMenu() []widget.MenuItem {
	return []widget.MenuItem{
		menuItem("Menu.Repository.AddOrCreate", a.addOrCreateRepository),
		menuItem("Menu.Repository.AddGroup", a.addGroup),
	}
}

func (a *App) repositoryMenu(node *repo.Node) []widget.MenuItem {
	items := []widget.MenuItem{
		menuItem("Menu.Context.Open", func() { a.ActivateRepository(node.ID) }),
		menuSeparator(),
	}
	items = append(items, a.pathMenu(node.Path)...)
	items = append(items, menuSeparator(),
		menuItem("Menu.Repository.RepoSettings", func() { a.openRepoSettings(node.ID) }))
	if a.State().ActiveRepository != node.ID {
		return items
	}
	return append(items, menuItem("Menu.Repository.CloseRepository", a.CloseRepository))
}

func (a *App) pathMenu(path string) []widget.MenuItem {
	return []widget.MenuItem{
		menuItem("Menu.Context.Reveal", func() { a.revealPath(path) }),
		menuItem("Menu.Context.Terminal", func() { a.openTerminalAt(path) }),
		menuItem("Menu.Context.CopyPath", func() { a.copyToClipboard(path) }),
	}
}

func (a *App) filesMenu(item any, row int) []widget.MenuItem {
	file, ok := item.(changes.Row)
	if !ok {
		return nil
	}
	a.filesGrid.Data().Grid.SetSelectedIndex(row)
	path := a.filePathOf(file)
	items := append(a.conflictItems(file),
		menuItem("Menu.Context.Reveal", func() { a.revealPath(path) }),
		menuItem("Menu.Context.Terminal", func() { a.openTerminalAt(containingDirectory(path)) }),
		menuItem("Menu.Context.CopyPath", func() { a.copyToClipboard(path) }),
	)
	items = append(items, menuSeparator())
	return append(items, a.editItems()...)
}

func (a *App) editItems() []widget.MenuItem {
	state := a.State()
	items := make([]widget.MenuItem, 0, 3)
	for _, entry := range []struct {
		key string
		id  CommandID
		run func()
	}{
		{"Menu.Edit.Stage", CmdStage, a.stageSelected},
		{"Menu.Edit.Unstage", CmdUnstage, a.unstageSelected},
		{"Menu.Edit.Discard", CmdDiscard, a.discardSelected},
	} {
		item := menuItem(entry.key, entry.run)
		item.Disabled = !state.Enabled(entry.id)
		items = append(items, item)
	}
	return items
}

func (a *App) filePathOf(row changes.Row) string {
	o := a.opened()
	if o == nil {
		return ""
	}
	return filepath.Join(o.path, filepath.FromSlash(row.RelPath))
}

func (a *App) journalMenu(item any, row int) []widget.MenuItem {
	commit, ok := item.(journal.Row)
	if !ok {
		return nil
	}
	a.journalGrid().Grid.SetSelectedIndex(row)
	items := []widget.MenuItem{
		menuItem("Menu.Context.CopyHash", func() { a.copyToClipboard(commit.ID.String()) }),
		menuItem("Menu.Context.CopyMessage", func() { a.copyToClipboard(commit.Message) }),
	}
	items = append(items, a.pickItems(commit.ID)...)
	return append(items, a.resetItems(commit.ID)...)
}

func (a *App) journalGrid() *widget.DataGridWidget {
	return a.named["journalGrid"].(*widget.DataGridWidget)
}

func (a *App) wireContextMenus() {
	a.reposView.OnMenu = a.treeMenu
	a.filesGrid.Data().RowContextMenu = a.filesMenu
	a.journalGrid().RowContextMenu = a.journalMenu
}
