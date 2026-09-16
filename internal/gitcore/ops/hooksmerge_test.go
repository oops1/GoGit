package ops

import (
	"errors"
	"slices"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hooks"
)

func divergedTopic(t *testing.T) *testRepo {
	t.Helper()
	tr := newTestRepo(t)
	tr.writeFile("base.txt", "base\n")
	mustStage(t, tr, "base.txt")
	base := tr.commitAll("base")
	tr.createBranch("topic", base)
	tr.writeFile("main.txt", "main\n")
	mustStage(t, tr, "main.txt")
	tr.commitAll("main change")
	if err := Switch(t.Context(), tr.repo, "topic", SwitchOptions{}); err != nil {
		t.Fatalf("Switch returned error %v", err)
	}
	tr.writeFile("topic.txt", "topic\n")
	mustStage(t, tr, "topic.txt")
	tr.commitAll("topic change")
	if err := Switch(t.Context(), tr.repo, "main", SwitchOptions{}); err != nil {
		t.Fatalf("Switch returned error %v", err)
	}
	return tr
}

func TestAMergeCommitRunsTheMergeHooksInGitOrder(t *testing.T) {
	tr := divergedTopic(t)
	log := hookLogPath(t)
	installHooks(t, tr, testHook{log: log}, hookPreMergeCommit, hookPrepareCommitMsg, hookPostMerge)
	installHooks(t, tr, testHook{log: log, appendTo: "Change-Id: I4567"}, hookCommitMsg)

	result, err := Merge(t.Context(), tr.repo, "topic", MergeOptions{})
	if err != nil || !result.Committed {
		t.Fatalf("Merge = %+v, %v", result, err)
	}

	want := []hookRecord{
		{name: hookPreMergeCommit},
		{name: hookPrepareCommitMsg, args: []string{".git/MERGE_MSG", "merge"}},
		{name: hookCommitMsg, args: []string{".git/MERGE_MSG"}},
		{name: hookPostMerge, args: []string{"0"}},
	}
	if got := readHookLog(t, log); !recordsEqual(got, want) {
		t.Fatalf("hooks = %+v, want %+v", got, want)
	}
	commit, err := tr.db().Commit(result.New)
	if err != nil {
		t.Fatalf("Commit lookup returned error %v", err)
	}
	if commit.Message != "Merge branch 'topic'\nChange-Id: I4567\n" {
		t.Fatalf("message = %q", commit.Message)
	}
	if state, err := ReadMergeState(tr.repo); err != nil || state.InProgress() {
		t.Fatalf("merge state = %+v, %v", state, err)
	}
}

func TestARejectingMergeHookLeavesTheMergeToCommit(t *testing.T) {
	cases := map[string]struct {
		name string
		hook testHook
		want error
	}{
		"pre-merge-commit": {hookPreMergeCommit, testHook{exit: 1}, hooks.ErrRejected},
		"commit-msg":       {hookCommitMsg, testHook{exit: 1}, hooks.ErrRejected},
		"emptied message":  {hookCommitMsg, testHook{empty: true}, ErrEmptyMessage},
	}
	for label, tc := range cases {
		t.Run(label, func(t *testing.T) {
			tr := divergedTopic(t)
			before := headOf(t, tr)
			log := hookLogPath(t)
			installHooks(t, tr, tc.hook, tc.name)
			installHooks(t, tr, testHook{log: log}, hookPostMerge)

			result, err := Merge(t.Context(), tr.repo, "topic", MergeOptions{})

			if !errors.Is(err, tc.want) || result.Committed {
				t.Fatalf("Merge = %+v, %v, want %v", result, err, tc.want)
			}
			if head := headOf(t, tr); head != before {
				t.Fatalf("HEAD moved from %s to %s", before, head)
			}
			if state, err := ReadMergeState(tr.repo); err != nil || state.Operation() != OperationMerge {
				t.Fatalf("merge state = %+v, %v, want a merge to commit", state, err)
			}
			if got := readHookLog(t, log); len(got) != 0 {
				t.Fatalf("post-merge ran: %+v", got)
			}
		})
	}
}

func TestNoVerifySkipsTheVerifyingMergeHooks(t *testing.T) {
	tr := divergedTopic(t)
	log := hookLogPath(t)
	installHooks(t, tr, testHook{log: log, exit: 1}, hookPreMergeCommit, hookCommitMsg)
	installHooks(t, tr, testHook{log: log}, hookPrepareCommitMsg, hookPostMerge)

	result, err := Merge(t.Context(), tr.repo, "topic", MergeOptions{Hooks: HookOptions{NoVerify: true}})
	if err != nil || !result.Committed {
		t.Fatalf("Merge = %+v, %v", result, err)
	}
	if got := hookNames(readHookLog(t, log)); !slices.Equal(got, []string{hookPrepareCommitMsg, hookPostMerge}) {
		t.Fatalf("hooks = %q", got)
	}
}

func TestPostMergeRunsWithTheSquashFlagOnlyForCompletedMerges(t *testing.T) {
	cases := []struct {
		label string
		setup func(t *testing.T, tr *testRepo)
		opts  MergeOptions
		want  []hookRecord
	}{
		{"merge commit", nil, MergeOptions{}, []hookRecord{{name: hookPostMerge, args: []string{"0"}}}},
		{"squash", nil, MergeOptions{Mode: MergeSquash}, []hookRecord{{name: hookPostMerge, args: []string{"1"}}}},
		{"no commit", nil, MergeOptions{NoCommit: true}, nil},
		{"fast-forward", func(t *testing.T, tr *testRepo) {
			if _, err := Merge(t.Context(), tr.repo, "topic", MergeOptions{}); err != nil {
				t.Fatalf("Merge returned error %v", err)
			}
			if err := Switch(t.Context(), tr.repo, "topic", SwitchOptions{}); err != nil {
				t.Fatalf("Switch returned error %v", err)
			}
		}, MergeOptions{}, []hookRecord{{name: hookPostMerge, args: []string{"0"}}}},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			tr := divergedTopic(t)
			target := "topic"
			if tc.setup != nil {
				tc.setup(t, tr)
				target = "main"
			}
			log := hookLogPath(t)
			installHooks(t, tr, testHook{log: log}, hookPostMerge)

			if _, err := Merge(t.Context(), tr.repo, target, tc.opts); err != nil {
				t.Fatalf("Merge returned error %v", err)
			}
			if got := readHookLog(t, log); !recordsEqual(got, tc.want) {
				t.Fatalf("hooks = %+v, want %+v", got, tc.want)
			}
			if _, err := Merge(t.Context(), tr.repo, "HEAD", MergeOptions{}); err != nil && !errors.Is(err, ErrMergeInProgress) {
				t.Fatalf("up-to-date Merge returned error %v", err)
			}
			if got := readHookLog(t, log); !recordsEqual(got, tc.want) {
				t.Fatalf("an up-to-date merge ran hooks: %+v", got)
			}
		})
	}
}

func TestPostMergeFlagSkipsUnbornAndUpToDateMerges(t *testing.T) {
	m := &merger{opts: MergeOptions{Mode: MergeSquash}}
	if _, ok := m.postMergeFlag(MergeResult{FastForward: true}); ok {
		t.Fatal("a merge into an unborn branch must not run post-merge")
	}
	if _, ok := m.postMergeFlag(MergeResult{Old: bogusObjectID(t, 1), UpToDate: true}); ok {
		t.Fatal("an up-to-date merge must not run post-merge")
	}
	m.opts.Mode = MergeFastForward
	if _, ok := m.postMergeFlag(MergeResult{Old: bogusObjectID(t, 1)}); ok {
		t.Fatal("a stopped merge must not run post-merge")
	}
}
