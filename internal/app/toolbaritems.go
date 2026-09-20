package app

import (
	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/ui/dialogs/toolbar"
)

const toolbarFlowID = "tools.flow"

type toolbarEntry struct {
	ID       string
	Name     string
	LabelKey string
	TipKey   string
	Icon     string
	Command  CommandID
	Items    func(*App) []widget.MenuItem
	Arrow    func(State) bool
}

func (e toolbarEntry) hasMenu() bool { return e.Items != nil }

func commandEntry(cmd CommandID, name, label, tip, icon string) toolbarEntry {
	return toolbarEntry{ID: string(cmd), Name: name, LabelKey: label, TipKey: tip, Icon: icon, Command: cmd}
}

func toolbarCatalog() []toolbarEntry {
	entries := []toolbarEntry{
		commandEntry(CmdFetch, "btnFetch", "Toolbar.Fetch", "Menu.Remote.Fetch", "fetch"),
		commandEntry(CmdPull, "btnPull", "Toolbar.Pull", "Toolbar.Pull.Tip", "pull"),
		commandEntry(CmdSync, "btnSync", "Toolbar.Sync", "Toolbar.Sync.Tip", "sync"),
		commandEntry(CmdPush, "btnPush", "Toolbar.Push", "Toolbar.Push.Tip", "push"),
		commandEntry(CmdPrune, "btnPrune", "Toolbar.Prune", "Menu.Remote.Prune", "prune_remote"),
		commandEntry(CmdManageRemotes, "btnRemotes", "Toolbar.Remotes", "Menu.Remote.Manage", "remotes"),
		commandEntry(CmdCommit, "btnCommit", "Toolbar.Commit", "Toolbar.Commit.Tip", "commit"),
		commandEntry(CmdStage, "btnStage", "Toolbar.Stage", "Menu.Local.Stage", "stage"),
		commandEntry(CmdIndexEditor, "btnIndexEditor", "Toolbar.IndexEditor", "Menu.Files.IndexEditor", "index_editor"),
		commandEntry(CmdUnstage, "btnUnstage", "Toolbar.Unstage", "Menu.Local.Unstage", "unstage"),
		commandEntry(CmdDiscard, "btnDiscard", "Toolbar.Discard", "Menu.Local.Discard", "discard"),
		commandEntry(CmdStashSave, "btnSaveStash", "Toolbar.SaveStash", "Toolbar.SaveStash.Tip", "stash_save"),
		commandEntry(CmdStashApply, "btnApplyStash", "Toolbar.ApplyStash", "Toolbar.ApplyStash.Tip", "stash_apply"),
		commandEntry(CmdStashDrop, "btnDropStash", "Toolbar.DropStash", "Menu.Local.DropStash", "stash_drop"),
		commandEntry(CmdIgnore, "btnIgnore", "Toolbar.Ignore", "Menu.Files.Ignore", "ignore"),
		commandEntry(CmdRemove, "btnRemove", "Toolbar.Remove", "Menu.Files.Remove", "remove"),
		commandEntry(CmdLog, "btnLog", "Toolbar.Log", "Menu.Query.Log", "log"),
		commandEntry(CmdBlame, "btnBlame", "Toolbar.Blame", "Menu.Context.Blame", "blame"),
		commandEntry(CmdInvestigate, "btnInvestigate", "Toolbar.Investigate", "Menu.Files.Investigate", "investigate"),
		commandEntry(CmdCompareFiles, "btnChanges", "Toolbar.Changes", "Menu.Query.CompareFiles", "compare"),
		commandEntry(CmdCompareRefs, "btnCompareRefs", "Toolbar.CompareBranches", "Menu.Query.CompareBranches", "compare_refs"),
		commandEntry(CmdReflog, "btnReflog", "Toolbar.Reflog", "Menu.Query.Reflog", "history"),
		commandEntry(CmdSwitch, "btnCheckOut", "Toolbar.CheckOut", "Menu.Branch.Switch", "switch"),
		commandEntry(CmdMerge, "btnMerge", "Toolbar.Merge", "Menu.Branch.Merge", "merge"),
		commandEntry(CmdRebase, "btnRebase", "Toolbar.Rebase", "Menu.Branch.Rebase", "rebase"),
		commandEntry(CmdContinue, "btnContinue", "Toolbar.Continue", "Menu.Branch.Continue", "continue"),
		commandEntry(CmdSkip, "btnSkip", "Toolbar.Skip", "Menu.Branch.Skip", "skip"),
		commandEntry(CmdAbortMerge, "btnAbortMerge", "Toolbar.AbortMerge", "Menu.Branch.AbortMerge", "merge_abort"),
		{ID: toolbarFlowID, Name: "btnGitFlow", LabelKey: "Toolbar.GitFlow", TipKey: "Toolbar.GitFlow.Tip", Icon: flowToolbarIcon, Items: (*App).flowMenuItems},
		commandEntry(CmdFlowStartFeature, "btnFlowStart", "Toolbar.FlowStart", "Menu.Tools.GitFlow.StartFeature", "merge"),
		commandEntry(CmdFlowIntegrateDevelop, "btnFlowIntegrate", "Toolbar.FlowIntegrate", "Menu.Tools.GitFlow.IntegrateDevelop", "merge"),
		commandEntry(CmdFlowFinishFeature, "btnFlowFinish", "Toolbar.FlowFinish", "Menu.Tools.GitFlow.FinishFeature", "merge"),
		commandEntry(CmdRevealRepository, "btnReveal", "Toolbar.Reveal", "Menu.Context.Reveal", "reveal"),
		commandEntry(CmdOpenTerminal, "btnTerminal", "Toolbar.Terminal", "Menu.Context.Terminal", "terminal"),
		commandEntry(CmdAddOrCreate, "btnAddRepository", "Toolbar.AddRepository", "Menu.Repository.AddOrCreate", "repo_add"),
		commandEntry(CmdClone, "btnClone", "Toolbar.Clone", "Menu.Repository.Clone", "clone"),
		commandEntry(CmdSearch, "btnSearchRepositories", "Toolbar.SearchRepositories", "Menu.Repository.Search", "search"),
		commandEntry(CmdSparseCheckout, "btnSparseCheckout", "Toolbar.SparseCheckout", "Menu.Repository.SparseCheckout", "sparse_checkout"),
		commandEntry(CmdRepoSettings, "btnRepositorySettings", "Toolbar.RepositorySettings", "Menu.Repository.RepoSettings", "repo_settings"),
		commandEntry(CmdSettings, "btnSettings", "Toolbar.Settings", "Menu.Edit.Preferences", "settings"),
		commandEntry(CmdRefresh, "btnRefresh", "Toolbar.Refresh", "Menu.View.Refresh", "refresh"),
	}
	withMenu(entries, CmdPull, (*App).pullMenuItems, nil)
	withMenu(entries, CmdSync, (*App).syncMenuItems, nil)
	withMenu(entries, CmdPush, (*App).pushMenuItems, nil)
	withMenu(entries, CmdStashSave, (*App).saveStashMenuItems, nil)
	withMenu(entries, CmdStashApply, (*App).applyStashMenuItems, stashesListed)
	withMenu(entries, CmdLog, (*App).logMenuItems, nil)
	return entries
}

func stashesListed(state State) bool { return state.HasStashes }

func withMenu(entries []toolbarEntry, cmd CommandID, items func(*App) []widget.MenuItem, arrow func(State) bool) {
	for i := range entries {
		if entries[i].Command == cmd {
			entries[i].Items = items
			entries[i].Arrow = arrow
			return
		}
	}
}

func defaultToolbarItems() []string {
	return []string{
		string(CmdPull), string(CmdSync), string(CmdPush),
		toolbar.SeparatorID,
		string(CmdCommit),
		toolbar.SeparatorID,
		string(CmdStage), string(CmdIndexEditor), string(CmdUnstage),
		toolbar.SeparatorID,
		string(CmdDiscard),
		toolbar.SeparatorID,
		string(CmdStashSave), string(CmdStashApply),
		toolbar.StretchID,
		string(CmdLog), string(CmdBlame), string(CmdInvestigate),
		toolbar.StretchID,
		toolbarFlowID,
		toolbar.SeparatorID,
		string(CmdMerge), string(CmdRebase),
	}
}

func toolbarEntryByID(id string) (toolbarEntry, bool) {
	for _, entry := range toolbarCatalog() {
		if entry.ID == id {
			return entry, true
		}
	}
	return toolbarEntry{}, false
}

func (a *App) configuredToolbarItems() []string {
	items := a.cfg.UI.ToolbarItems
	if len(items) == 0 {
		return defaultToolbarItems()
	}
	return items
}

var pullMenuLeaves = []menuLeafEntry{
	{Key: "Menu.Remote.Fetch", Command: CmdFetch},
	{Key: "Menu.Remote.Pull", Command: CmdPull},
	{},
	{Key: "Menu.Remote.Prune", Command: CmdPrune},
}

var syncMenuLeaves = []menuLeafEntry{
	{Key: "Menu.Remote.Pull", Command: CmdPull},
	{Key: "Menu.Remote.Push", Command: CmdPush},
	{},
	{Key: "Menu.Remote.Fetch", Command: CmdFetch},
}

var pushMenuLeaves = []menuLeafEntry{
	{Key: "Menu.Remote.Push", Command: CmdPush},
	{},
	{Key: "Menu.Remote.Manage", Command: CmdManageRemotes},
}

var logMenuLeaves = []menuLeafEntry{
	{Key: "Menu.Query.Log", Command: CmdLog},
	{Key: "Menu.Context.Blame", Command: CmdBlame},
	{Key: "Menu.Files.Investigate", Command: CmdInvestigate},
	{},
	{Key: "Menu.Query.Reflog", Command: CmdReflog},
}

func (a *App) pullMenuItems() []widget.MenuItem { return a.commandMenuItems(pullMenuLeaves) }

func (a *App) syncMenuItems() []widget.MenuItem { return a.commandMenuItems(syncMenuLeaves) }

func (a *App) pushMenuItems() []widget.MenuItem { return a.commandMenuItems(pushMenuLeaves) }

func (a *App) logMenuItems() []widget.MenuItem { return a.commandMenuItems(logMenuLeaves) }

func (a *App) commandMenuItems(leaves []menuLeafEntry) []widget.MenuItem {
	state := a.State()
	items := make([]widget.MenuItem, 0, len(leaves))
	for _, leaf := range leaves {
		if leaf.Key == "" {
			items = append(items, menuSeparator())
			continue
		}
		cmd := leaf.Command
		items = append(items, enabledItem(leaf.Key, func() { a.Dispatch(cmd) }, state.Enabled(cmd)))
	}
	return items
}
