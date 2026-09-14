package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

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

func operationNumber(t *testing.T, a *App, views *[]*operation.View, number int) []string {
	t.Helper()
	deadline := time.Now().Add(testTimeout)
	var view *operation.View
	for view == nil {
		view = readOnDispatcher(t, a, func() *operation.View {
			if len(*views) < number {
				return nil
			}
			return (*views)[number-1]
		})
		if view == nil && time.Now().After(deadline) {
			t.Fatalf("operation %d did not start in time", number)
		}
		if view == nil {
			time.Sleep(10 * time.Millisecond)
		}
	}
	waitForFinishedOperation(t, a, view)
	waitForPostQueueDrain(t, a)
	return readOnDispatcher(t, a, view.Lines)
}

type offerScript struct {
	mu       sync.Mutex
	finished ops.FinishFlowResult
	pushErr  error
	answer   bool
	pushed   []ops.FinishFlowOptions
}

func (s *offerScript) set(change func(*offerScript)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	change(s)
}

func (s *offerScript) pushes() []ops.FinishFlowOptions {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.pushed)
}

func TestFinishingWithoutPushOffersToSendTheResult(t *testing.T) {
	a, target := flowReadyApp(t, true)
	addOriginRemote(t, target)
	views := captureOperationViews(t)
	script := &offerScript{}
	prevFinish, prevPush := runFinishFlow, runPushFinishedFlow
	t.Cleanup(func() { runFinishFlow, runPushFinishedFlow = prevFinish, prevPush })
	runFinishFlow = func(context.Context, *gitrepo.Repository, string, string, ops.FinishFlowOptions) (ops.FinishFlowResult, error) {
		script.mu.Lock()
		defer script.mu.Unlock()
		return script.finished, nil
	}
	runPushFinishedFlow = func(_ context.Context, _ *gitrepo.Repository, _, _ string, opts ops.FinishFlowOptions) error {
		script.mu.Lock()
		defer script.mu.Unlock()
		script.pushed = append(script.pushed, opts)
		return script.pushErr
	}
	runOnDispatcher(t, a, func() {
		a.askConfirm = func(_, _ string, cb func(bool)) {
			script.mu.Lock()
			answer := script.answer
			script.mu.Unlock()
			cb(answer)
		}
	})
	started := readOnDispatcher(t, a, func() int { return len(*views) })
	finish := func() []string {
		runOnDispatcher(t, a, func() { a.finishFlow(ops.FlowKindRelease, "1.0", flow.FinishModel{Message: "Finish 1.0"}) })
		started++
		return operationNumber(t, a, views, started)
	}

	lines := finish()
	if !slices.Contains(lines, i18n.Tf("Operation.Log.FlowNotPushed", "1.0")) || len(script.pushes()) != 0 {
		t.Fatalf("declined offer: log = %v, pushes = %d", lines, len(script.pushes()))
	}

	script.set(func(s *offerScript) { s.answer = true })
	finish()
	started++
	lines = operationNumber(t, a, views, started)
	pushed := script.pushes()
	if !slices.Contains(lines, i18n.Tf("Operation.Log.FlowPushed", "1.0")) || len(pushed) != 1 || pushed[0].Network.Remote != "origin" || pushed[0].Message != "Finish 1.0" {
		t.Fatalf("accepted offer: log = %v, pushes = %+v", lines, pushed)
	}

	script.set(func(s *offerScript) { s.pushErr = errors.New("offline") })
	finish()
	started++
	if lines := operationNumber(t, a, views, started); slices.Contains(lines, i18n.Tf("Operation.Log.FlowPushed", "1.0")) {
		t.Fatalf("a failed push was reported as sent: %v", lines)
	}

	script.set(func(s *offerScript) { s.finished = ops.FinishFlowResult{Pushed: true} })
	if lines := finish(); slices.Contains(lines, i18n.Tf("Operation.Log.FlowNotPushed", "1.0")) {
		t.Fatalf("a pushed finish still offered to push: %v", lines)
	}
}
