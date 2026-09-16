package ops

import (
	"errors"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hooks"
	"github.com/oops1/gogit/internal/gitcore/remote"
)

func pushWithHook(t *testing.T, hook testHook) (*testRepo, *testRepo) {
	t.Helper()
	src := newFetchServer(t)
	src.createBranch("old", src.branchTarget("main"))
	client := cloneForFetch(t, src)
	client.writeFile("c.txt", "new\n")
	mustStage(t, client, "c.txt")
	commit := client.commitAll("client commit")
	client.createBranch("topic", commit)
	installHooks(t, client, hook, hookPrePush)
	return src, client
}

var hookPushSpecs = []string{"refs/heads/topic:refs/heads/topic", ":refs/heads/old", "refs/heads/main:refs/heads/main"}

func TestPrePushReceivesTheRemoteAndTheUpdatesInGitOrder(t *testing.T) {
	log := hookLogPath(t)
	src, client := pushWithHook(t, testHook{log: log, stdin: true})
	base, commit := src.branchTarget("main"), client.branchTarget("main")

	if _, err := Push(t.Context(), client.repo, "origin", remote.PushOptions{Refspecs: mustPushSpecs(t, hookPushSpecs...)}); err != nil {
		t.Fatalf("Push returned error %v", err)
	}

	zero := strings.Repeat("0", 40)
	want := []hookRecord{{
		name: hookPrePush,
		args: []string{"origin", src.dir},
		stdin: []string{
			"refs/heads/main " + commit.String() + " refs/heads/main " + base.String(),
			"(delete) " + zero + " refs/heads/old " + base.String(),
			"refs/heads/topic " + commit.String() + " refs/heads/topic " + zero,
		},
	}}
	if got := readHookLog(t, log); !recordsEqual(got, want) {
		t.Fatalf("hooks = %+v, want %+v", got, want)
	}
}

func TestARejectingPrePushHookSendsNothing(t *testing.T) {
	src, client := pushWithHook(t, testHook{exit: 1})
	base := src.branchTarget("main")

	_, err := PushWithHooks(t.Context(), client.repo, "origin", remote.PushOptions{Refspecs: mustPushSpecs(t, hookPushSpecs...)}, HookOptions{})

	if !errors.Is(err, hooks.ErrRejected) {
		t.Fatalf("Push returned %v, want the pre-push rejection", err)
	}
	if got := src.branchTarget("main"); got != base {
		t.Fatalf("server main moved to %s", got)
	}
}

func TestNoVerifySkipsPrePush(t *testing.T) {
	log := hookLogPath(t)
	src, client := pushWithHook(t, testHook{log: log, exit: 1})

	_, err := PushWithHooks(t.Context(), client.repo, "origin", remote.PushOptions{Refspecs: mustPushSpecs(t, hookPushSpecs...)}, HookOptions{NoVerify: true})

	if err != nil || src.branchTarget("main") != client.branchTarget("main") {
		t.Fatalf("Push returned %v", err)
	}
	if got := readHookLog(t, log); len(got) != 0 {
		t.Fatalf("hooks = %+v, want none", got)
	}
}
