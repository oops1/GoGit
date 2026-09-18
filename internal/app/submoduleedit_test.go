package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/gitcore/ops"
	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/operation"
	"github.com/oops1/gogit/internal/ui/submoduleadd"
)

func captureSubmoduleAddViews(t *testing.T) *[]*submoduleadd.View {
	t.Helper()
	views := &[]*submoduleadd.View{}
	prev := newSubmoduleAddView
	newSubmoduleAddView = func() (*submoduleadd.View, error) {
		view, err := prev()
		if err == nil {
			*views = append(*views, view)
		}
		return view, err
	}
	t.Cleanup(func() { newSubmoduleAddView = prev })
	return views
}

func finishedLines(t *testing.T, a *App, views *[]*operation.View, start func()) []string {
	t.Helper()
	before := len(*views)
	runOnDispatcher(t, a, start)
	if len(*views) == before {
		t.Fatal("no operation window was opened")
	}
	view := lastOperationView(t, views)
	waitForFinishedOperation(t, a, view)
	return readOnDispatcher(t, a, view.Lines)
}

func initializedSuperproject(t *testing.T) (*App, string) {
	t.Helper()
	super := buildSuperproject(t)
	a := activatedWorkingApp(t, super)
	waitForSubmodules(t, a, submoduleIn(ops.SubmoduleStateNotInitialized))
	runSubmoduleCommand(t, a, CmdSubmoduleInitialize)
	waitForSubmodules(t, a, submoduleIn(ops.SubmoduleStateUpToDate))
	return a, super
}

func TestAddingASubmoduleThroughTheDialog(t *testing.T) {
	super := buildSuperproject(t)
	a := activatedWorkingApp(t, super)
	waitForSubmodules(t, a, submoduleIn(ops.SubmoduleStateNotInitialized))
	dialogs := captureSubmoduleAddViews(t)
	views := captureOperationViews(t)
	lib := filepath.ToSlash(filepath.Join(filepath.Dir(super), "lib"))

	runOnDispatcher(t, a, func() { a.Dispatch(CmdSubmoduleAdd) })
	if len(*dialogs) != 1 {
		t.Fatal("the add command did not open the dialog")
	}
	runOnDispatcher(t, a, func() { (*dialogs)[0].Dialog().CancelAction() })

	runOnDispatcher(t, a, func() { a.Dispatch(CmdSubmoduleAdd) })
	lines := finishedLines(t, a, views, func() { (*dialogs)[1].OnOK(submoduleadd.Model{URL: lib, Path: "deps/copy", Branch: "main"}) })

	if !slices.Contains(lines, i18n.Tf("Operation.Log.SubmoduleAdded", "deps/copy", "deps/copy")) {
		t.Fatalf("add log = %q", lines)
	}
	waitForSubmodules(t, a, func(list []ops.Submodule) bool { return len(list) == 2 })

	lines = finishedLines(t, a, views, func() { a.startSubmoduleAdd(submoduleadd.Model{URL: "lib", Path: "bad"}) })
	if !slices.ContainsFunc(lines, func(line string) bool { return strings.Contains(line, "absolute") }) {
		t.Fatalf("a refused add logged %q", lines)
	}
}

func TestTheAddDialogNeedsARepositoryAndReportsLoadFailures(t *testing.T) {
	a := newTestApp(t)
	dialogs := captureSubmoduleAddViews(t)
	runOnDispatcher(t, a, a.openSubmoduleAdd)
	if len(*dialogs) != 0 {
		t.Fatal("the dialog opened without a repository")
	}

	super := buildSuperproject(t)
	a = activatedWorkingApp(t, super)
	prev := newSubmoduleAddView
	newSubmoduleAddView = func() (*submoduleadd.View, error) { return nil, errors.New("boom") }
	t.Cleanup(func() { newSubmoduleAddView = prev })
	runOnDispatcher(t, a, a.openSubmoduleAdd)
}

func TestResettingASubmoduleRestoresTheRecordedCommit(t *testing.T) {
	a, super := initializedSuperproject(t)
	list := waitForSubmodules(t, a, submoduleIn(ops.SubmoduleStateUpToDate))
	file := filepath.Join(super, "libs", "lib", "lib.txt")
	if err := os.WriteFile(file, []byte("changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	answers := 0
	a.askConfirm = func(_, _ string, cb func(bool)) { answers++; cb(answers > 1) }
	views := captureOperationViews(t)

	runOnDispatcher(t, a, func() { clickMenuItem(t, a.submoduleMenu(list[0]), "Menu.Remote.Submodule.Reset") })
	if len(*views) != 0 {
		t.Fatal("a declined reset started an operation")
	}
	lines := finishedLines(t, a, views, func() { clickMenuItem(t, a.submoduleMenu(list[0]), "Menu.Remote.Submodule.Reset") })

	if data, err := os.ReadFile(file); err != nil || string(data) != "lib\n" || !slices.Contains(lines, i18n.Tf("Operation.Log.SubmoduleCheckedOut", "libs/lib", list[0].Recorded.String())) {
		t.Fatalf("file = %q, %v; log = %q", data, err, lines)
	}
}

func TestUnregisteringAModifiedSubmoduleAsksToDiscardTheChanges(t *testing.T) {
	a, super := initializedSuperproject(t)
	list := waitForSubmodules(t, a, submoduleIn(ops.SubmoduleStateUpToDate))
	if err := os.WriteFile(filepath.Join(super, "libs", "lib", "lib.txt"), []byte("changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var questions []string
	a.askConfirm = func(title, _ string, cb func(bool)) {
		questions = append(questions, title)
		cb(true)
	}
	views := captureOperationViews(t)

	lines := finishedLines(t, a, views, func() { clickMenuItem(t, a.submoduleMenu(list[0]), "Menu.Remote.Submodule.Unregister") })
	if !slices.Contains(lines, i18n.Tf("Operation.Log.SubmoduleLocalChanges", "libs/lib")) {
		t.Fatalf("first attempt log = %q", lines)
	}
	waitForSubmodules(t, a, submoduleIn(ops.SubmoduleStateNotInitialized))
	second := lastOperationView(t, views)
	waitForFinishedOperation(t, a, second)
	lines = readOnDispatcher(t, a, second.Lines)

	if !slices.Contains(lines, i18n.Tf("Operation.Log.SubmoduleCleared", "libs/lib")) || !slices.Equal(questions, []string{i18n.T("Dialog.SubmoduleUnregister.Title"), i18n.T("Dialog.SubmoduleForce.Title")}) {
		t.Fatalf("log = %q, questions = %q", lines, questions)
	}
}

func TestRemovingTheSelectedSubmoduleFromTheMainMenu(t *testing.T) {
	a, _ := initializedSuperproject(t)
	a.askConfirm = func(_, _ string, cb func(bool)) { cb(true) }

	for _, id := range []CommandID{CmdSubmoduleRemove, CmdSubmoduleUnregister, CmdSubmoduleReset} {
		runOnDispatcher(t, a, func() {
			a.statusLabel.SetText("")
			a.Dispatch(id)
		})
		if got := readOnDispatcher(t, a, a.statusLabel.Text); got != i18n.T("Status.SubmoduleNotSelected") {
			t.Fatalf("%s status = %q", id, got)
		}
	}

	views := captureOperationViews(t)
	lines := finishedLines(t, a, views, func() {
		item, ok := a.branchesView.SubmoduleItem("libs/lib")
		if !ok {
			t.Error("the submodule node is missing")
			return
		}
		a.Widget("branchesTree").(*widget.TreeViewWidget).Tree.SetSelectedItem(item)
		a.Dispatch(CmdSubmoduleRemove)
	})

	if !slices.Contains(lines, i18n.Tf("Operation.Log.SubmoduleRemoved", "libs/lib")) {
		t.Fatalf("remove log = %q", lines)
	}
	waitForSubmodules(t, a, func(list []ops.Submodule) bool { return len(list) == 0 })
}

func TestSubmoduleRemovalNeedsAnOpenRepositoryAndReportsFailures(t *testing.T) {
	a := newTestApp(t)
	runOnDispatcher(t, a, func() { a.startSubmoduleRemove(ops.Submodule{Path: "x"}, false) })

	super := buildSuperproject(t)
	a = activatedWorkingApp(t, super)
	waitForSubmodules(t, a, submoduleIn(ops.SubmoduleStateNotInitialized))
	a.askConfirm = func(_, _ string, cb func(bool)) { cb(false) }
	for _, key := range []string{"Menu.Remote.Submodule.Remove", "Menu.Remote.Submodule.Unregister"} {
		for _, item := range readOnDispatcher(t, a, func() []widget.MenuItem {
			return a.submoduleMenu(ops.Submodule{Path: "libs/lib", Active: true, Populated: true})
		}) {
			if item.Text == i18n.T(key) {
				runOnDispatcher(t, a, item.OnClick)
			}
		}
	}

	prev := openGitRepository
	openGitRepository = func(string, gitrepo.OpenOptions) (*gitrepo.Repository, error) { return nil, errors.New("boom") }
	t.Cleanup(func() { openGitRepository = prev })
	views := captureOperationViews(t)
	for name, start := range map[string]func(){
		"unregister": func() { a.startSubmoduleUnregister(ops.Submodule{Path: "libs/lib"}, false) },
		"reset":      func() { a.startSubmoduleReset(ops.Submodule{Path: "libs/lib"}) },
		"add":        func() { a.startSubmoduleAdd(submoduleadd.Model{URL: "../lib"}) },
		"remove":     func() { a.startSubmoduleRemove(ops.Submodule{Path: "libs/lib"}, true) },
	} {
		if lines := finishedLines(t, a, views, start); !slices.ContainsFunc(lines, func(line string) bool { return strings.Contains(line, "boom") }) {
			t.Fatalf("%s log = %q", name, lines)
		}
	}
}

func TestFetchLogsTheSubmodulesItFetches(t *testing.T) {
	a := newTestApp(t)
	views := captureOperationViews(t)
	a.RunOperation("Fetch", func(_ context.Context, reporter OperationReporter) error {
		submoduleEventLog(reporter, new(int))(ops.SubmoduleEvent{Kind: ops.SubmoduleCommandRefused, Path: "p", Command: "make"})
		submoduleEventLog(reporter, new(int))(ops.SubmoduleEvent{Kind: ops.SubmoduleUnregistered, Path: "p", Name: "n", URL: "u"})
		return nil
	})
	view := lastOperationView(t, views)
	waitForFinishedOperation(t, a, view)
	want := []string{i18n.Tf("Operation.Log.SubmoduleCommandRefused", "p", "make"), i18n.Tf("Operation.Log.SubmoduleUnregistered", "n", "u", "p")}
	if lines := readOnDispatcher(t, a, view.Lines); !slices.Equal(lines, want) {
		t.Fatalf("lines = %q", lines)
	}
}
