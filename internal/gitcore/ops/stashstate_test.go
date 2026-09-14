package ops

import (
	"errors"
	"strings"
	"testing"
)

func TestStashPushEndsTheMergeWaitingForItsCommit(t *testing.T) {
	tr := newTestRepo(t)
	tr.fork(map[string]string{"f": changeLine(tenLines("f"), 0, "OURS")}, map[string]string{"g": "theirs\n"})
	if _, err := tr.merge("feature", MergeOptions{NoCommit: true}); err != nil {
		t.Fatal(err)
	}
	head := tr.branchTarget("main")
	tr.writeFile(".git/MERGE_RR", "")

	if _, err := StashPush(t.Context(), tr.repo, StashOptions{When: mergeTime}); err != nil {
		t.Fatalf("StashPush returned error %v", err)
	}

	if state := tr.mergeState(); state.InProgress() {
		t.Fatalf("merge state = %+v", state)
	}
	for _, name := range append([]string{"MERGE_RR"}, mergeStateFiles...) {
		if tr.exists(".git/" + name) {
			t.Fatalf("%s is still there", name)
		}
	}
	if got := strings.TrimSpace(tr.readFile(".git/" + origHeadFile)); got != head.String() {
		t.Fatalf("ORIG_HEAD = %q, want %s", got, head)
	}
}

func TestStashPushReportsAnOrigHeadItCannotWrite(t *testing.T) {
	tr := newTestRepo(t)
	stashChanges(tr)
	if err := tr.repo.Root().MkdirAll(origHeadFile+"/blocked", 0o777); err != nil {
		t.Fatal(err)
	}

	if _, err := StashPush(t.Context(), tr.repo, StashOptions{When: mergeTime}); err == nil || errors.Is(err, ErrNothingToStash) {
		t.Fatalf("err = %v", err)
	}
}
