package ops

import (
	"errors"
	"slices"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hooks"
	"github.com/oops1/gogit/internal/gitcore/refs"
)

func topicToRebase(t *testing.T) *testRepo {
	t.Helper()
	tr := divergedTopic(t)
	if err := Switch(t.Context(), tr.repo, "topic", SwitchOptions{}); err != nil {
		t.Fatalf("Switch returned error %v", err)
	}
	return tr
}

func TestRebaseRunsPreRebasePostCheckoutAndPostRewrite(t *testing.T) {
	tr := topicToRebase(t)
	old, onto := tr.branchTarget("topic"), tr.branchTarget("main")
	log := hookLogPath(t)
	installHooks(t, tr, testHook{log: log}, hookPreRebase, hookPostCheckout)
	installHooks(t, tr, testHook{log: log, stdin: true}, hookPostRewrite)

	result, err := Rebase(t.Context(), tr.repo, "main", RebaseOptions{})
	if err != nil || !result.Finished() {
		t.Fatalf("Rebase = %+v, %v", result, err)
	}

	want := []hookRecord{
		{name: hookPreRebase, args: []string{"main"}},
		{name: hookPostCheckout, args: []string{old.String(), onto.String(), "1"}},
		{name: hookPostRewrite, args: []string{"rebase"}, stdin: []string{old.String() + " " + result.New.String()}},
	}
	if got := readHookLog(t, log); !recordsEqual(got, want) {
		t.Fatalf("hooks = %+v, want %+v", got, want)
	}
}

func TestARejectingPreRebaseHookLeavesTheBranchAlone(t *testing.T) {
	tr := topicToRebase(t)
	old := tr.branchTarget("topic")
	installHooks(t, tr, testHook{exit: 1}, hookPreRebase)

	if _, err := Rebase(t.Context(), tr.repo, "main", RebaseOptions{}); !errors.Is(err, hooks.ErrRejected) {
		t.Fatalf("Rebase returned %v, want the pre-rebase rejection", err)
	}
	if got := tr.branchTarget("topic"); got != old {
		t.Fatalf("topic moved to %s", got)
	}
	if head, attached := tr.headSymbolicTarget(); !attached || head != refs.BranchName("topic") {
		t.Fatalf("HEAD = %s, attached %v", head, attached)
	}
	if state, err := ReadRebaseState(tr.repo); err != nil || state.InProgress() {
		t.Fatalf("rebase state = %+v, %v", state, err)
	}
}

func TestNoVerifySkipsPreRebaseAndAnUpToDateRebaseRunsNoHooks(t *testing.T) {
	tr := topicToRebase(t)
	log := hookLogPath(t)
	installHooks(t, tr, testHook{log: log, exit: 1}, hookPreRebase)

	if _, err := Rebase(t.Context(), tr.repo, "main", RebaseOptions{Hooks: HookOptions{NoVerify: true}}); err != nil {
		t.Fatalf("Rebase returned error %v", err)
	}
	result, err := Rebase(t.Context(), tr.repo, "main", RebaseOptions{})
	if err != nil || !result.UpToDate {
		t.Fatalf("second Rebase = %+v, %v", result, err)
	}
	if got := readHookLog(t, log); len(got) != 0 {
		t.Fatalf("hooks = %+v, want none", got)
	}
}

func TestPullWithRebaseGivesPreRebaseTheUpstreamCommitAndIgnoresNoVerify(t *testing.T) {
	src, client := divergedPull(t, "[pull]\n\trebase = true\n")
	upstream := src.branchTarget("main")
	log := hookLogPath(t)
	installHooks(t, client, testHook{log: log}, hookPreRebase)

	if _, err := Pull(t.Context(), client.repo, PullOptions{Hooks: HookOptions{NoVerify: true}}); err != nil {
		t.Fatalf("Pull returned error %v", err)
	}
	if got := readHookLog(t, log); len(got) != 1 || !slices.Equal(got[0].args, []string{upstream.String()}) {
		t.Fatalf("hooks = %+v, want pre-rebase %s", got, upstream)
	}
}
