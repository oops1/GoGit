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

func enabledItem(key string, action func(), enabled bool) widget.MenuItem {
	item := menuItem(key, action)
	item.Disabled = !enabled
	return item
}

func laterItem(key string) widget.MenuItem {
	return enabledItem(key, nil, false)
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
	working := len(a.selectedWorkingPaths()) > 0
	a.setFilesSelected(working)
	state := a.State()
	path := a.filePathOf(file)
	rel := file.RelPath
	tracked := file.Status != changes.RowUntracked
	conflict := file.Status == changes.RowConflict
	onDisk := path != "" && file.Status != changes.RowDeleted

	items := []widget.MenuItem{
		enabledItem("Menu.Files.OpenFile", func() { a.openFile(path) }, onDisk),
		enabledItem("Menu.Context.Reveal", func() { a.revealPath(path) }, path != ""),
		laterItem("Menu.Files.Edit"),
		laterItem("Menu.Files.SetExecutable"),
		laterItem("Menu.Files.UnsetExecutable"),
		menuSeparator(),
		enabledItem("Menu.Files.ShowChanges", a.openCompare, state.Enabled(CmdCompareFiles)),
	}
	if history := a.historyItems(rel); len(history) > 1 {
		for _, entry := range history[1:] {
			entry.Disabled = entry.Disabled || !tracked
			items = append(items, entry)
		}
	}
	items = append(items,
		a.investigateFileItem(rel, tracked),
		menuSeparator(),
		enabledItem("Menu.Local.Commit", a.openCommit, state.Enabled(CmdCommit)),
		enabledItem("Menu.Files.StashSelection", a.openStashSelection, state.Enabled(CmdStashSelection)),
		menuSeparator(),
	)
	edit := a.editItems()
	edit[0].Disabled = edit[0].Disabled || (file.WorkingState == "" && !conflict)
	edit[1].Disabled = edit[1].Disabled || file.IndexState == ""
	edit[2].Disabled = edit[2].Disabled || !tracked
	return append(items,
		edit[0],
		edit[1],
		laterItem("Menu.Files.IndexEditor"),
		laterItem("Menu.Files.Rename"),
		menuSeparator(),
		enabledItem("Menu.Files.ConflictSolver", func() { a.openConflictEditor(rel) }, conflict),
		a.resolveMenu(file),
		menuSeparator(),
		laterItem("Menu.Files.Ignore"),
		edit[2],
		laterItem("Menu.Files.Remove"),
		enabledItem("Menu.Files.Delete", func() { a.deleteFile(path) }, working && onDisk),
		menuSeparator(),
		enabledItem("Menu.Files.CopyName", func() { a.copyToClipboard(file.Name) }, file.Name != ""),
		enabledItem("Menu.Context.CopyPath", func() { a.copyToClipboard(path) }, path != ""),
		enabledItem("Menu.Files.CopyRelativePath", func() { a.copyToClipboard(rel) }, rel != ""),
		menuSeparator(),
		laterItem("Menu.Files.SelectDirectory"),
		laterItem("Menu.Files.SelectRoot"),
	)
}

func (a *App) resolveMenu(file changes.Row) widget.MenuItem {
	item := widget.MenuItem{Text: i18n.T("Menu.Files.Resolve"), Disabled: true}
	if conflict := a.conflictItems(file); len(conflict) > 2 {
		item.Disabled = false
		item.SubItems = conflict[1 : len(conflict)-1]
	}
	return item
}

func (a *App) editItems() []widget.MenuItem {
	state := a.State()
	items := make([]widget.MenuItem, 0, 3)
	for _, entry := range []struct {
		key string
		id  CommandID
		run func()
	}{
		{"Menu.Local.Stage", CmdStage, a.stageSelected},
		{"Menu.Local.Unstage", CmdUnstage, a.unstageSelected},
		{"Menu.Local.Discard", CmdDiscard, a.discardSelected},
	} {
		items = append(items, enabledItem(entry.key, entry.run, state.Enabled(entry.id)))
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
	items = append(items, a.resetItems(commit.ID)...)
	return append(items, a.tagItems(commit.ID)...)
}

func (a *App) journalGrid() *widget.DataGridWidget {
	return a.named["journalGrid"].(*widget.DataGridWidget)
}

func (a *App) wireContextMenus() {
	a.reposView.OnMenu = a.treeMenu
	a.filesGrid.Data().RowContextMenu = a.filesMenu
	a.journalGrid().RowContextMenu = a.journalMenu
	a.diffView.OnMenu = a.diffMenu
}
