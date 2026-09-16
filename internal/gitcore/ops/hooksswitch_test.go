package ops

import (
	"errors"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hooks"
)

func TestSwitchRunsPostCheckoutWithTheOldAndNewCommits(t *testing.T) {
	tr := divergedTopic(t)
	main, topic := tr.branchTarget("main"), tr.branchTarget("topic")
	log := hookLogPath(t)
	installHooks(t, tr, testHook{log: log}, hookPostCheckout)

	if err := Switch(t.Context(), tr.repo, "topic", SwitchOptions{}); err != nil {
		t.Fatalf("Switch returned error %v", err)
	}
	if err := Switch(t.Context(), tr.repo, main.String(), SwitchOptions{}); err != nil {
		t.Fatalf("Switch returned error %v", err)
	}
	if _, err := StartBranch(t.Context(), tr.repo, "fresh", "", StartBranchOptions{}); err != nil {
		t.Fatalf("StartBranch returned error %v", err)
	}

	want := []hookRecord{
		{name: hookPostCheckout, args: []string{main.String(), topic.String(), "1"}},
		{name: hookPostCheckout, args: []string{topic.String(), main.String(), "1"}},
		{name: hookPostCheckout, args: []string{main.String(), main.String(), "1"}},
	}
	if got := readHookLog(t, log); !recordsEqual(got, want) {
		t.Fatalf("hooks = %+v, want %+v", got, want)
	}
}

func TestAFailingPostCheckoutHookFailsTheFinishedSwitch(t *testing.T) {
	tr := divergedTopic(t)
	installHooks(t, tr, testHook{exit: 2}, hookPostCheckout)

	err := Switch(t.Context(), tr.repo, "topic", SwitchOptions{})

	var hookErr *hooks.Error
	if !errors.As(err, &hookErr) || hookErr.ExitCode != 2 {
		t.Fatalf("Switch returned %v, want the post-checkout status", err)
	}
	if head, _ := tr.headSymbolicTarget(); head.Short() != "topic" {
		t.Fatalf("HEAD = %s, want the switch to be done", head)
	}
}
