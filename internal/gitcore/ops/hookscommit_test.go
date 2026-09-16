package ops

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hooks"
	"github.com/oops1/gogit/internal/gitcore/refs"
)

func stagedRepo(t *testing.T) *testRepo {
	t.Helper()
	tr := newTestRepo(t)
	tr.writeFile("a.txt", "a\n")
	mustStage(t, tr, "a.txt")
	return tr
}

func installHooks(t *testing.T, tr *testRepo, hook testHook, names ...string) {
	t.Helper()
	for _, name := range names {
		installTestHook(t, tr.repo.HooksDir(), name, hook)
	}
}

func headOf(t *testing.T, tr *testRepo) string {
	t.Helper()
	ref, err := tr.refs().Resolve(refs.HEAD)
	if err != nil {
		return ""
	}
	return ref.Target.String()
}

func TestCommitRunsTheCommitHooksInGitOrderAndKeepsTheEditedMessage(t *testing.T) {
	tr := stagedRepo(t)
	log := hookLogPath(t)
	installHooks(t, tr, testHook{log: log}, hookPreCommit, hookPrepareCommitMsg, hookPostCommit)
	installHooks(t, tr, testHook{log: log, appendTo: "Change-Id: I0123"}, hookCommitMsg)

	id, err := Commit(t.Context(), tr.repo, CommitOptions{Message: "subject"})
	if err != nil {
		t.Fatalf("Commit returned error %v", err)
	}

	want := []hookRecord{
		{name: hookPreCommit},
		{name: hookPrepareCommitMsg, args: []string{".git/COMMIT_EDITMSG", "message"}},
		{name: hookCommitMsg, args: []string{".git/COMMIT_EDITMSG"}},
		{name: hookPostCommit},
	}
	if got := readHookLog(t, log); !recordsEqual(got, want) {
		t.Fatalf("hooks = %+v, want %+v", got, want)
	}
	commit, err := tr.db().Commit(id)
	if err != nil {
		t.Fatalf("Commit lookup returned error %v", err)
	}
	if commit.Message != "subject\n\nChange-Id: I0123\n" {
		t.Fatalf("message = %q", commit.Message)
	}
}

func TestARejectingPreCommitHookStopsTheCommitAndReleasesTheIndex(t *testing.T) {
	tr := stagedRepo(t)
	log := hookLogPath(t)
	installHooks(t, tr, testHook{log: log, print: "lint failed", exit: 1}, hookPreCommit)
	installHooks(t, tr, testHook{log: log}, hookPrepareCommitMsg)

	_, err := Commit(t.Context(), tr.repo, CommitOptions{Message: "subject"})

	var hookErr *hooks.Error
	if !errors.As(err, &hookErr) || hookErr.Hook != hookPreCommit || !slices.Contains(hookErr.Output, "lint failed") {
		t.Fatalf("Commit returned %v, want the pre-commit rejection", err)
	}
	if head := headOf(t, tr); head != "" {
		t.Fatalf("HEAD moved to %s", head)
	}
	if got := hookNames(readHookLog(t, log)); !slices.Equal(got, []string{hookPreCommit}) {
		t.Fatalf("hooks = %q", got)
	}
	if _, err := Commit(t.Context(), tr.repo, CommitOptions{Message: "subject", Hooks: HookOptions{NoVerify: true}}); err != nil {
		t.Fatalf("Commit after the rejection returned error %v", err)
	}
}

func TestNoVerifySkipsOnlyTheVerifyingCommitHooks(t *testing.T) {
	tr := stagedRepo(t)
	log := hookLogPath(t)
	installHooks(t, tr, testHook{log: log, exit: 1}, hookPreCommit, hookCommitMsg, hookPostCommit)
	installHooks(t, tr, testHook{log: log}, hookPrepareCommitMsg)

	if _, err := Commit(t.Context(), tr.repo, CommitOptions{Message: "subject", Hooks: HookOptions{NoVerify: true}}); err != nil {
		t.Fatalf("Commit returned error %v", err)
	}

	if got := hookNames(readHookLog(t, log)); !slices.Equal(got, []string{hookPrepareCommitMsg, hookPostCommit}) {
		t.Fatalf("hooks = %q", got)
	}
}

func TestMessageHookFailuresStopTheCommit(t *testing.T) {
	cases := map[string]struct {
		name string
		hook testHook
		want error
	}{
		"prepare rejects":    {hookPrepareCommitMsg, testHook{exit: 1}, hooks.ErrRejected},
		"commit-msg rejects": {hookCommitMsg, testHook{exit: 1}, hooks.ErrRejected},
		"message emptied":    {hookCommitMsg, testHook{empty: true}, ErrEmptyMessage},
	}
	for label, tc := range cases {
		t.Run(label, func(t *testing.T) {
			tr := stagedRepo(t)
			installHooks(t, tr, tc.hook, tc.name)

			if _, err := Commit(t.Context(), tr.repo, CommitOptions{Message: "subject"}); !errors.Is(err, tc.want) {
				t.Fatalf("Commit returned %v, want %v", err, tc.want)
			}
			if head := headOf(t, tr); head != "" {
				t.Fatalf("HEAD moved to %s", head)
			}
		})
	}
}

func TestMessageHooksDoNotRunWhenThereIsNothingToCommit(t *testing.T) {
	tr := newTestRepo(t)
	log := hookLogPath(t)
	installHooks(t, tr, testHook{log: log}, hookPrepareCommitMsg)

	if _, err := Commit(t.Context(), tr.repo, CommitOptions{Message: "subject"}); !errors.Is(err, ErrNothingToCommit) {
		t.Fatalf("Commit returned %v, want ErrNothingToCommit", err)
	}
	if got := readHookLog(t, log); len(got) != 0 {
		t.Fatalf("hooks = %+v, want none", got)
	}
}

func TestMessageHooksNeedAReadableIndex(t *testing.T) {
	tr := stagedRepo(t)
	installHooks(t, tr, testHook{}, hookPrepareCommitMsg)
	tr.corruptIndexFile()

	if _, err := Commit(t.Context(), tr.repo, CommitOptions{Message: "subject"}); err == nil {
		t.Fatal("Commit must fail on a damaged index")
	}
}

func TestAmendReportsTheRewriteToPostRewrite(t *testing.T) {
	tr := stagedRepo(t)
	first, err := Commit(t.Context(), tr.repo, CommitOptions{Message: "first"})
	if err != nil {
		t.Fatalf("Commit returned error %v", err)
	}
	log := hookLogPath(t)
	installHooks(t, tr, testHook{log: log, stdin: true}, hookPostRewrite)

	amended, err := Commit(t.Context(), tr.repo, CommitOptions{Message: "amended", Amend: true})
	if err != nil {
		t.Fatalf("Commit returned error %v", err)
	}

	want := []hookRecord{{name: hookPostRewrite, args: []string{"amend"}, stdin: []string{first.String() + " " + amended.String()}}}
	if got := readHookLog(t, log); !recordsEqual(got, want) {
		t.Fatalf("hooks = %+v, want %+v", got, want)
	}
}

func TestHookEventsReachTheCaller(t *testing.T) {
	tr := stagedRepo(t)
	installHooks(t, tr, testHook{print: "checking"}, hookPreCommit)
	var lines []string
	events := func(e hooks.Event) {
		if e.Kind == hooks.EventOutput {
			lines = append(lines, strings.TrimSpace(e.Line))
		}
	}

	if _, err := Commit(t.Context(), tr.repo, CommitOptions{Message: "subject", Hooks: HookOptions{Events: events}}); err != nil {
		t.Fatalf("Commit returned error %v", err)
	}
	if !slices.Equal(lines, []string{"checking"}) {
		t.Fatalf("lines = %q", lines)
	}
}
