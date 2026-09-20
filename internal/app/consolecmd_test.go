package app

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/config"
	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/console"
)

func newConsoleTestApp(t *testing.T) (*App, string) {
	t.Helper()
	target := filepath.Join(t.TempDir(), "repo")
	initTestRepoWithBranch(t, target, "main")
	setTestUserIdentity(t, target)
	cfg := config.Default()
	cfg.Repositories = []config.Repository{{ID: "r1", Name: "Repo", Path: target}}
	a := newTestAppWithConfig(t, cfg)
	a.ActivateRepository("r1")
	return a, target
}

func captureConsoleView(t *testing.T) **console.View {
	t.Helper()
	captured := new(*console.View)
	prev := newConsoleView
	newConsoleView = func() (*console.View, error) {
		view, err := prev()
		*captured = view
		return view, err
	}
	t.Cleanup(func() { newConsoleView = prev })
	return captured
}

func waitForConsoleAnswer(t *testing.T, a *App, view *console.View) []string {
	t.Helper()
	deadline := time.Now().Add(testTimeout)
	for readOnDispatcher(t, a, view.Busy) {
		if time.Now().After(deadline) {
			t.Fatal("the console command did not finish in time")
		}
		time.Sleep(10 * time.Millisecond)
	}
	return readOnDispatcher(t, a, view.Texts)
}

func TestTheToolsMenuHoldsTheConsole(t *testing.T) {
	a := newTestApp(t)

	text, enabled, ok := a.MenuItemByCommand(CmdConsole)

	if !ok || enabled {
		t.Fatalf("console item: found = %v, enabled = %v", ok, enabled)
	}
	if text != widget.Tr("Menu.Tools.Console") {
		t.Fatalf("text = %q", text)
	}
	if a.menu.Items()[toolsMenuIndex].Text != widget.Tr("Menu.Tools") {
		t.Fatalf("tools menu = %q", a.menu.Items()[toolsMenuIndex].Text)
	}
}

func TestTheConsoleWakesUpWithARepository(t *testing.T) {
	a, _ := newConsoleTestApp(t)

	if _, enabled, _ := a.MenuItemByCommand(CmdConsole); !enabled {
		t.Fatal("the console must be available once a repository is open")
	}
}

func TestDispatchingTheConsoleOpensTheWindow(t *testing.T) {
	captured := captureConsoleView(t)
	a, _ := newConsoleTestApp(t)

	if !a.Dispatch(CmdConsole) {
		t.Fatal("the console command must run")
	}
	if *captured == nil || !(*captured).Dialog().IsModal() {
		t.Fatal("the console window must be shown")
	}
	(*captured).Dialog().CancelAction()
	if (*captured).Dialog().IsModal() {
		t.Fatal("closing the console must take the window down")
	}
}

func TestTheFunctionKeyOpensTheConsole(t *testing.T) {
	captured := captureConsoleView(t)
	a, _ := newConsoleTestApp(t)

	index := slices.IndexFunc(a.Root().InputBindings, func(b widget.InputBinding) bool { return b.Key == widget.KeyF9 })
	if index < 0 {
		t.Fatal("F9 must open the console")
	}
	a.Root().InputBindings[index].Command.Execute(nil)

	if *captured == nil {
		t.Fatal("the console window must be shown")
	}
}

func TestTheConsoleRunsACommandAndShowsTheAnswer(t *testing.T) {
	captured := captureConsoleView(t)
	a, target := newConsoleTestApp(t)
	if err := writeFile(target, "a.txt", "a\n"); err != nil {
		t.Fatal(err)
	}
	a.Dispatch(CmdConsole)
	view := *captured

	view.SetText("status --porcelain")
	view.Submit()

	got := waitForConsoleAnswer(t, a, view)
	if !slices.Equal(got, []string{"> status --porcelain", "?? a.txt"}) {
		t.Fatalf("lines = %#v", got)
	}
}

func TestTheConsoleShowsWhyACommandFailed(t *testing.T) {
	captured := captureConsoleView(t)
	a, _ := newConsoleTestApp(t)
	a.Dispatch(CmdConsole)
	view := *captured

	view.SetText("frobnicate")
	view.Submit()

	got := waitForConsoleAnswer(t, a, view)
	if len(got) != 2 || got[1] != i18n.Tf("Console.Error.UnknownCommand", "frobnicate") {
		t.Fatalf("lines = %#v", got)
	}
}

func TestAFailedConsoleWindowLandsInTheStatusLine(t *testing.T) {
	a := newTestApp(t)
	wantErr := errors.New("boom")
	prev := newConsoleView
	newConsoleView = func() (*console.View, error) { return nil, wantErr }
	t.Cleanup(func() { newConsoleView = prev })

	a.openConsole()

	if got := a.statusLabel.Text(); got != i18n.Tf("Status.ConsoleFailed", wantErr) {
		t.Fatalf("status = %q", got)
	}
}

func TestTheConsoleAsksForARepositoryOnceItIsClosed(t *testing.T) {
	captured := captureConsoleView(t)
	a, _ := newConsoleTestApp(t)
	a.Dispatch(CmdConsole)
	view := *captured
	a.CloseRepository()

	a.runConsoleCommand(view, "status")

	if got := view.Texts(); len(got) != 1 || got[0] != i18n.T("Console.Error.NoRepository") {
		t.Fatalf("lines = %#v", got)
	}
}

func TestTheConsoleWaitsForTheRunningOperation(t *testing.T) {
	captured := captureConsoleView(t)
	a, _ := newConsoleTestApp(t)
	a.Dispatch(CmdConsole)
	view := *captured
	release := make(chan struct{})
	a.startWrite(func(context.Context, *gitrepo.Repository) error {
		<-release
		return nil
	}, func(error) {})
	t.Cleanup(func() { close(release) })

	a.runConsoleCommand(view, "status")

	if got := view.Texts(); len(got) != 1 || got[0] != i18n.T("Console.Error.Busy") {
		t.Fatalf("lines = %#v", got)
	}
}

func TestTheConsoleCarriesTheTransportSettings(t *testing.T) {
	captured := captureConsoleView(t)
	a, _ := newConsoleTestApp(t)
	a.Dispatch(CmdConsole)
	view := *captured
	agent := make(chan string, 1)
	prev := runConsoleLine
	runConsoleLine = func(_ context.Context, env console.Env, line string) (string, error) {
		agent <- env.Transport(nil).UserAgent
		return strings.ToUpper(line), nil
	}
	t.Cleanup(func() { runConsoleLine = prev })

	view.SetText("status")
	view.Submit()

	got := waitForConsoleAnswer(t, a, view)
	if !slices.Equal(got, []string{"> status", "STATUS"}) {
		t.Fatalf("lines = %#v", got)
	}
	if used := <-agent; used != remoteUserAgent {
		t.Fatalf("user agent = %q, want %q", used, remoteUserAgent)
	}
}
