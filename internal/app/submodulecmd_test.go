package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/config"
	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/index"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/ops"
	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/gitcore/transport"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/repo"
	"github.com/oops1/gogit/internal/ui/sidebar"
)

func allowLocalSubmodules(t *testing.T) {
	t.Helper()
	global := filepath.Join(t.TempDir(), "gitconfig")
	text := "[user]\n\tname = Go Git\n\temail = gogit@example.com\n[protocol \"file\"]\n\tallow = always\n"
	if err := os.WriteFile(global, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", global)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
}

func commitTestFiles(t *testing.T, dir string, files map[string]string, links map[string]hash.ObjectID) hash.ObjectID {
	t.Helper()
	r, err := gitrepo.Open(dir, gitrepo.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	var paths []string
	for name, content := range files {
		if err := writeFile(filepath.Dir(filepath.Join(dir, name)), filepath.Base(name), content); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, name)
	}
	if err := ops.Stage(t.Context(), r, paths, ops.StageOptions{}); err != nil {
		t.Fatal(err)
	}
	if len(links) > 0 {
		idx, err := index.ReadFile(r.IndexFile())
		if err != nil {
			t.Fatal(err)
		}
		for path, id := range links {
			idx.Add(index.Entry{Path: path, Mode: object.ModeSubmodule, ID: id, Stage: index.StageMerged})
		}
		if err := idx.WriteFile(r.IndexFile(), index.Version2); err != nil {
			t.Fatal(err)
		}
	}
	id, err := ops.Commit(t.Context(), r, ops.CommitOptions{Message: "commit"})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func buildSuperproject(t *testing.T) string {
	t.Helper()
	allowLocalSubmodules(t)
	root := t.TempDir()
	lib := filepath.Join(root, "lib")
	initTestRepoWithBranch(t, lib, "main")
	libHead := commitTestFiles(t, lib, map[string]string{"lib.txt": "lib\n"}, nil)
	super := filepath.Join(root, "super")
	initTestRepoWithBranch(t, super, "main")
	gitmodules := "[submodule \"lib\"]\n\tpath = libs/lib\n\turl = " + filepath.ToSlash(lib) + "\n"
	commitTestFiles(t, super, map[string]string{".gitmodules": gitmodules}, map[string]hash.ObjectID{"libs/lib": libHead})
	return super
}

func waitForSubmodules(t *testing.T, a *App, ready func([]ops.Submodule) bool) []ops.Submodule {
	t.Helper()
	deadline := time.Now().Add(testTimeout)
	for {
		list := readOnDispatcher(t, a, a.branchesView.Submodules)
		if ready(list) {
			return list
		}
		if time.Now().After(deadline) {
			t.Fatalf("submodules = %+v", list)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func submoduleIn(state ops.SubmoduleState) func([]ops.Submodule) bool {
	return func(list []ops.Submodule) bool { return len(list) == 1 && list[0].State == state }
}

func runSubmoduleCommand(t *testing.T, a *App, id CommandID) []string {
	t.Helper()
	views := captureOperationViews(t)
	runOnDispatcher(t, a, func() { a.Dispatch(id) })
	view := lastOperationView(t, views)
	waitForFinishedOperation(t, a, view)
	return readOnDispatcher(t, a, view.Lines)
}

func TestTheBranchesTreeShowsSubmodulesAndTheMenuFollowsThem(t *testing.T) {
	super := buildSuperproject(t)
	a := activatedWorkingApp(t, super)

	list := waitForSubmodules(t, a, submoduleIn(ops.SubmoduleStateNotInitialized))

	if list[0].Path != "libs/lib" || !readOnDispatcher(t, a, a.State).HasSubmodules {
		t.Fatalf("submodules = %+v", list)
	}
	if !readOnDispatcher(t, a, func() bool { _, ok := a.branchesView.SubmoduleItem("libs/lib"); return ok }) {
		t.Fatal("the submodule node is missing")
	}
	for id, want := range map[CommandID]bool{
		CmdSubmoduleUpdate: true, CmdSubmoduleInitialize: true, CmdSubmoduleSync: true,
		CmdSubmoduleAdd: true, CmdSubmoduleRemove: true, CmdSubmoduleUnregister: true, CmdSubmoduleReset: true,
	} {
		if _, enabled, ok := readOnDispatcher(t, a, func() menuState { return menuStateOf(a, id) }).split(); !ok || enabled != want {
			t.Fatalf("%s enabled = %v, want %v", id, enabled, want)
		}
	}
	if got, ok := a.sidebar.CountText(sidebar.SectionSubmodules); !ok || got != "1" {
		t.Fatalf("sidebar count = %q", got)
	}

	items := readOnDispatcher(t, a, func() []string { return menuTexts(a.submoduleMenu(list[0])) })
	want := refKeys("Menu.Context.Open", "-", "Menu.Remote.Submodule.Update", "Menu.Remote.Submodule.Initialize", "Menu.Remote.Submodule.Synchronize", "-",
		"Menu.Remote.Submodule.Remove", "Menu.Remote.Submodule.Unregister", "Menu.Remote.Submodule.Reset", "-",
		"Menu.Context.Reveal", "Menu.Context.Terminal", "Menu.Context.CopyPath")
	if !slices.Equal(items, want) {
		t.Fatalf("menu = %v\nwant %v", items, want)
	}
	menu := readOnDispatcher(t, a, func() map[string]bool { return enabledByText(a.submoduleMenu(list[0])) })
	if menu[i18n.T("Menu.Context.Open")] || !menu[i18n.T("Menu.Remote.Submodule.Initialize")] || menu[i18n.T("Menu.Remote.Submodule.Update")] {
		t.Fatalf("enabled items before initialization = %v", menu)
	}

	runOnDispatcher(t, a, a.CloseRepository)
	if len(readOnDispatcher(t, a, a.branchesView.Submodules)) != 0 || readOnDispatcher(t, a, a.State).HasSubmodules {
		t.Fatal("closing the repository kept the submodules")
	}
}

type menuState struct {
	text    string
	enabled bool
	ok      bool
}

func (m menuState) split() (string, bool, bool) { return m.text, m.enabled, m.ok }

func menuStateOf(a *App, id CommandID) menuState {
	text, enabled, ok := a.MenuItemByCommand(id)
	return menuState{text, enabled, ok}
}

func enabledByText(items []widget.MenuItem) map[string]bool {
	out := map[string]bool{}
	for _, item := range items {
		if !item.Separator {
			out[item.Text] = !item.Disabled
		}
	}
	return out
}

func TestInitializeUpdateAndSynchronizeRunInTheOperationWindow(t *testing.T) {
	super := buildSuperproject(t)
	a := activatedWorkingApp(t, super)
	waitForSubmodules(t, a, submoduleIn(ops.SubmoduleStateNotInitialized))

	lines := runSubmoduleCommand(t, a, CmdSubmoduleInitialize)
	list := waitForSubmodules(t, a, submoduleIn(ops.SubmoduleStateUpToDate))
	if !slices.Contains(lines, i18n.Tf("Operation.Log.SubmoduleCheckedOut", "libs/lib", list[0].Head.String())) {
		t.Fatalf("initialize log = %q", lines)
	}

	if lines := runSubmoduleCommand(t, a, CmdSubmoduleUpdate); !slices.Contains(lines, i18n.T("Operation.Log.SubmodulesUpToDate")) {
		t.Fatalf("update log = %q", lines)
	}
	if lines := runSubmoduleCommand(t, a, CmdSubmoduleSync); !slices.Contains(lines, i18n.Tf("Operation.Log.SubmoduleSynchronized", "libs/lib")) {
		t.Fatalf("sync log = %q", lines)
	}

	menu := readOnDispatcher(t, a, func() map[string]bool { return enabledByText(a.submoduleMenu(list[0])) })
	if !menu[i18n.T("Menu.Context.Open")] || menu[i18n.T("Menu.Remote.Submodule.Initialize")] || !menu[i18n.T("Menu.Remote.Submodule.Update")] {
		t.Fatalf("enabled items after initialization = %v", menu)
	}
}

func TestOpeningASubmoduleActivatesItAsARepository(t *testing.T) {
	super := buildSuperproject(t)
	a := activatedWorkingApp(t, super)
	waitForSubmodules(t, a, submoduleIn(ops.SubmoduleStateNotInitialized))
	runSubmoduleCommand(t, a, CmdSubmoduleInitialize)
	list := waitForSubmodules(t, a, submoduleIn(ops.SubmoduleStateUpToDate))
	dir := filepath.Join(super, "libs", "lib")

	runOnDispatcher(t, a, func() { a.openSubmodule(list[0]) })

	node, ok := a.registry.FindByPath(dir)
	if !ok || readOnDispatcher(t, a, a.State).ActiveRepository != node.ID {
		t.Fatalf("the submodule %s was not activated", dir)
	}
	runOnDispatcher(t, a, func() { a.ActivateRepository("r1") })
	runOnDispatcher(t, a, func() { a.openSubmodule(list[0]) })
	if readOnDispatcher(t, a, a.State).ActiveRepository != node.ID || len(a.cfg.Repositories) != 2 {
		t.Fatalf("opening again did not reuse the node: %+v", a.cfg.Repositories)
	}
}

func clickSubmoduleMenuItem(t *testing.T, a *App, sub ops.Submodule, key string) []string {
	t.Helper()
	views := captureOperationViews(t)
	runOnDispatcher(t, a, func() { clickMenuItem(t, a.submoduleMenu(sub), key) })
	view := lastOperationView(t, views)
	waitForFinishedOperation(t, a, view)
	return readOnDispatcher(t, a, view.Lines)
}

func TestTheSubmoduleMenuRunsItsActionsForThatSubmodule(t *testing.T) {
	super := buildSuperproject(t)
	cfg := config.Default()
	cfg.Groups = []config.Group{{ID: "g1", Name: "Work"}}
	cfg.Repositories = []config.Repository{{ID: "r1", Name: "Super", Path: super, Group: "g1"}}
	a := newTestAppWithConfig(t, cfg)
	runOnDispatcher(t, a, func() { a.ActivateRepository("r1") })
	list := waitForSubmodules(t, a, submoduleIn(ops.SubmoduleStateNotInitialized))

	if lines := clickSubmoduleMenuItem(t, a, list[0], "Menu.Remote.Submodule.Initialize"); len(lines) == 0 {
		t.Fatal("initialize logged nothing")
	}
	list = waitForSubmodules(t, a, submoduleIn(ops.SubmoduleStateUpToDate))
	if lines := clickSubmoduleMenuItem(t, a, list[0], "Menu.Remote.Submodule.Update"); !slices.Contains(lines, i18n.T("Operation.Log.SubmodulesUpToDate")) {
		t.Fatalf("update log = %q", lines)
	}
	if lines := clickSubmoduleMenuItem(t, a, list[0], "Menu.Remote.Submodule.Synchronize"); len(lines) == 0 {
		t.Fatal("synchronize logged nothing")
	}

	runOnDispatcher(t, a, func() { clickMenuItem(t, a.submoduleMenu(list[0]), "Menu.Context.Open") })

	node, ok := a.registry.FindByPath(filepath.Join(super, "libs", "lib"))
	if parent, found := a.registry.ParentOf(node.ID); !ok || !found || parent.ID != "g1" {
		t.Fatal("the opened submodule did not join the group of its superproject")
	}
}

func TestOpeningASubmoduleReportsRegistryAndConfigFailures(t *testing.T) {
	super := buildSuperproject(t)
	a := activatedWorkingApp(t, super)
	sub := ops.Submodule{Path: "libs/lib", Populated: true}

	prev := addRegistryRepository
	addRegistryRepository = func(*repo.Registry, string, string, string) (*repo.Node, error) { return nil, errors.New("boom") }
	t.Cleanup(func() { addRegistryRepository = prev })
	runOnDispatcher(t, a, func() { a.openSubmodule(sub) })
	if got := readOnDispatcher(t, a, a.statusLabel.Text); !strings.Contains(got, "boom") {
		t.Fatalf("status = %q", got)
	}
	addRegistryRepository = prev

	if err := os.MkdirAll(a.paths.ConfigFile(), 0o700); err != nil {
		t.Fatal(err)
	}
	runOnDispatcher(t, a, func() { a.openSubmodule(sub) })
	if _, ok := a.registry.FindByPath(filepath.Join(super, "libs", "lib")); !ok {
		t.Fatal("a config save failure stopped the submodule from being added")
	}
}

func TestSubmoduleActionsNeedAnOpenRepository(t *testing.T) {
	a := newTestApp(t)
	runOnDispatcher(t, a, func() {
		a.openSubmodule(ops.Submodule{Path: "x", Populated: true})
		a.startSubmoduleSync(nil)
	})
	items := readOnDispatcher(t, a, func() []string { return menuTexts(a.submoduleMenu(ops.Submodule{Path: "x"})) })
	if len(items) != 9 {
		t.Fatalf("menu without a repository = %v", items)
	}
}

func TestOpeningAnUnpopulatedSubmoduleDoesNothing(t *testing.T) {
	super := buildSuperproject(t)
	a := activatedWorkingApp(t, super)
	runOnDispatcher(t, a, func() { a.openSubmodule(ops.Submodule{Path: "libs/lib"}) })
	if len(a.cfg.Repositories) != 1 {
		t.Fatalf("repositories = %+v", a.cfg.Repositories)
	}
}

func TestSubmoduleLoadingFailuresLeaveTheListEmpty(t *testing.T) {
	boom := errors.New("boom")
	loader := submoduleLoader{
		open: func(string, gitrepo.OpenOptions) (*gitrepo.Repository, error) { return nil, boom },
		log:  newTestApp(t).log,
	}
	if list := loader.load(t.TempDir()); list != nil {
		t.Fatalf("list = %+v", list)
	}
	super := buildSuperproject(t)
	loader.open = gitrepo.Open
	loader.list = func(context.Context, *gitrepo.Repository) ([]ops.Submodule, error) { return nil, boom }
	if list := loader.load(super); list != nil {
		t.Fatalf("list = %+v", list)
	}
}

func TestSubmoduleEventsReadLikeGit(t *testing.T) {
	newTestApp(t)
	id := hash.SumSHA1("commit", []byte("x"))
	tests := []struct {
		event ops.SubmoduleEvent
		want  string
	}{
		{ops.SubmoduleEvent{Kind: ops.SubmoduleRegistered, Name: "lib", URL: "u", Path: "p"}, i18n.Tf("Operation.Log.SubmoduleRegistered", "lib", "u", "p")},
		{ops.SubmoduleEvent{Kind: ops.SubmoduleCloning, URL: "u", Path: "p"}, i18n.Tf("Operation.Log.SubmoduleCloning", "u", "p")},
		{ops.SubmoduleEvent{Kind: ops.SubmoduleFetching, Path: "p"}, i18n.Tf("Operation.Log.SubmoduleFetching", "p")},
		{ops.SubmoduleEvent{Kind: ops.SubmoduleFetchRetry, Path: "p", Commit: id}, i18n.Tf("Operation.Log.SubmoduleFetchRetry", "p", id.String())},
		{ops.SubmoduleEvent{Kind: ops.SubmoduleCheckedOut, Path: "p", Commit: id}, i18n.Tf("Operation.Log.SubmoduleCheckedOut", "p", id.String())},
		{ops.SubmoduleEvent{Kind: ops.SubmoduleRebased, Path: "p", Commit: id}, i18n.Tf("Operation.Log.SubmoduleRebased", "p", id.String())},
		{ops.SubmoduleEvent{Kind: ops.SubmoduleMerged, Path: "p", Commit: id}, i18n.Tf("Operation.Log.SubmoduleMerged", "p", id.String())},
		{ops.SubmoduleEvent{Kind: ops.SubmoduleSkipped, Path: "p"}, i18n.Tf("Operation.Log.SubmoduleSkipped", "p")},
		{ops.SubmoduleEvent{Kind: ops.SubmoduleSkippedUnmerged, Path: "p"}, i18n.Tf("Operation.Log.SubmoduleSkippedUnmerged", "p")},
		{ops.SubmoduleEvent{Kind: ops.SubmoduleNotInitialized, Path: "p"}, i18n.Tf("Operation.Log.SubmoduleNotInitialized", "p")},
		{ops.SubmoduleEvent{Kind: ops.SubmoduleSynchronized, Path: "p"}, i18n.Tf("Operation.Log.SubmoduleSynchronized", "p")},
	}
	for _, tc := range tests {
		if got := submoduleEventText(tc.event); got != tc.want || strings.Contains(got, "%!") {
			t.Fatalf("event %d = %q, want %q", tc.event.Kind, got, tc.want)
		}
	}
	if paths := literalPaths([]string{"a*b"}); !slices.Equal(paths, []string{":(literal)a*b"}) {
		t.Fatalf("literalPaths = %v", paths)
	}
}

func TestSubmoduleProtocolFailuresExplainTheSetting(t *testing.T) {
	a := newTestApp(t)
	views := captureOperationViews(t)
	a.RunOperation("Update", func(_ context.Context, reporter OperationReporter) error {
		reportSubmoduleError(reporter, errors.New("other"))
		reportSubmoduleError(reporter, transport.ErrProtocolNotAllowed)
		return nil
	})
	view := lastOperationView(t, views)
	waitForFinishedOperation(t, a, view)
	if lines := readOnDispatcher(t, a, view.Lines); !slices.Equal(lines, []string{i18n.T("Operation.Log.SubmoduleProtocolHint")}) {
		t.Fatalf("lines = %q", lines)
	}
}

func TestAFailedSubmoduleUpdateReachesTheOperationWindow(t *testing.T) {
	super := buildSuperproject(t)
	a := activatedWorkingApp(t, super)
	waitForSubmodules(t, a, submoduleIn(ops.SubmoduleStateNotInitialized))
	prev := openGitRepository
	openGitRepository = func(string, gitrepo.OpenOptions) (*gitrepo.Repository, error) { return nil, errors.New("boom") }
	t.Cleanup(func() { openGitRepository = prev })

	for name, start := range map[string]func(){
		"update": func() { a.startSubmoduleUpdate(nil, false) },
		"sync":   func() { a.startSubmoduleSync(nil) },
	} {
		views := captureOperationViews(t)
		runOnDispatcher(t, a, start)
		view := lastOperationView(t, views)
		waitForFinishedOperation(t, a, view)
		if lines := readOnDispatcher(t, a, view.Lines); !slices.ContainsFunc(lines, func(line string) bool { return strings.Contains(line, "boom") }) {
			t.Fatalf("%s log = %q", name, lines)
		}
	}
}

func TestTheSubmodulesSectionOfTheSideBarShowsTheBranchesPane(t *testing.T) {
	cfg := config.Default()
	a := newTestAppWithConfig(t, cfg)
	runOnDispatcher(t, a, func() { a.showSidebarSubmoduleCount(0) })
	if got, _ := a.sidebar.CountText(sidebar.SectionSubmodules); got != "—" {
		t.Fatalf("count = %q", got)
	}
}
