package app

import (
	"bytes"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oops1/headless-gui/v3/widget"
	"github.com/oops1/headless-gui/v3/widget/treeview"

	"github.com/oops1/gogit/internal/config"
	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/repo"
	"github.com/oops1/gogit/internal/ui/addrepo"
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

func stubShowAddRepo(a *App, result addrepo.Result, ok bool) {
	a.showAddRepo = func(_ addrepo.Request, cb func(addrepo.Result, bool)) {
		cb(result, ok)
	}
}

func stubShowAddRepoApplying(t *testing.T, a *App, req addrepo.Request) {
	t.Helper()
	a.showAddRepo = func(_ addrepo.Request, cb func(addrepo.Result, bool)) {
		result, err := addrepo.Apply(req)
		if err != nil {
			t.Fatal(err)
		}
		cb(result, true)
	}
}

func initTestRepo(t *testing.T, path string) {
	t.Helper()
	r, err := gitrepo.Init(path, gitrepo.InitOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestCmdAddOrCreateOpensExistingRepositoryAndActivatesIt(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "repo")
	initTestRepo(t, target)
	a, paths := newTestAppWithPaths(t)
	stubShowAddRepo(a, addrepo.Result{Name: "repo", Path: target}, true)

	a.Dispatch(CmdAddOrCreate)

	if a.State().ActiveRepository == "" {
		t.Fatal("newly added repository must become active")
	}
	node, ok := a.registry.Active()
	if !ok || node.Name != "repo" || node.Path != target {
		t.Fatalf("active node = %+v, ok = %v", node, ok)
	}
	item, ok := a.reposView.Item(node.ID)
	if !ok || item.DisplayText() != "● repo" {
		t.Fatalf("tree item missing or not marked active: %q", item.DisplayText())
	}
	loaded, err := config.Load(paths.ConfigFile())
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Repositories) != 1 || loaded.Repositories[0].Path != target {
		t.Fatalf("repository not persisted: %+v", loaded.Repositories)
	}
}

func TestCmdAddOrCreateCreatesNewRepositoryOnDisk(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "created")
	a := newTestApp(t)
	stubShowAddRepoApplying(t, a, addrepo.Request{Path: target, Name: "created", Mode: addrepo.ModeCreate})

	a.Dispatch(CmdAddOrCreate)

	if !gitrepo.IsRepository(target) {
		t.Fatalf("repository directory not created at %q", target)
	}
	if a.State().ActiveRepository == "" {
		t.Fatal("created repository must become active")
	}
}

func TestCmdAddOrCreateIgnoresDuplicatePath(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "repo")
	cfg := config.Default()
	cfg.Repositories = []config.Repository{{ID: "r1", Name: "Existing", Path: target}}
	a := newTestAppWithConfig(t, cfg)
	stubShowAddRepo(a, addrepo.Result{Name: "repo", Path: target}, true)

	a.Dispatch(CmdAddOrCreate)

	count := 0
	for range a.registry.Walk() {
		count++
	}
	if count != 1 {
		t.Fatalf("duplicate path must not be added, node count = %d", count)
	}
	if a.State().ActiveRepository != "" {
		t.Fatal("duplicate add must not change the active repository")
	}
}

func TestCmdAddOrCreateDoesNothingOnCancel(t *testing.T) {
	a := newTestApp(t)
	stubShowAddRepo(a, addrepo.Result{}, false)

	a.Dispatch(CmdAddOrCreate)

	if a.State().ActiveRepository != "" {
		t.Fatal("cancelling must not change the active repository")
	}
	if len(a.cfg.Repositories) != 0 {
		t.Fatalf("cancelling must not add a repository: %+v", a.cfg.Repositories)
	}
}

func TestCmdAddOrCreateNestsUnderSelectedGroup(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "repo")
	initTestRepo(t, target)
	cfg := config.Default()
	cfg.Groups = []config.Group{{ID: "g1", Name: "Parent"}}
	a := newTestAppWithConfig(t, cfg)
	a.selectedNode = "g1"
	stubShowAddRepo(a, addrepo.Result{Name: "repo", Path: target}, true)

	a.Dispatch(CmdAddOrCreate)

	parent, ok := a.registry.Find("g1")
	if !ok || len(parent.Children) != 1 || parent.Children[0].Name != "repo" {
		t.Fatalf("repository not nested under selected group: %+v", parent)
	}
}

func TestCmdAddOrCreateLogsWarningWhenConfigSaveFails(t *testing.T) {
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
	stubShowAddRepo(a, addrepo.Result{Name: "repo", Path: filepath.Join(t.TempDir(), "repo")}, true)

	a.Dispatch(CmdAddOrCreate)

	if !strings.Contains(buf.String(), "save config failed") {
		t.Fatalf("expected save failure to be logged: %s", buf.String())
	}
}

func TestDefaultShowAddRepoShowsModalWithoutPanicking(t *testing.T) {
	a := newTestApp(t)
	a.showAddRepo(addrepo.Request{}, func(addrepo.Result, bool) {})
}

func TestDefaultShowAddRepoLogsWarningWhenViewCreationFails(t *testing.T) {
	widget.ClearStrings()
	t.Cleanup(widget.ClearStrings)
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	a, err := New(config.Default(), config.Paths{Dir: t.TempDir()}, logger)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)

	prev := newAddRepoView
	wantErr := errors.New("boom")
	newAddRepoView = func(widget.ModalShower, addrepo.Request) (*addrepo.View, error) {
		return nil, wantErr
	}
	t.Cleanup(func() { newAddRepoView = prev })

	called := false
	a.showAddRepo(addrepo.Request{}, func(addrepo.Result, bool) { called = true })

	if called {
		t.Fatal("callback must not run when the dialog fails to open")
	}
	if !strings.Contains(buf.String(), "open add repository dialog failed") {
		t.Fatalf("expected dialog failure to be logged: %s", buf.String())
	}
}

func TestWireAddRepoViewInvokesCallbackOnSuccessfulOpen(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "repo")
	initTestRepo(t, target)
	a := newTestApp(t)
	view, err := addrepo.NewView(a.Engine(), addrepo.Request{})
	if err != nil {
		t.Fatal(err)
	}

	var got addrepo.Result
	var gotOK bool
	a.wireAddRepoView(view, func(result addrepo.Result, ok bool) {
		got, gotOK = result, ok
	})

	view.OnOK(addrepo.Request{Path: target, Mode: addrepo.ModeOpen})

	if !gotOK || got.Path != target {
		t.Fatalf("result = %+v, ok = %v", got, gotOK)
	}
}

func TestWireAddRepoViewReportsNotOKOnCancel(t *testing.T) {
	a := newTestApp(t)
	view, err := addrepo.NewView(a.Engine(), addrepo.Request{})
	if err != nil {
		t.Fatal(err)
	}

	gotOK := true
	a.wireAddRepoView(view, func(_ addrepo.Result, ok bool) { gotOK = ok })

	view.OnCancel()

	if gotOK {
		t.Fatal("cancel must report ok = false")
	}
}

func TestWireAddRepoViewLogsWarningWhenApplyFails(t *testing.T) {
	widget.ClearStrings()
	t.Cleanup(widget.ClearStrings)
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	a, err := New(config.Default(), config.Paths{Dir: t.TempDir()}, logger)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)
	view, err := addrepo.NewView(a.Engine(), addrepo.Request{})
	if err != nil {
		t.Fatal(err)
	}

	called := false
	a.wireAddRepoView(view, func(addrepo.Result, bool) { called = true })

	view.OnOK(addrepo.Request{Path: t.TempDir(), Mode: addrepo.ModeOpen})

	if called {
		t.Fatal("callback must not run when Apply fails")
	}
	if !strings.Contains(buf.String(), "create repository failed") {
		t.Fatalf("expected apply failure to be logged: %s", buf.String())
	}
}
