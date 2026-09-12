package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/gitcore/diff"
	"github.com/oops1/gogit/internal/gitcore/index"
	"github.com/oops1/gogit/internal/gitcore/ops"
	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/gitcore/worktree"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/changes"
	"github.com/oops1/gogit/internal/ui/diffview"
)

const twelveLines = "l1\nl2\nl3\nl4\nl5\nl6\nl7\nl8\nl9\nl10\nl11\nl12\n"

const bothEndsChanged = "L1\nl2\nl3\nl4\nl5\nl6\nl7\nl8\nl9\nl10\nl11\nL12\n"

func appWithCommittedFile(t *testing.T, base string) (*App, string) {
	t.Helper()
	a, target := forkedApp(t, false)
	r := openTestRepository(t, target).repo
	if err := os.WriteFile(filepath.Join(target, "f.txt"), []byte(base), 0o666); err != nil {
		t.Fatal(err)
	}
	if err := ops.Stage(t.Context(), r, []string{"f.txt"}, ops.StageOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := ops.Commit(t.Context(), r, ops.CommitOptions{Message: "base"}); err != nil {
		t.Fatal(err)
	}
	return a, target
}

func writeWorkingText(t *testing.T, target, text string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(target, "f.txt"), []byte(text), 0o666); err != nil {
		t.Fatal(err)
	}
}

func shownDiffOf(t *testing.T, a *App, entry worktree.Entry, hunks int) diffTarget {
	t.Helper()
	readOnDispatcher(t, a, func() bool { a.reloadWorktree(); a.showWorkingDiff(entry); return true })
	waitForDiffHunks(t, a, hunks)
	return readOnDispatcher(t, a, a.currentShownDiff)
}

func waitForDiffHunks(t *testing.T, a *App, want int) {
	t.Helper()
	deadline := time.Now().Add(testTimeout)
	for len(diffDocumentOnDispatcher(t, a).Hunks) != want {
		if time.Now().After(deadline) {
			t.Fatalf("the diff has %d hunks, want %d", len(diffDocumentOnDispatcher(t, a).Hunks), want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func stagedContent(t *testing.T, target string) string {
	t.Helper()
	o := openTestRepository(t, target)
	entry, ok := o.currentWorktree().Index().Get("f.txt", index.StageMerged)
	if !ok {
		t.Fatal("f.txt is not staged")
	}
	_, data, err := o.db.Get(entry.ID)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestTheWorkingDiffRemembersWhatItCompares(t *testing.T) {
	a, target := appWithCommittedFile(t, twelveLines)
	writeWorkingText(t, target, bothEndsChanged)

	shown := shownDiffOf(t, a, worktree.Entry{Path: "f.txt", Unstaged: worktree.StatusModified}, 2)

	if shown.kind != diffKindWorktree || shown.path != "f.txt" || len(shown.file.Hunks) != 2 {
		t.Fatalf("shown = %+v", shown)
	}
}

func TestStagingOneHunkLeavesTheOtherInTheWorkingCopy(t *testing.T) {
	a, target := appWithCommittedFile(t, twelveLines)
	writeWorkingText(t, target, bothEndsChanged)
	shown := shownDiffOf(t, a, worktree.Entry{Path: "f.txt", Unstaged: worktree.StatusModified}, 2)

	readOnDispatcher(t, a, func() bool { a.stageLines(shown, changes.PickedHunks([]int{0})); return true })
	waitForDiffHunks(t, a, 1)

	if got := stagedContent(t, target); got != "L1\nl2\nl3\nl4\nl5\nl6\nl7\nl8\nl9\nl10\nl11\nl12\n" {
		t.Fatalf("staged = %q", got)
	}
}

func TestUnstagingOneHunkTakesItBackOutOfTheIndex(t *testing.T) {
	a, target := appWithCommittedFile(t, twelveLines)
	writeWorkingText(t, target, bothEndsChanged)
	if err := ops.Stage(t.Context(), openTestRepository(t, target).repo, []string{"f.txt"}, ops.StageOptions{}); err != nil {
		t.Fatal(err)
	}
	shown := shownDiffOf(t, a, worktree.Entry{Path: "f.txt", Unstaged: worktree.StatusUnmodified}, 2)
	if shown.kind != diffKindIndex {
		t.Fatalf("shown = %+v, want the staged diff", shown)
	}

	readOnDispatcher(t, a, func() bool { a.unstageLines(shown, changes.PickedHunks([]int{0})); return true })
	waitForDiffHunks(t, a, 1)

	if got := stagedContent(t, target); got != "l1\nl2\nl3\nl4\nl5\nl6\nl7\nl8\nl9\nl10\nl11\nL12\n" {
		t.Fatalf("staged = %q", got)
	}
}

func TestDiscardingAHunkAsksFirst(t *testing.T) {
	a, target := appWithCommittedFile(t, twelveLines)
	writeWorkingText(t, target, bothEndsChanged)
	shown := shownDiffOf(t, a, worktree.Entry{Path: "f.txt", Unstaged: worktree.StatusModified}, 2)
	answers := []bool{false, true}
	a.askConfirm = func(_, _ string, cb func(bool)) {
		answer := answers[0]
		answers = answers[1:]
		cb(answer)
	}

	readOnDispatcher(t, a, func() bool { a.discardLines(shown, changes.PickedHunks([]int{0})); return true })
	if data, _ := os.ReadFile(filepath.Join(target, "f.txt")); string(data) != bothEndsChanged {
		t.Fatalf("a refused discard changed the file: %q", data)
	}
	readOnDispatcher(t, a, func() bool { a.discardLines(shown, changes.PickedHunks([]int{0})); return true })
	waitForDiffHunks(t, a, 1)

	data, err := os.ReadFile(filepath.Join(target, "f.txt"))
	if err != nil || !strings.HasPrefix(string(data), "l1\n") || !strings.HasSuffix(string(data), "L12\n") {
		t.Fatalf("f.txt = %q, %v", data, err)
	}
}

func unsplittableTarget() diffTarget {
	return diffTarget{kind: diffKindWorktree, path: "f.txt", file: diff.File{
		Hunks: diff.Blobs([]byte("a"), []byte("a\nb"), diff.Defaults()),
	}}
}

func TestLinesThatCannotBeSplitOffAreExplained(t *testing.T) {
	a := newTestApp(t)
	target := unsplittableTarget()

	readOnDispatcher(t, a, func() bool { a.stageLines(target, func(_, line int) bool { return line == 2 }); return true })
	if got := readOnDispatcher(t, a, a.statusLabel.Text); got != i18n.T("Status.PatchUnsplittable") {
		t.Fatalf("status after staging = %q", got)
	}

	readOnDispatcher(t, a, func() bool { a.statusLabel.SetText(""); return true })
	readOnDispatcher(t, a, func() bool { a.discardLines(target, func(_, line int) bool { return line == 0 }); return true })
	if got := readOnDispatcher(t, a, a.statusLabel.Text); got != i18n.T("Status.PatchUnsplittable") {
		t.Fatalf("status after discarding = %q", got)
	}
}

func TestAFailedPatchIsReported(t *testing.T) {
	a, target := appWithCommittedFile(t, twelveLines)
	writeWorkingText(t, target, bothEndsChanged)
	shown := shownDiffOf(t, a, worktree.Entry{Path: "f.txt", Unstaged: worktree.StatusModified}, 2)
	prev := patchIndex
	patchIndex = func(context.Context, *gitrepo.Repository, string, []diff.Hunk) error { return errors.New("boom") }
	t.Cleanup(func() { patchIndex = prev })

	readOnDispatcher(t, a, func() bool { a.stageLines(shown, changes.PickedHunks([]int{0})); return true })

	waitForStatusText(t, a, i18n.Tf("Status.PatchFailed", errors.New("boom")))
}

func diffMenuTexts(items []widget.MenuItem) []string {
	var out []string
	for _, item := range items {
		out = append(out, item.Text)
	}
	return out
}

func TestTheDiffMenuFollowsWhatTheDiffCompares(t *testing.T) {
	a := newTestApp(t)
	file := diff.File{Hunks: diff.Blobs([]byte("a\n"), []byte("b\n"), diff.Defaults())}
	lines := []diffview.LineRef{{Hunk: 0, Line: 0}}
	hunks := []int{0}

	if items := readOnDispatcher(t, a, a.diffMenu); items != nil {
		t.Fatalf("nothing shown, yet the menu offers %v", diffMenuTexts(items))
	}
	if items := a.diffMenuFor(diffTarget{file: file}, lines, hunks); items != nil {
		t.Fatalf("a commit diff offers %v", diffMenuTexts(items))
	}
	working := a.diffMenuFor(diffTarget{kind: diffKindWorktree, file: file}, lines, hunks)
	if len(working) != 5 || working[0].Text != i18n.T("Menu.Diff.StageLines") || working[3].Text != i18n.T("Menu.Diff.DiscardLines") {
		t.Fatalf("working copy menu = %v", diffMenuTexts(working))
	}
	staged := a.diffMenuFor(diffTarget{kind: diffKindIndex, file: file}, lines, hunks)
	if len(staged) != 2 || staged[1].Text != i18n.T("Menu.Diff.UnstageHunk") {
		t.Fatalf("staged menu = %v", diffMenuTexts(staged))
	}
	for _, item := range append(working, staged...) {
		if item.OnClick != nil {
			if item.Disabled {
				t.Fatalf("%q is off with lines and a hunk selected", item.Text)
			}
			readOnDispatcher(t, a, func() bool { item.OnClick(); return true })
		}
	}
}

func TestTheDiffMenuTurnsItsItemsOffWithoutASelection(t *testing.T) {
	a := newTestApp(t)
	file := diff.File{Hunks: diff.Blobs([]byte("a\n"), []byte("b\n"), diff.Defaults())}

	for _, item := range a.diffMenuFor(diffTarget{kind: diffKindWorktree, file: file}, nil, nil) {
		if item.OnClick != nil && !item.Disabled {
			t.Fatalf("%q is on with nothing selected", item.Text)
		}
	}
}
