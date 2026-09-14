package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/ops"
	"github.com/oops1/gogit/internal/gitcore/refs"
	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/flow"
	"github.com/oops1/gogit/internal/ui/operation"
)

func tagHead(t *testing.T, dir, name string) {
	t.Helper()
	r := openRepoAt(t, dir)
	if _, err := ops.CreateTag(t.Context(), r, name, "HEAD", ops.CreateTagOptions{Message: name}); err != nil {
		t.Fatal(err)
	}
}

func pushAndRead(t *testing.T, a *App, views *[]*operation.View) []string {
	t.Helper()
	a.startPush()
	view := lastOperationView(t, views)
	waitForFinishedOperation(t, a, view)
	return readOnDispatcher(t, a, view.Lines)
}

func TestPushSendsTheTagsOfThePushedCommits(t *testing.T) {
	dir := t.TempDir()
	server := filepath.Join(dir, "server")
	local := filepath.Join(dir, "local")
	initRemoteServerRepo(t, server, "main")
	a := newRemoteTestApp(t)
	views := captureOperationViews(t)
	cloneIntoRegistry(t, a, server, local)
	setTestUserIdentity(t, local)
	addRemoteServerCommit(t, local, "main", "local.txt", "local\n")
	tagHead(t, local, "v1")

	lines := pushAndRead(t, a, views)
	if !slices.Contains(lines, i18n.Tf("Operation.Log.PushedTags", "v1")) {
		t.Fatalf("log = %v", lines)
	}
	if _, ok := readRemoteRef(t, server, refs.TagName("v1")); !ok {
		t.Fatal("the tag did not reach the server")
	}

	tagHead(t, local, "v2")
	lines = pushAndRead(t, a, views)
	if !slices.Contains(lines, i18n.Tf("Operation.Log.PushedTags", "v2")) || slices.Contains(lines, i18n.T("Operation.Log.UpToDate")) {
		t.Fatalf("log for a tag on an up-to-date branch = %v", lines)
	}
}

func addOriginRemote(t *testing.T, target string) {
	t.Helper()
	file, err := os.OpenFile(filepath.Join(target, ".git", "config"), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	if _, err := file.WriteString("[remote \"origin\"]\n\turl = " + filepath.ToSlash(t.TempDir()) + "\n"); err != nil {
		t.Fatal(err)
	}
}

func TestFinishingWithoutPushOffersToSendTheResult(t *testing.T) {
	a, target := flowReadyApp(t, true)
	addOriginRemote(t, target)
	operations := captureOperationViews(t)
	prevFinish, prevPush := runFinishFlow, runPushFinishedFlow
	t.Cleanup(func() { runFinishFlow, runPushFinishedFlow = prevFinish, prevPush })
	finished := ops.FinishFlowResult{}
	runFinishFlow = func(context.Context, *gitrepo.Repository, string, string, ops.FinishFlowOptions) (ops.FinishFlowResult, error) {
		return finished, nil
	}
	var pushed []ops.FinishFlowOptions
	var pushErr error
	runPushFinishedFlow = func(_ context.Context, _ *gitrepo.Repository, _, _ string, opts ops.FinishFlowOptions) error {
		pushed = append(pushed, opts)
		return pushErr
	}
	answer := false
	runOnDispatcher(t, a, func() { a.askConfirm = func(_, _ string, cb func(bool)) { cb(answer) } })
	finish := func() []string {
		runOnDispatcher(t, a, func() { a.finishFlow(ops.FlowKindRelease, "1.0", flow.FinishModel{Message: "Finish 1.0"}) })
		return operationLines(t, a, operations)
	}

	lines := finish()
	if !slices.Contains(lines, i18n.Tf("Operation.Log.FlowNotPushed", "1.0")) || len(pushed) != 0 {
		t.Fatalf("declined offer: log = %v, pushes = %d", lines, len(pushed))
	}

	answer = true
	finish()
	lines = operationLines(t, a, operations)
	if !slices.Contains(lines, i18n.Tf("Operation.Log.FlowPushed", "1.0")) || len(pushed) != 1 || pushed[0].Network.Remote != "origin" || pushed[0].Message != "Finish 1.0" {
		t.Fatalf("accepted offer: log = %v, pushes = %+v", lines, pushed)
	}

	pushErr = errors.New("offline")
	finish()
	if lines := operationLines(t, a, operations); slices.Contains(lines, i18n.Tf("Operation.Log.FlowPushed", "1.0")) {
		t.Fatalf("a failed push was reported as sent: %v", lines)
	}

	finished = ops.FinishFlowResult{Pushed: true}
	if lines := finish(); slices.Contains(lines, i18n.Tf("Operation.Log.FlowNotPushed", "1.0")) {
		t.Fatalf("a pushed finish still offered to push: %v", lines)
	}
}
