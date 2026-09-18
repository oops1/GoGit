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
	CmdSparseCheckout       CommandID = "repository.sparse-checkout"
	CmdRepoSettings         CommandID = "repository.repo-settings"
	CmdSettings             CommandID = "edit.preferences"
	CmdCopy                 CommandID = "edit.copy"
	CmdSelectAll            CommandID = "edit.select-all"
	CmdClose                CommandID = "repository.close"
	CmdClone                CommandID = "repository.clone"
	CmdFetch                CommandID = "remote.fetch"
	CmdPull                 CommandID = "remote.pull"
	CmdSync                 CommandID = "remote.sync"
	CmdPush                 CommandID = "remote.push"
	CmdPrune                CommandID = "remote.prune"
	CmdManageRemotes        CommandID = "remote.manage"
	CmdSubmoduleUpdate      CommandID = "remote.submodule.update"
	CmdSubmoduleInitialize  CommandID = "remote.submodule.initialize"
	CmdSubmoduleSync        CommandID = "remote.submodule.sync"
	CmdSubmoduleAdd         CommandID = "remote.submodule.add"
	CmdSubmoduleRemove      CommandID = "remote.submodule.remove"
	CmdSubmoduleUnregister  CommandID = "remote.submodule.unregister"
	CmdSubmoduleReset       CommandID = "remote.submodule.reset"
	CmdStage                CommandID = "local.stage"
	CmdUnstage              CommandID = "local.unstage"
	CmdDiscard              CommandID = "local.discard"
	CmdIndexEditor          CommandID = "local.index-editor"
	CmdCommit               CommandID = "local.commit"
	CmdStashSave            CommandID = "local.stash-save"
	CmdStashApply           CommandID = "local.stash-apply"
	CmdStashDrop            CommandID = "local.stash-drop"
	CmdStashSelection       CommandID = "local.stash-selection"
	CmdIgnore               CommandID = "local.ignore"
	CmdRemove               CommandID = "local.remove"
	CmdCompareFiles         CommandID = "query.compare-files"
	CmdLog                  CommandID = "query.log"
	CmdBlame                CommandID = "query.blame"
	CmdInvestigate          CommandID = "query.investigate"
	CmdMerge                CommandID = "branch.merge"
	CmdRebase               CommandID = "branch.rebase"
	CmdRebaseSteps          CommandID = "branch.rebase.steps"
	CmdReflog               CommandID = "query.reflog"
	CmdSwitch               CommandID = "branch.switch"
	CmdCompareRefs          CommandID = "query.compare-branches"
	CmdContinue             CommandID = "branch.continue"
	CmdSkip                 CommandID = "branch.skip"
	CmdAbortMerge           CommandID = "branch.abort-merge"
	CmdFlowStartFeature     CommandID = "tools.flow.start-feature"
	CmdFlowIntegrateDevelop CommandID = "tools.flow.integrate-develop"
	CmdFlowFinishFeature    CommandID = "tools.flow.finish-feature"
	CmdFlowStartHotfix      CommandID = "tools.flow.start-hotfix"
	CmdFlowFinishHotfix     CommandID = "tools.flow.finish-hotfix"
	CmdFlowStartRelease     CommandID = "tools.flow.start-release"
	CmdFlowFinishRelease    CommandID = "tools.flow.finish-release"
	CmdFlowStartSupport     CommandID = "tools.flow.start-support"
	CmdFlowConfigure        CommandID = "tools.flow.configure"
	CmdGc                   CommandID = "tools.gc"
	CmdFsck                 CommandID = "tools.fsck"
	CmdRevealRepository     CommandID = "tools.reveal"
	CmdOpenTerminal         CommandID = "tools.terminal"
	CmdResetLayout          CommandID = "window.reset-layout"
	CmdRefresh              CommandID = "view.refresh"
	CmdCheckUpdates         CommandID = "help.check-updates"
	CmdAbout                CommandID = "help.about"
)

const (
	viewPanePrefix     = "window.pane:"
	viewThemePrefix    = "view.theme:"
	viewLanguagePrefix = "view.language:"
	layoutModePrefix   = "window.layout:"
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

var layoutModeOrder = []string{config.LayoutDocks, config.LayoutSidebar}

var layoutModeKeys = map[string]string{
	config.LayoutDocks:   "Layout.Docks",
	config.LayoutSidebar: "Layout.Sidebar",
}

func cmdPane(id string) CommandID       { return CommandID(viewPanePrefix + id) }
func cmdTheme(name string) CommandID    { return CommandID(viewThemePrefix + name) }
func cmdLanguage(code string) CommandID { return CommandID(viewLanguagePrefix + code) }
func cmdLayout(mode string) CommandID   { return CommandID(layoutModePrefix + mode) }

func paneIDFromCommand(id CommandID) (string, bool) {
	return cutPrefix(id, viewPanePrefix)
}

func layoutFromCommand(id CommandID) (string, bool) {
	return cutPrefix(id, layoutModePrefix)
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
	HasChanges       bool
	HasStashable     bool
	HasRemotes       bool
	HasStashes       bool
	HasSubmodules    bool
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

var commandsWaitingForTheirCore = map[CommandID]bool{
	CmdCopy:        true,
	CmdSelectAll:   true,
	CmdIgnore:      true,
	CmdRemove:      true,
	CmdLog:         true,
	CmdBlame:       true,
	CmdInvestigate: true,
	CmdGc:          true,
	CmdFsck:        true,
}

func (s State) Enabled(id CommandID) bool {
	if commandsWaitingForTheirCore[id] {
		return false
	}
	switch id {
	case CmdCloseRepository, CmdAddWorktree, CmdPruneWorktrees, CmdSparseCheckout, CmdManageRemotes, CmdRefresh, CmdRepoSettings, CmdFlowConfigure,
		CmdRevealRepository, CmdOpenTerminal:
		return s.ActiveRepository != ""
	case CmdFetch, CmdPull, CmdSync, CmdPush, CmdPrune:
		return s.ActiveRepository != "" && s.HasRemotes
	case CmdRemoveWorktree:
		return s.ActiveRepository != "" && s.ActiveIsWorktree
	case CmdStage, CmdUnstage, CmdDiscard, CmdIndexEditor:
		return s.ActiveRepository != "" && s.FilesSelected
	case CmdCommit:
		return s.ActiveRepository != "" && (s.HasStagedChanges || s.HasChanges || s.Merging)
	case CmdStashSave:
		return s.ActiveRepository != "" && s.HasStashable && !s.Merging
	case CmdStashSelection:
		return s.ActiveRepository != "" && s.FilesSelected && !s.Merging
	case CmdStashApply, CmdStashDrop:
		return s.ActiveRepository != "" && s.HasStashes
	case CmdSubmoduleUpdate, CmdSubmoduleInitialize, CmdSubmoduleSync, CmdSubmoduleRemove, CmdSubmoduleUnregister, CmdSubmoduleReset:
		return s.ActiveRepository != "" && s.HasSubmodules
	case CmdSubmoduleAdd:
		return s.ActiveRepository != ""
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
