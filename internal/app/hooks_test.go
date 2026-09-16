package app

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hooks"
	"github.com/oops1/gogit/internal/gitcore/ops"
	"github.com/oops1/gogit/internal/gitcore/refs"
	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/clone"
	"github.com/oops1/gogit/internal/ui/commit"
	pushdialog "github.com/oops1/gogit/internal/ui/push"
)

func TestHookEventsWriteTheOperationLog(t *testing.T) {
	a := newRemoteTestApp(t)
	views := captureOperationViews(t)
	rejection := &hooks.Error{Hook: "pre-push", ExitCode: 1}

	a.RunOperation("Push", func(_ context.Context, reporter OperationReporter) error {
		sink := hookEvents(reporter)
		for _, e := range []hooks.Event{
			{Kind: hooks.EventStarted, Hook: "pre-push"},
			{Kind: hooks.EventOutput, Hook: "pre-push", Line: "checking"},
			{Kind: hooks.EventTruncated, Hook: "pre-push"},
			{Kind: hooks.EventIgnored, Hook: "post-commit"},
			{Kind: hooks.EventNoInterpreter, Hook: "commit-msg", Interpreter: "sh"},
		} {
			sink(e)
		}
		reportHookRejection(reporter, errors.New("not a hook"))
		reportHookRejection(reporter, rejection)
		return rejection
	})

	lines := operationLines(t, a, views)
	for _, want := range []string{
		i18n.Tf("Operation.Log.HookRunning", "pre-push"),
		"checking",
		i18n.Tf("Operation.Log.HookTruncated", "pre-push"),
		i18n.Tf("Operation.Log.HookIgnored", "post-commit"),
		i18n.Tf("Operation.Log.HookNoInterpreter", "commit-msg", "sh"),
		i18n.Tf("Operation.Log.HookRejected", "pre-push"),
	} {
		if !slices.Contains(lines, want) {
			t.Fatalf("log %q misses %q", lines, want)
		}
	}
}

func TestHookFailureTextNamesTheHookAndItsLastLines(t *testing.T) {
	if _, ok := hookFailureText(errors.New("plain")); ok {
		t.Fatal("a plain error is not a hook failure")
	}
	silent, _ := hookFailureText(&hooks.Error{Hook: "pre-commit", ExitCode: 2})
	if silent != i18n.Tf("Status.HookRejected", "pre-commit", i18n.Tf("Status.HookExitStatus", 2)) {
		t.Fatalf("silent = %q", silent)
	}
	chatty, _ := hookFailureText(&hooks.Error{Hook: "commit-msg", ExitCode: 1, Output: []string{"a", "b", "c", "d"}})
	if chatty != i18n.Tf("Status.HookRejected", "commit-msg", "b; c; d") {
		t.Fatalf("chatty = %q", chatty)
	}
}

func commitWithHook(t *testing.T, line string, exit int) (*App, string, []string) {
	t.Helper()
	target := filepath.Join(t.TempDir(), "main")
	buildStagedFileFixture(t, target)
	setTestUserIdentity(t, target)
	writeAppHook(t, target, "pre-commit", line, exit)
	a := activatedWorkingApp(t, target)
	waitForWorkingRows(t, a, 1)
	views := captureOperationViews(t)

	runOnDispatcher(t, a, func() { a.commitFiles(commit.Model{Message: "with hooks"}, true, nil) })

	return a, target, operationLines(t, a, views)
}

func TestACommitWithHooksShowsTheirOutputInTheOperationWindow(t *testing.T) {
	a, target, lines := commitWithHook(t, "checked", 0)

	if !slices.Contains(lines, i18n.Tf("Operation.Log.HookRunning", "pre-commit")) || !slices.Contains(lines, "checked") {
		t.Fatalf("log = %q", lines)
	}
	waitForStatusText(t, a, i18n.Tf("Status.Committed", shortHash(branchTip(t, target, "main"))))
}

func TestARejectingCommitHookIsReportedWithItsOutput(t *testing.T) {
	a, _, lines := commitWithHook(t, "lint failed", 1)

	if !slices.Contains(lines, i18n.Tf("Operation.Log.HookRejected", "pre-commit")) {
		t.Fatalf("log = %q", lines)
	}
	waitForStatusText(t, a, i18n.Tf("Status.HookRejected", "pre-commit", "lint failed"))
}

func capturePushViews(t *testing.T, fail error) *[]*pushdialog.View {
	t.Helper()
	views := &[]*pushdialog.View{}
	prev := newPushView
	newPushView = func() (*pushdialog.View, error) {
		if fail != nil {
			return nil, fail
		}
		view, err := prev()
		if err == nil {
			*views = append(*views, view)
		}
		return view, err
	}
	t.Cleanup(func() { newPushView = prev })
	return views
}

func pushableClone(t *testing.T) (*App, string, string) {
	t.Helper()
	dir := t.TempDir()
	server := filepath.Join(dir, "server")
	local := filepath.Join(dir, "local")
	initRemoteServerRepo(t, server, "main")
	a := newRemoteTestApp(t)
	cloneIntoRegistry(t, a, server, local)
	addRemoteServerCommit(t, local, "main", "local.txt", "local\n")
	writeAppHook(t, local, "pre-push", "push refused", 1)
	return a, server, local
}

func TestThePushDialogCanBypassARejectingPrePushHook(t *testing.T) {
	a, server, local := pushableClone(t)
	pushViews := capturePushViews(t, nil)
	views := captureOperationViews(t)

	runOnDispatcher(t, a, a.handlers[CmdPush])
	view := (*pushViews)[0]
	if got := readOnDispatcher(t, a, func() string { return view.Dialog().Title }); got != i18n.T("Dialog.Push.Title") {
		t.Fatalf("title = %q", got)
	}
	runOnDispatcher(t, a, func() { view.OnOK(true) })
	operationLines(t, a, views)

	got, ok := readRemoteRef(t, server, refs.BranchName("main"))
	if want := branchTip(t, local, "main"); !ok || got != want {
		t.Fatalf("server main = %v, want %v", got, want)
	}
}

func TestAPushRejectedByPrePushIsReported(t *testing.T) {
	a, _, _ := pushableClone(t)
	views := captureOperationViews(t)

	a.startPush()
	lines := operationLines(t, a, views)

	if !slices.Contains(lines, i18n.Tf("Operation.Log.HookRejected", "pre-push")) || !slices.Contains(lines, "push refused") {
		t.Fatalf("log = %q", lines)
	}
}

func TestThePushDialogNeedsARepositoryAndItsView(t *testing.T) {
	a := newRemoteTestApp(t)
	pushViews := capturePushViews(t, nil)
	runOnDispatcher(t, a, a.openPush)
	if len(*pushViews) != 0 {
		t.Fatal("no push dialog without a repository")
	}

	a, _, _ = pushableClone(t)
	runOnDispatcher(t, a, a.openPush)
	if len(*pushViews) != 1 {
		t.Fatalf("push dialogs = %d, want one", len(*pushViews))
	}
	runOnDispatcher(t, a, (*pushViews)[0].Dialog().CancelAction)

	failing := capturePushViews(t, errors.New("no dialog"))
	runOnDispatcher(t, a, a.openPush)
	if len(*failing) != 0 {
		t.Fatal("a dialog that cannot load must not be shown")
	}
}

func stubClone(t *testing.T, result func(dir string) (*gitrepo.Repository, error)) {
	t.Helper()
	prev := cloneRepository
	cloneRepository = func(_ context.Context, _, dir string, _ ops.CloneOptions) (*gitrepo.Repository, error) {
		return result(dir)
	}
	t.Cleanup(func() { cloneRepository = prev })
}

func TestACloneWithAFailingPostCheckoutHookIsKept(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "cloned")
	initRemoteServerRepo(t, dest, "main")
	stubClone(t, func(dir string) (*gitrepo.Repository, error) {
		r, err := gitrepo.Open(dir, gitrepo.OpenOptions{})
		if err != nil {
			t.Fatal(err)
		}
		return r, &hooks.Error{Hook: "post-checkout", ExitCode: 1}
	})
	a := newRemoteTestApp(t)
	views := captureOperationViews(t)

	a.startClone(clone.Result{URL: "unused", Directory: dest})
	lines := operationLines(t, a, views)

	if !slices.Contains(lines, i18n.Tf("Operation.Log.Cloned", dest)) || !slices.Contains(lines, i18n.Tf("Operation.Log.HookRejected", "post-checkout")) {
		t.Fatalf("log = %q", lines)
	}
}
