package app

import (
	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/config"
	"github.com/oops1/gogit/internal/ui/branches"
	"github.com/oops1/gogit/internal/ui/sidebar"
)

const (
	paneRepositories = "repositories"
	paneBranches     = "branches"
	paneFiles        = "files"
	paneJournal      = "journal"
	paneDetails      = "details"
	sidebarGridRow   = 1
)

func (a *App) setupSidebar() {
	grid, ok := a.named["mainGrid"].(*widget.Grid)
	if !ok {
		return
	}
	a.mainGrid = grid
	a.sidebar = sidebar.NewView()
	root := a.sidebar.Root()
	root.SetGridProps(sidebarGridRow, 0, 1, 1)
	root.SetVisible(false)
	grid.AddChild(root)
}

func (a *App) restyleSidebar(t *widget.Theme) {
	if a.sidebar != nil {
		a.sidebar.Restyle(t)
	}
}

func (a *App) retitleSidebar() {
	if a.sidebar != nil {
		a.sidebar.Retitle()
	}
}

func (a *App) applyLayoutMode(mode string) {
	if a.sidebar == nil {
		return
	}
	wanted := mode == config.LayoutSidebar
	if wanted == a.sidebarMounted {
		return
	}
	dock := a.Dock()
	if wanted {
		parts := sidebar.Parts{
			Repositories: takePaneContent(dock, paneRepositories),
			Branches:     takePaneContent(dock, paneBranches),
			Files:        takePaneContent(dock, paneFiles),
			Journal:      takePaneContent(dock, paneJournal),
			Details:      takePaneContent(dock, paneDetails),
			Diff:         dock.Center(),
		}
		dock.SetCenter(nil)
		a.sidebar.Mount(parts)
	} else {
		parts := a.sidebar.Unmount()
		putPaneContent(dock, paneRepositories, parts.Repositories)
		putPaneContent(dock, paneBranches, parts.Branches)
		putPaneContent(dock, paneFiles, parts.Files)
		putPaneContent(dock, paneJournal, parts.Journal)
		putPaneContent(dock, paneDetails, parts.Details)
		dock.SetCenter(parts.Diff)
	}
	dock.SetVisible(!wanted)
	a.sidebar.Root().SetVisible(wanted)
	a.sidebarMounted = wanted
	a.mainGrid.SetBounds(a.mainGrid.Bounds())
	a.mainGrid.Invalidate()
}

func takePaneContent(dock *widget.DockManager, id string) widget.Widget {
	pane := dock.FindPane(id)
	if pane == nil {
		return nil
	}
	content := pane.Content()
	pane.SetContent(nil)
	return content
}

func putPaneContent(dock *widget.DockManager, id string, content widget.Widget) {
	if pane := dock.FindPane(id); pane != nil {
		pane.SetContent(content)
	}
}

func (a *App) showSidebarBranchCounts(snap branches.Snapshot) {
	if a.sidebar == nil {
		return
	}
	remote := 0
	for _, r := range snap.Remotes {
		remote += len(r.Branches)
	}
	a.sidebar.SetCount(sidebar.SectionBranches, len(snap.Local))
	a.sidebar.SetCount(sidebar.SectionTags, len(snap.Tags))
	a.sidebar.SetCount(sidebar.SectionRemotes, remote)
	a.sidebar.SetCount(sidebar.SectionStash, sidebar.NoCount)
}

func (a *App) showSidebarSubmoduleCount(count int) {
	if a.sidebar == nil {
		return
	}
	if count == 0 {
		count = sidebar.NoCount
	}
	a.sidebar.SetCount(sidebar.SectionSubmodules, count)
}

func (a *App) showSidebarWorkingCounts(files, staged int) {
	if a.sidebar == nil {
		return
	}
	a.sidebar.SetCount(sidebar.SectionWorkingCopy, files)
	a.sidebar.SetCount(sidebar.SectionIndex, staged)
}
