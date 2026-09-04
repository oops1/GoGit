package app

import (
	"strings"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/config"
)

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
	CmdStage           CommandID = "edit.stage"
	CmdUnstage         CommandID = "edit.unstage"
	CmdDiscard         CommandID = "edit.discard"
	CmdCommit          CommandID = "local.commit"
	CmdResetLayout     CommandID = "view.reset-layout"
	CmdRefresh         CommandID = "view.refresh"
)

const (
	viewPanePrefix     = "view.pane:"
	viewThemePrefix    = "view.theme:"
	viewLanguagePrefix = "view.language:"
	checkedPrefix      = "✓ "
)

var viewPaneIDs = []string{"repositories", "branches", "files", "journal"}

var viewPaneKeys = map[string]string{
	"repositories": "Pane.Repositories",
	"branches":     "Pane.Branches",
	"files":        "Pane.Files",
	"journal":      "Pane.Journal",
}

var viewThemeOrder = []string{config.ThemeSystem, config.ThemeDark, config.ThemeLight}

var viewThemeKeys = map[string]string{
	config.ThemeSystem: "Theme.System",
	config.ThemeDark:   "Theme.Dark",
	config.ThemeLight:  "Theme.Light",
}

var viewLanguageOrder = []string{"en", "ru"}

func cmdPane(id string) CommandID       { return CommandID(viewPanePrefix + id) }
func cmdTheme(name string) CommandID    { return CommandID(viewThemePrefix + name) }
func cmdLanguage(code string) CommandID { return CommandID(viewLanguagePrefix + code) }

func paneIDFromCommand(id CommandID) (string, bool) {
	return cutPrefix(id, viewPanePrefix)
}

func themeFromCommand(id CommandID) (string, bool) {
	return cutPrefix(id, viewThemePrefix)
}

func languageFromCommand(id CommandID) (string, bool) {
	return cutPrefix(id, viewLanguagePrefix)
}

func cutPrefix(id CommandID, prefix string) (string, bool) {
	s := string(id)
	if !strings.HasPrefix(s, prefix) {
		return "", false
	}
	return strings.TrimPrefix(s, prefix), true
}

func languageKey(code string) string {
	return "Language." + code
}

var dockSideSizes = map[widget.DockSide]int{
	widget.DockLeft:   260,
	widget.DockTop:    200,
	widget.DockBottom: 220,
}

var gridColumnKeys = map[string][]string{
	"journalGrid": {"Journal.Column.Graph", "Journal.Column.Message", "Journal.Column.Author", "Journal.Column.Date", "Journal.Column.Hash"},
}

var toolbarButtons = map[CommandID]string{
	CmdPull:   "btnPull",
	CmdSync:   "btnSync",
	CmdPush:   "btnPush",
	CmdCommit: "btnCommit",
}

var toolbarIcons = map[CommandID]string{
	CmdPull:   "pull",
	CmdSync:   "sync",
	CmdPush:   "push",
	CmdCommit: "commit",
}

type State struct {
	ActiveRepository string
	ActiveIsWorktree bool
	FilesSelected    bool
	HasStagedChanges bool
}

func (s State) Enabled(id CommandID) bool {
	switch id {
	case CmdCloseRepository, CmdAddWorktree, CmdPruneWorktrees, CmdPull, CmdSync, CmdPush, CmdRefresh:
		return s.ActiveRepository != ""
	case CmdRemoveWorktree:
		return s.ActiveRepository != "" && s.ActiveIsWorktree
	case CmdStage, CmdUnstage, CmdDiscard:
		return s.ActiveRepository != "" && s.FilesSelected
	case CmdCommit:
		return s.ActiveRepository != "" && s.HasStagedChanges
	}
	return true
}
