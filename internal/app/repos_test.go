package app

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oops1/headless-gui/v3/widget"
	"github.com/oops1/headless-gui/v3/widget/treeview"

	"github.com/oops1/gogit/internal/config"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/repo"
)

func TestReposTreeReflectsConfigAfterNew(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.Groups = []config.Group{{ID: "g1", Name: "Work"}}
	cfg.Repositories = []config.Repository{{ID: "r1", Name: "Main", Path: filepath.Join(dir, "main"), Group: "g1"}}
	a := newTestAppWithConfig(t, cfg)

	node, ok := a.registry.Find("r1")
	if !ok || node.Name != "Main" {
		t.Fatalf("registry not built from config: %+v %v", node, ok)
	}
	item, ok := a.reposView.Item("r1")
	if !ok || item.DisplayText() != "Main" {
		t.Fatal("tree does not reflect config on startup")
	}
}

func TestActivateRepositorySetsStateStatusTextAndMarker(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.Repositories = []config.Repository{{ID: "r1", Name: "Main", Path: filepath.Join(dir, "main")}}
	a := newTestAppWithConfig(t, cfg)

	a.ActivateRepository("r1")

	if a.State().ActiveRepository != "r1" || a.State().ActiveIsWorktree {
		t.Fatalf("state = %+v", a.State())
	}
	if got := a.Widget("statusText").(*widget.Label).Text(); got != "Main" {
		t.Fatalf("status text = %q", got)
	}
	item, ok := a.reposView.Item("r1")
	if !ok || item.DisplayText() != "● Main" {
		t.Fatalf("marker missing: %q", item.DisplayText())
	}
}

func TestActivateRepositoryOnWorktreeSetsActiveIsWorktree(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.Repositories = []config.Repository{
		{ID: "r1", Name: "Main", Path: filepath.Join(dir, "main")},
		{ID: "w1", Name: "feature", Path: filepath.Join(dir, "feature"), Worktree: true, Parent: "r1"},
	}
	a := newTestAppWithConfig(t, cfg)

	a.ActivateRepository("w1")

	if a.State().ActiveRepository != "w1" || !a.State().ActiveIsWorktree {
		t.Fatalf("state = %+v", a.State())
	}
}

func TestActivateRepositoryIgnoresGroupNode(t *testing.T) {
	cfg := config.Default()
	cfg.Groups = []config.Group{{ID: "g1", Name: "Work"}}
	a := newTestAppWithConfig(t, cfg)

	a.ActivateRepository("g1")

	if a.State().ActiveRepository != "" {
		t.Fatal("activating a group must be a no-op")
	}
}

func TestActivateRepositoryIgnoresUnknownID(t *testing.T) {
	a := newTestApp(t)

	a.ActivateRepository("missing")

	if a.State().ActiveRepository != "" {
		t.Fatal("activating an unknown id must be a no-op")
	}
}

func TestReposTreeActivationInvokesApp(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.Repositories = []config.Repository{{ID: "r1", Name: "Main", Path: filepath.Join(dir, "main")}}
	a := newTestAppWithConfig(t, cfg)

	item, ok := a.reposView.Item("r1")
	if !ok {
		t.Fatal("item missing")
	}
	tree := a.Widget("reposTree").(*widget.TreeViewWidget)
	tree.Tree.OnItemInvoked(treeview.ItemInvokedEvent{Item: item})

	if a.State().ActiveRepository != "r1" {
		t.Fatal("double click / enter on the tree must activate the repository")
	}
}

func TestReposTreeSelectionRemembersGroupForAddGroupParent(t *testing.T) {
	cfg := config.Default()
	cfg.Groups = []config.Group{{ID: "g1", Name: "Parent"}}
	a := newTestAppWithConfig(t, cfg)

	item, ok := a.reposView.Item("g1")
	if !ok {
		t.Fatal("item missing")
	}
	tree := a.Widget("reposTree").(*widget.TreeViewWidget)
	tree.Tree.SetSelectedItem(item)

	if a.selectedNode != "g1" {
		t.Fatalf("selectedNode = %q, want %q", a.selectedNode, "g1")
	}
}

func TestCloseRepositoryClearsRegistryTreeAndStatus(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.Repositories = []config.Repository{{ID: "r1", Name: "Main", Path: filepath.Join(dir, "main")}}
	a := newTestAppWithConfig(t, cfg)
	a.ActivateRepository("r1")

	a.CloseRepository()

	if a.State().ActiveRepository != "" {
		t.Fatal("state must be reset")
	}
	if _, ok := a.registry.Active(); ok {
		t.Fatal("registry active node must be cleared")
	}
	if got := a.Widget("statusText").(*widget.Label).Text(); got != i18n.T("Status.NoRepository") {
		t.Fatalf("status text = %q", got)
	}
	item, ok := a.reposView.Item("r1")
	if !ok || item.DisplayText() != "Main" {
		t.Fatalf("marker not removed from tree: %q", item.DisplayText())
	}
}

func TestCmdAddGroupCreatesRootGroupAndPersistsConfig(t *testing.T) {
	a, paths := newTestAppWithPaths(t)
	a.askInput = func(title, prompt string, cb func(text string, ok bool)) {
		cb("Work", true)
	}

	a.Dispatch(CmdAddGroup)

	var groupID string
	for n := range a.registry.Walk() {
		if n.Kind == repo.KindGroup && n.Name == "Work" {
			groupID = n.ID
		}
	}
	if groupID == "" {
		t.Fatal("group not added to registry")
	}
	if item, ok := a.reposView.Item(groupID); !ok || item.DisplayText() != "Work" {
		t.Fatal("group not rendered in tree")
	}

	loaded, err := config.Load(paths.ConfigFile())
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Groups) != 1 || loaded.Groups[0].Name != "Work" {
		t.Fatalf("group not saved to config: %+v", loaded.Groups)
	}
}

func TestCmdAddGroupLogsWarningWhenConfigSaveFails(t *testing.T) {
	widget.ClearStrings()
	t.Cleanup(widget.ClearStrings)
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "config.toml"), 0o700); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	a, err := New(config.Default(), config.Paths{Dir: dir}, logger)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)
	a.askInput = func(title, prompt string, cb func(text string, ok bool)) {
		cb("Work", true)
	}

	a.Dispatch(CmdAddGroup)

	if !strings.Contains(buf.String(), "save config failed") {
		t.Fatalf("expected save failure to be logged: %s", buf.String())
	}
}

func TestCmdAddGroupIgnoresEmptyName(t *testing.T) {
	a := newTestApp(t)
	a.askInput = func(title, prompt string, cb func(text string, ok bool)) {
		cb("   ", true)
	}

	a.Dispatch(CmdAddGroup)

	if len(a.cfg.Groups) != 0 {
		t.Fatalf("blank name must be ignored: %+v", a.cfg.Groups)
	}
}

func TestCmdAddGroupIgnoresCancel(t *testing.T) {
	a := newTestApp(t)
	a.askInput = func(title, prompt string, cb func(text string, ok bool)) {
		cb("Work", false)
	}

	a.Dispatch(CmdAddGroup)

	if len(a.cfg.Groups) != 0 {
		t.Fatalf("cancel must be ignored: %+v", a.cfg.Groups)
	}
}

func TestCmdAddGroupNestsUnderSelectedGroup(t *testing.T) {
	cfg := config.Default()
	cfg.Groups = []config.Group{{ID: "g1", Name: "Parent"}}
	a := newTestAppWithConfig(t, cfg)
	a.selectedNode = "g1"
	a.askInput = func(title, prompt string, cb func(text string, ok bool)) {
		cb("Child", true)
	}

	a.Dispatch(CmdAddGroup)

	parent, ok := a.registry.Find("g1")
	if !ok || len(parent.Children) != 1 || parent.Children[0].Name != "Child" {
		t.Fatalf("child group not nested under selected parent: %+v", parent)
	}
}

func TestCmdAddGroupUsesRootWhenSelectedNodeIsNotAGroup(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.Repositories = []config.Repository{{ID: "r1", Name: "Main", Path: filepath.Join(dir, "main")}}
	a := newTestAppWithConfig(t, cfg)
	a.selectedNode = "r1"
	a.askInput = func(title, prompt string, cb func(text string, ok bool)) {
		cb("Work", true)
	}

	a.Dispatch(CmdAddGroup)

	found := false
	for _, root := range a.registry.Roots() {
		if root.Kind == repo.KindGroup && root.Name == "Work" {
			found = true
		}
	}
	if !found {
		t.Fatal("group must land at root when the selected node is not a group")
	}
}

func TestRestoreActiveRepositoryFromConfigOnStartup(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.Repositories = []config.Repository{{ID: "r1", Name: "Main", Path: filepath.Join(dir, "main")}}
	cfg.ActiveRepository = "r1"
	a := newTestAppWithConfig(t, cfg)

	if a.State().ActiveRepository != "r1" {
		t.Fatalf("state = %+v", a.State())
	}
	if got := a.Widget("statusText").(*widget.Label).Text(); got != "Main" {
		t.Fatalf("status text = %q", got)
	}
	item, ok := a.reposView.Item("r1")
	if !ok || item.DisplayText() != "● Main" {
		t.Fatalf("marker missing on restore: %q", item.DisplayText())
	}
}

func TestRestoreActiveRepositoryIgnoresUnknownID(t *testing.T) {
	cfg := config.Default()
	cfg.ActiveRepository = "missing"
	a := newTestAppWithConfig(t, cfg)

	if a.State().ActiveRepository != "" {
		t.Fatal("unknown active id must be ignored")
	}
}

func TestRestoreActiveRepositoryIgnoresGroupID(t *testing.T) {
	cfg := config.Default()
	cfg.Groups = []config.Group{{ID: "g1", Name: "Work"}}
	cfg.ActiveRepository = "g1"
	a := newTestAppWithConfig(t, cfg)

	if a.State().ActiveRepository != "" {
		t.Fatal("a group id must never be restored as the active repository")
	}
}

func TestStatusTextSurvivesLanguageSwitch(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.Repositories = []config.Repository{{ID: "r1", Name: "Main", Path: filepath.Join(dir, "main")}}
	a := newTestAppWithConfig(t, cfg)
	a.ActivateRepository("r1")

	a.SetLanguage("ru")
	if got := a.Widget("statusText").(*widget.Label).Text(); got != "Main" {
		t.Fatalf("status text after language switch = %q", got)
	}

	a.CloseRepository()
	a.SetLanguage("en")
	if got := a.Widget("statusText").(*widget.Label).Text(); got != i18n.T("Status.NoRepository") {
		t.Fatalf("status text after close and language switch = %q", got)
	}
}

func TestDefaultAskInputShowsModalWithoutPanicking(t *testing.T) {
	a := newTestApp(t)
	a.askInput("Title", "Prompt", func(string, bool) {})
}
