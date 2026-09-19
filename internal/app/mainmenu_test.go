package app

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/config"
	"github.com/oops1/gogit/internal/i18n"
)

var smartGitMenuOrder = []string{
	"Menu.Repository",
	"Menu.Edit",
	"Menu.View",
	"Menu.Remote",
	"Menu.Local",
	"Menu.Branch",
	"Menu.Query",
	"Menu.Tools",
	"Menu.Window",
	"Menu.Help",
}

func TestTheMenuBarFollowsTheSmartGitOrder(t *testing.T) {
	a := newTestApp(t)
	items := a.menu.Items()
	if len(items) != len(smartGitMenuOrder) {
		t.Fatalf("top menus = %d, want %d", len(items), len(smartGitMenuOrder))
	}
	for i, key := range smartGitMenuOrder {
		if menuBarDefs[i].TitleKey != key {
			t.Fatalf("menu %d is %q, want %q", i, menuBarDefs[i].TitleKey, key)
		}
		if items[i].Text != widget.Tr(key) {
			t.Fatalf("menu %d text = %q, want %q", i, items[i].Text, widget.Tr(key))
		}
	}
}

func TestTheMenuBarIndexesNameTheirMenus(t *testing.T) {
	for i, index := range []int{
		repositoryMenuIndex, editMenuIndex, viewMenuIndex, remoteMenuIndex, localMenuIndex,
		branchMenuIndex, queryMenuIndex, toolsMenuIndex, windowMenuIndex, helpMenuIndex,
	} {
		if index != i {
			t.Fatalf("menu index %d of %q is %d", i, smartGitMenuOrder[i], index)
		}
	}
}

func TestTheMenuBarKeepsEveryTranslationInBothLanguages(t *testing.T) {
	a := newTestApp(t)
	for _, lang := range []string{"en", "ru"} {
		a.SetLanguage(lang)
		for i, def := range menuBarDefs {
			checkTranslated(t, lang, def.TitleKey, a.menu.Items()[i].Text)
			for _, entry := range def.Tree {
				switch {
				case entry.Leaf != nil:
					checkTranslated(t, lang, entry.Leaf.Key, i18n.T(entry.Leaf.Key))
				case entry.Group != nil:
					checkTranslated(t, lang, entry.Group.Key, i18n.T(entry.Group.Key))
					for _, leaf := range entry.Group.Items {
						if leaf.Key == "" {
							continue
						}
						checkTranslated(t, lang, leaf.Key, i18n.T(leaf.Key))
					}
				}
			}
		}
	}
}

func checkTranslated(t *testing.T, lang, key, text string) {
	t.Helper()
	if text == "" || text == key {
		t.Fatalf("language %q: key %q shows %q instead of a translation", lang, key, text)
	}
}

func TestTheWholeMenuBarMirrorsTheMarkup(t *testing.T) {
	a := newTestApp(t)
	items := a.menu.Items()
	for i, def := range menuBarDefs {
		subs := items[i].Items
		if len(subs) != len(def.Tree) {
			t.Fatalf("menu %q has %d items, the markup shows %d", def.TitleKey, len(def.Tree), len(subs))
		}
		for at, entry := range def.Tree {
			if entry.Separator != subs[at].Separator {
				t.Fatalf("menu %q item %d disagrees about being a separator", def.TitleKey, at)
			}
			if entry.Group == nil {
				if len(subs[at].SubItems) != 0 {
					t.Fatalf("menu %q item %d must be flat", def.TitleKey, at)
				}
				continue
			}
			if len(subs[at].SubItems) != len(entry.Group.Items) {
				t.Fatalf("menu %q group %q has %d items, the markup shows %d",
					def.TitleKey, entry.Group.Key, len(entry.Group.Items), len(subs[at].SubItems))
			}
		}
	}
}

func TestEveryMenuItemEitherRunsACommandOrStaysDisabled(t *testing.T) {
	a := newTestApp(t)
	items := a.menu.Items()
	for i, def := range menuBarDefs {
		for at, entry := range def.Tree {
			if entry.Separator {
				continue
			}
			if entry.Leaf != nil {
				checkMenuLeaf(t, a, def.TitleKey, *entry.Leaf, subItem(items[i].Items, at))
				continue
			}
			for j, leaf := range entry.Group.Items {
				if leaf.Key == "" {
					continue
				}
				checkMenuLeaf(t, a, entry.Group.Key, leaf, subItem(items[i].Items[at].SubItems, j))
			}
		}
	}
}

func handlerOf(a *App, id CommandID) func() {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.handlers[id]
}

func subItem(items []widget.MenuItem, at int) widget.MenuItem {
	if at >= len(items) {
		return widget.MenuItem{}
	}
	return items[at]
}

func checkMenuLeaf(t *testing.T, a *App, menu string, leaf menuLeafEntry, item widget.MenuItem) {
	t.Helper()
	if leaf.Command == "" {
		t.Fatalf("menu %q item %q carries no command", menu, leaf.Key)
	}
	if item.OnClick == nil {
		t.Fatalf("menu %q item %q is not wired to the dispatcher", menu, leaf.Key)
	}
	if handlerOf(a, leaf.Command) == nil && !item.Disabled {
		t.Fatalf("menu %q item %q has no handler yet, so it must stay disabled", menu, leaf.Key)
	}
}

func TestTheItemsStillWaitingForTheirCoreStayDisabled(t *testing.T) {
	full := State{
		ActiveRepository: "r1", ActiveIsWorktree: true, FilesSelected: true,
		HasStagedChanges: true, HasChanges: true, HasStashable: true, HasRemotes: true,
		HasStashes: true, HasSubmodules: true, Merging: true, Rebasing: true,
		FlowConfigured: true,
	}
	for id := range commandsWaitingForTheirCore {
		if full.Enabled(id) {
			t.Fatalf("command %q has no core yet but reports itself enabled", id)
		}
	}
	shown := map[CommandID]bool{}
	for _, def := range menuBarDefs {
		for _, entry := range def.Tree {
			if entry.Leaf != nil {
				shown[entry.Leaf.Command] = true
			}
			if entry.Group == nil {
				continue
			}
			for _, leaf := range entry.Group.Items {
				shown[leaf.Command] = true
			}
		}
	}
	for id := range commandsWaitingForTheirCore {
		if !shown[id] {
			t.Fatalf("command %q is listed as waiting but no menu shows it", id)
		}
	}
}

func TestTheLocalMenuGathersTheWorkingCopyCommands(t *testing.T) {
	want := []CommandID{
		CmdCommit, CmdStage, CmdUnstage, CmdDiscard, CmdIndexEditor,
		CmdStashSave, CmdStashApply, CmdStashDrop, CmdIgnore, CmdRemove,
	}
	if got := treeCommands(localMenuTree); !slices.Equal(got, want) {
		t.Fatalf("local menu = %v, want %v", got, want)
	}
}

func TestTheQueryMenuGathersTheHistoryCommands(t *testing.T) {
	want := []CommandID{CmdLog, CmdBlame, CmdInvestigate, CmdCompareFiles, CmdCompareRefs, CmdReflog}
	if got := treeCommands(queryMenuTree); !slices.Equal(got, want) {
		t.Fatalf("query menu = %v, want %v", got, want)
	}
}

func TestTheEditMenuGathersCopySelectionAndSettings(t *testing.T) {
	want := []CommandID{CmdCopy, CmdSelectAll, CmdSettings}
	if got := treeCommands(editMenuTree); !slices.Equal(got, want) {
		t.Fatalf("edit menu = %v, want %v", got, want)
	}
}

func TestTheToolsMenuGathersFlowMaintenanceAndTheOutsideWorld(t *testing.T) {
	if toolsMenuTree[0].Group == nil || toolsMenuTree[0].Group.Key != "Menu.Tools.GitFlow" {
		t.Fatal("git-flow must open the tools menu")
	}
	if toolsMenuTree[2].Group == nil || toolsMenuTree[2].Group.Key != "Menu.Tools.Maintenance" {
		t.Fatal("repository maintenance must follow git-flow")
	}
	if got := treeCommands(toolsMenuTree); !slices.Equal(got, []CommandID{CmdBisect, CmdRevealRepository, CmdOpenTerminal, CmdConsole}) {
		t.Fatalf("tools menu leaves = %v", got)
	}
	if got := groupCommands(*toolsMenuTree[2].Group); !slices.Equal(got, []CommandID{CmdGc, CmdFsck}) {
		t.Fatalf("maintenance group = %v", got)
	}
}

func TestTheBranchMenuNoLongerCarriesQueriesOrGitFlow(t *testing.T) {
	for _, entry := range branchMenuTree {
		if entry.Group != nil {
			t.Fatalf("the branch menu must have no groups, found %q", entry.Group.Key)
		}
	}
	for _, id := range []CommandID{CmdReflog, CmdCompareRefs} {
		if slices.Contains(treeCommands(branchMenuTree), id) {
			t.Fatalf("%q belongs to the query menu", id)
		}
	}
}

func treeCommands(tree []menuTreeEntry) []CommandID {
	commands := []CommandID{}
	for _, entry := range tree {
		if entry.Leaf != nil {
			commands = append(commands, entry.Leaf.Command)
		}
	}
	return commands
}

func groupCommands(group menuGroupEntry) []CommandID {
	commands := []CommandID{}
	for _, leaf := range group.Items {
		commands = append(commands, leaf.Command)
	}
	return commands
}

func TestTheToolsMenuOpensTheActiveRepositoryOutside(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	initTestRepo(t, target)
	cfg := config.Default()
	cfg.Repositories = []config.Repository{{ID: "r1", Name: "Main", Path: target}}
	a := newTestAppWithConfig(t, cfg)
	started := captureStartedTools(t)

	if a.Dispatch(CmdRevealRepository) || a.Dispatch(CmdOpenTerminal) {
		t.Fatal("both tools must be disabled without an active repository")
	}
	a.ActivateRepository("r1")
	if !a.Dispatch(CmdRevealRepository) || !a.Dispatch(CmdOpenTerminal) {
		t.Fatal("both tools must run once a repository is open")
	}

	if len(*started) != 2 {
		t.Fatalf("started tools = %v", *started)
	}
	pointedAtTheRepository := 0
	for _, line := range *started {
		if !strings.Contains(line, dir) {
			t.Fatalf("tool %q was not pointed at the repository", line)
		}
		if strings.Contains(line, target) {
			pointedAtTheRepository++
		}
	}
	if pointedAtTheRepository == 0 {
		t.Fatalf("started tools = %v, want one of them at the repository itself", *started)
	}
}

func TestTheToolsMenuOpensNothingWithoutAnOpenRepository(t *testing.T) {
	a := newTestApp(t)
	started := captureStartedTools(t)
	a.SetActiveRepository("r1", false)

	if !a.Dispatch(CmdRevealRepository) || !a.Dispatch(CmdOpenTerminal) {
		t.Fatal("the commands are enabled, so they must dispatch")
	}

	if len(*started) != 0 {
		t.Fatalf("started tools = %v, want none for a repository that is not open", *started)
	}
}

func TestTheWindowMenuSwitchesBetweenDockPanesAndTheSideBar(t *testing.T) {
	a := newTestApp(t)
	if a.sidebarMounted {
		t.Fatal("dock panes are the default")
	}

	a.SetLayout(config.LayoutSidebar)
	if !a.sidebarMounted || a.Config().UI.Layout != config.LayoutSidebar {
		t.Fatal("the side bar must be mounted")
	}

	a.SetLayout("no such layout")
	if a.sidebarMounted || a.Config().UI.Layout != config.LayoutDocks {
		t.Fatal("an unknown mode must fall back to dock panes")
	}
}
