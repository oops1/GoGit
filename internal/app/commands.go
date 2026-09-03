package app

import "github.com/oops1/headless-gui/v3/widget"

type CommandID string

const (
	CmdAddOrCreate     CommandID = "repository.add-or-create"
	CmdAddGroup        CommandID = "repository.add-group"
	CmdSearch          CommandID = "repository.search"
	CmdCloseRepository CommandID = "repository.close-repository"
	CmdAddWorktree     CommandID = "repository.add-worktree"
	CmdRemoveWorktree  CommandID = "repository.remove-worktree"
	CmdPruneWorktrees  CommandID = "repository.prune-worktrees"
	CmdSettings        CommandID = "repository.settings"
	CmdClose           CommandID = "repository.close"
	CmdPull            CommandID = "remote.pull"
	CmdSync            CommandID = "remote.sync"
	CmdPush            CommandID = "remote.push"
	CmdCommit          CommandID = "local.commit"
	CmdResetLayout     CommandID = "view.reset-layout"
)

const cmdSeparator CommandID = ""

var repositoryMenu = []CommandID{
	CmdAddOrCreate,
	CmdAddGroup,
	CmdSearch,
	CmdCloseRepository,
	cmdSeparator,
	CmdAddWorktree,
	CmdRemoveWorktree,
	CmdPruneWorktrees,
	cmdSeparator,
	CmdSettings,
	CmdClose,
}

var menuKeys = map[CommandID]string{
	CmdAddOrCreate:     "Menu.Repository.AddOrCreate",
	CmdAddGroup:        "Menu.Repository.AddGroup",
	CmdSearch:          "Menu.Repository.Search",
	CmdCloseRepository: "Menu.Repository.CloseRepository",
	CmdAddWorktree:     "Menu.Repository.AddWorktree",
	CmdRemoveWorktree:  "Menu.Repository.RemoveWorktree",
	CmdPruneWorktrees:  "Menu.Repository.PruneWorktrees",
	CmdSettings:        "Menu.Repository.Settings",
	CmdClose:           "Menu.Repository.Close",
}

var dockSideSizes = map[widget.DockSide]int{
	widget.DockLeft:   260,
	widget.DockTop:    200,
	widget.DockBottom: 220,
}

var gridColumnKeys = map[string][]string{
	"filesGrid":   {"Files.Column.Status", "Files.Column.Name", "Files.Column.Path"},
	"journalGrid": {"Journal.Column.Graph", "Journal.Column.Message", "Journal.Column.Author", "Journal.Column.Date", "Journal.Column.Hash"},
}

var toolbarButtons = map[CommandID]string{
	CmdPull:   "btnPull",
	CmdSync:   "btnSync",
	CmdPush:   "btnPush",
	CmdCommit: "btnCommit",
}

type State struct {
	ActiveRepository string
	ActiveIsWorktree bool
}

func (s State) Enabled(id CommandID) bool {
	switch id {
	case CmdCloseRepository, CmdAddWorktree, CmdPruneWorktrees, CmdPull, CmdSync, CmdPush, CmdCommit:
		return s.ActiveRepository != ""
	case CmdRemoveWorktree:
		return s.ActiveRepository != "" && s.ActiveIsWorktree
	}
	return true
}
