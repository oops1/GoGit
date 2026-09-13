package app

import (
	"strings"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/config"
	"github.com/oops1/gogit/internal/gitcore/ops"
)

type CommandID string

const (
	CmdAddOrCreate          CommandID = "repository.add-or-create"
	CmdAddGroup             CommandID = "repository.add-group"
	CmdSearch               CommandID = "repository.search"
	CmdCloseRepository      CommandID = "repository.close-repository"
	CmdAddWorktree          CommandID = "repository.add-worktree"
	CmdRemoveWorktree       CommandID = "repository.remove-worktree"
	CmdPruneWorktrees       CommandID = "repository.prune-worktrees"
	CmdRepoSettings         CommandID = "repository.repo-settings"
	CmdSettings             CommandID = "repository.settings"
	CmdClose                CommandID = "repository.close"
	CmdClone                CommandID = "repository.clone"
	CmdFetch                CommandID = "remote.fetch"
	CmdPull                 CommandID = "remote.pull"
	CmdSync                 CommandID = "remote.sync"
	CmdPush                 CommandID = "remote.push"
	CmdPrune                CommandID = "remote.prune"
	CmdManageRemotes        CommandID = "remote.manage"
	CmdStage                CommandID = "edit.stage"
	CmdUnstage              CommandID = "edit.unstage"
	CmdDiscard              CommandID = "edit.discard"
	CmdCommit               CommandID = "local.commit"
	CmdCompareFiles         CommandID = "edit.compare-files"
	CmdMerge                CommandID = "branch.merge"
	CmdRebase               CommandID = "branch.rebase"
	CmdRebaseSteps          CommandID = "branch.rebase.steps"
	CmdReflog               CommandID = "branch.reflog"
	CmdSwitch               CommandID = "branch.switch"
	CmdCompareRefs          CommandID = "branch.compare"
	CmdContinue             CommandID = "branch.continue"
	CmdSkip                 CommandID = "branch.skip"
	CmdAbortMerge           CommandID = "branch.abort-merge"
	CmdFlowStartFeature     CommandID = "branch.flow.start-feature"
	CmdFlowIntegrateDevelop CommandID = "branch.flow.integrate-develop"
	CmdFlowFinishFeature    CommandID = "branch.flow.finish-feature"
	CmdFlowStartHotfix      CommandID = "branch.flow.start-hotfix"
	CmdFlowFinishHotfix     CommandID = "branch.flow.finish-hotfix"
	CmdFlowStartRelease     CommandID = "branch.flow.start-release"
	CmdFlowFinishRelease    CommandID = "branch.flow.finish-release"
	CmdFlowStartSupport     CommandID = "branch.flow.start-support"
	CmdFlowConfigure        CommandID = "branch.flow.configure"
	CmdResetLayout          CommandID = "view.reset-layout"
	CmdRefresh              CommandID = "view.refresh"
	CmdCheckUpdates         CommandID = "help.check-updates"
	CmdAbout                CommandID = "help.about"
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
	"journalGrid": {"", "Journal.Column.Message", "Journal.Column.Author", "Journal.Column.Date", "Journal.Column.Hash"},
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
	HasRemotes       bool
	Merging          bool
	Rebasing         bool
	Rewording        bool
	FlowConfigured   bool
	FlowLight        bool
	FlowCurrent      FlowBranch
	FlowPending      FlowBranch
}

type FlowBranch struct {
	Kind string
	Name string
}

func (s State) flowFinishTarget() FlowBranch {
	if s.FlowPending.Name != "" {
		return s.FlowPending
	}
	return s.FlowCurrent
}

func (s State) flowReady() bool {
	return s.ActiveRepository != "" && !s.Merging && s.FlowConfigured
}

func (s State) Enabled(id CommandID) bool {
	switch id {
	case CmdCloseRepository, CmdAddWorktree, CmdPruneWorktrees, CmdManageRemotes, CmdRefresh, CmdRepoSettings, CmdFlowConfigure:
		return s.ActiveRepository != ""
	case CmdFetch, CmdPull, CmdSync, CmdPush, CmdPrune:
		return s.ActiveRepository != "" && s.HasRemotes
	case CmdRemoveWorktree:
		return s.ActiveRepository != "" && s.ActiveIsWorktree
	case CmdStage, CmdUnstage, CmdDiscard:
		return s.ActiveRepository != "" && s.FilesSelected
	case CmdCommit:
		return s.ActiveRepository != "" && (s.HasStagedChanges || s.Merging)
	case CmdMerge, CmdRebase, CmdRebaseSteps, CmdSwitch:
		return s.ActiveRepository != "" && !s.Merging
	case CmdFlowStartFeature:
		return s.flowReady()
	case CmdFlowStartHotfix, CmdFlowStartRelease, CmdFlowStartSupport:
		return s.flowReady() && !s.FlowLight
	case CmdFlowIntegrateDevelop:
		return s.flowReady() && s.FlowPending.Name == "" && s.FlowCurrent.Kind == ops.FlowKindFeature
	case CmdFlowFinishFeature, CmdFlowFinishRelease, CmdFlowFinishHotfix:
		return s.flowReady() && s.flowFinishTarget().Kind == flowFinishCommands[id]
	case CmdReflog, CmdCompareRefs:
		return s.ActiveRepository != ""
	case CmdAbortMerge, CmdContinue:
		return s.ActiveRepository != "" && s.Merging
	case CmdSkip:
		return s.ActiveRepository != "" && s.Rebasing
	}
	return true
}

const (
	statusPathSeparator = " — "
	statusEllipsis      = "…"
)
