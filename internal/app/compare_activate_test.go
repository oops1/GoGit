package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/gitcore/diff"
	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/changes"
	"github.com/oops1/gogit/internal/ui/compare"
)

func workingCompareApp(t *testing.T) (*App, string, **compare.View) {
	t.Helper()
	target := filepath.Join(t.TempDir(), "main")
	buildWorkingRepoFixture(t, target)
	a := activatedWorkingApp(t, target)
	waitForWorkingRows(t, a, 4)
	return a, target, captureCompareView(t)
}

func activate(t *testing.T, a *App, relPath string) {
	t.Helper()
	readOnDispatcher(t, a, func() bool {
		a.onFilesRowActivated(0, changes.Row{RelPath: relPath})
		return true
	})
}

func TestDoubleClickingAModifiedFileComparesItWithTheRepository(t *testing.T) {
	a, target, captured := workingCompareApp(t)

	activate(t, a, "modified.txt")

	view := *captured
	if view == nil {
		t.Fatal("the compare window must open")
	}
	if got := view.Diff().Text(widget.DiffLeft); got != "old\n" {
		t.Fatalf("left = %q, want the version in the repository", got)
	}
	if !view.Diff().IsReadOnly(widget.DiffLeft) {
		t.Fatal("the repository side is not for editing")
	}
	if got := view.Diff().FilePath(widget.DiffRight); got != filepath.Join(target, "modified.txt") {
		t.Fatalf("right = %q, want the file in the working copy", got)
	}
	if view.Diff().IsReadOnly(widget.DiffRight) {
		t.Fatal("the working copy side must stay editable")
	}
}

func TestANewFileHasNothingOnTheRepositorySide(t *testing.T) {
	a, _, captured := workingCompareApp(t)

	activate(t, a, "untracked.txt")

	view := *captured
	if got := view.Diff().Text(widget.DiffLeft); got != "" {
		t.Fatalf("left = %q, want nothing for a file the repository does not have", got)
	}
	if view.Diff().FilePath(widget.DiffRight) == "" {
		t.Fatal("the new file must be on the right")
	}
}

func TestADeletedFileShowsWhatWasLost(t *testing.T) {
	a, target, captured := workingCompareApp(t)
	if err := os.Remove(filepath.Join(target, "clean.txt")); err != nil {
		t.Fatal(err)
	}
	a.RefreshRepository()
	waitForWorkingIdle(t, a)

	activate(t, a, "clean.txt")

	view := *captured
	if got := view.Diff().Text(widget.DiffLeft); got != "same\n" {
		t.Fatalf("left = %q, want the content the repository still has", got)
	}
	if view.Diff().FilePath(widget.DiffRight) != "" || !view.Diff().IsReadOnly(widget.DiffRight) {
		t.Fatal("a deleted file has nothing to load on the right")
	}
}

func TestAFileOfACommitIsComparedWithItsParent(t *testing.T) {
	a, _, captured := workingCompareApp(t)
	o := a.opened()
	oldID := putChangesBlob(t, o.db, "before\n")
	newID := putChangesBlob(t, o.db, "after\n")
	commit := hash.SumSHA1("commit", []byte("any"))
	a.filesMu.Lock()
	a.filesMode = filesModeCommit
	a.currentFiles = []diff.File{{OldPath: "a.txt", NewPath: "a.txt", OldID: oldID, NewID: newID}}
	a.filesMu.Unlock()
	a.selectedCommit = commit

	activate(t, a, "a.txt")

	view := *captured
	if view.Diff().Text(widget.DiffLeft) != "before\n" || view.Diff().Text(widget.DiffRight) != "after\n" {
		t.Fatalf("sides = %q | %q, want the parent and the commit", view.Diff().Text(widget.DiffLeft), view.Diff().Text(widget.DiffRight))
	}
	if !view.Diff().IsReadOnly(widget.DiffLeft) || !view.Diff().IsReadOnly(widget.DiffRight) {
		t.Fatal("history is not for editing")
	}
}

func TestAFileAddedOrDeletedByACommitHasOneEmptySide(t *testing.T) {
	a, _, captured := workingCompareApp(t)
	o := a.opened()
	id := putChangesBlob(t, o.db, "only\n")
	a.filesMu.Lock()
	a.filesMode = filesModeCommit
	a.currentFiles = []diff.File{
		{NewPath: "added.txt", NewID: id},
		{OldPath: "removed.txt", OldID: id},
	}
	a.filesMu.Unlock()

	activate(t, a, "added.txt")
	if (*captured).Diff().Text(widget.DiffLeft) != "" || (*captured).Diff().Text(widget.DiffRight) != "only\n" {
		t.Fatal("an added file has nothing in the parent")
	}

	activate(t, a, "removed.txt")
	if (*captured).Diff().Text(widget.DiffLeft) != "only\n" || (*captured).Diff().Text(widget.DiffRight) != "" {
		t.Fatal("a deleted file has nothing in the commit")
	}
}

func TestActivatingSomethingThatIsNotAFileOpensNothing(t *testing.T) {
	a, _, captured := workingCompareApp(t)

	readOnDispatcher(t, a, func() bool {
		a.onFilesRowActivated(0, "not a row")
		a.onFilesRowActivated(0, changes.Row{RelPath: "missing.txt"})
		a.filesMu.Lock()
		a.filesMode = filesModeCommit
		a.filesMu.Unlock()
		a.onFilesRowActivated(0, changes.Row{RelPath: "missing.txt"})
		return true
	})

	if *captured != nil {
		t.Fatal("nothing must open for a row that is not a known file")
	}
}

func TestActivatingWithoutARepositoryOpensNothing(t *testing.T) {
	a := newTestApp(t)
	captured := captureCompareView(t)

	a.onFilesRowActivated(0, changes.Row{RelPath: "a.txt"})

	if *captured != nil {
		t.Fatal("nothing to compare without a repository")
	}
}

func TestABrokenObjectIsLoggedInsteadOfOpened(t *testing.T) {
	a, _, captured := workingCompareApp(t)
	missing := hash.SumSHA1("blob", []byte("never written"))
	a.filesMu.Lock()
	a.filesMode = filesModeCommit
	a.currentFiles = []diff.File{
		{OldPath: "old.txt", NewPath: "old.txt", OldID: missing},
		{OldPath: "new.txt", NewPath: "new.txt", NewID: missing},
	}
	a.filesMu.Unlock()

	activate(t, a, "old.txt")
	activate(t, a, "new.txt")

	if *captured != nil {
		t.Fatal("a file whose objects cannot be read must not open")
	}
}

func TestTheTitlesSayWhereEachSideComesFrom(t *testing.T) {
	commit := hash.SumSHA1("commit", []byte("x")).String()
	o := &openedRepository{}
	left, right, err := commitSides(o, diff.File{OldPath: "a", NewPath: "a"}, commit)
	if err != nil {
		t.Fatal(err)
	}
	if left.title != i18n.T("Dialog.Compare.NotInParent") || right.title != i18n.T("Dialog.Compare.Deleted") {
		t.Fatalf("titles = %q | %q, want both sides named for what they lack", left.title, right.title)
	}
	if got := shortCommit(commit); got != commit[:compareShortHash] {
		t.Fatalf("short = %q", got)
	}
	if got := shortCommit("abc"); got != "abc" {
		t.Fatalf("short = %q, want a short id kept whole", got)
	}
}

func TestSelectedFilesAreComparedInTheOrderTheListShowsThem(t *testing.T) {
	a, _, _ := workingCompareApp(t)
	grid := a.filesGrid.Data()
	b := grid.Grid.Bounds()
	y := func(i int) int {
		return b.Min.Y + grid.Grid.HeaderHeight + i*grid.Grid.RowHeight + grid.Grid.RowHeight/2
	}
	click := func(i int, mod widget.KeyMod) {
		readOnDispatcher(t, a, func() bool {
			grid.OnMouseButton(widget.MouseEvent{X: b.Min.X + 20, Y: y(i), Button: widget.MouseLeft, Pressed: true, Mod: mod})
			grid.OnMouseButton(widget.MouseEvent{X: b.Min.X + 20, Y: y(i), Button: widget.MouseLeft, Pressed: false, Mod: mod})
			return true
		})
	}
	click(2, 0)
	click(0, widget.ModCtrl)

	got := readOnDispatcher(t, a, a.orderedWorkingPaths)
	first := readOnDispatcher(t, a, func() string { return a.filesItems.Get(0).(changes.Row).RelPath })
	third := readOnDispatcher(t, a, func() string { return a.filesItems.Get(2).(changes.Row).RelPath })
	if len(got) != 2 || got[0] != first || got[1] != third {
		t.Fatalf("paths = %v, want %q then %q as the list shows them", got, first, third)
	}
}

func TestAWorkingFileWhoseHistoryCannotBeReadIsReported(t *testing.T) {
	a, target, _ := workingCompareApp(t)
	o := a.opened()
	entry, ok := entryOf(a.currentEntries, changes.Row{RelPath: "modified.txt"})
	if !ok {
		t.Fatal("fixture has no modified.txt")
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, _, err := workingSides(ctx, o, entry); err == nil {
		t.Fatal("a cancelled read must fail")
	}

	blob := hash.SumSHA1("blob", []byte("old\n"))
	if err := os.Remove(filepath.Join(target, ".git", "objects", blob.String()[:2], blob.String()[2:])); err != nil {
		t.Fatal(err)
	}
	if _, _, err := workingSides(t.Context(), o, entry); err == nil {
		t.Fatal("a missing blob must fail")
	}
}

func TestTheSaveDialogIsTheEnginesOwn(t *testing.T) {
	a := newTestApp(t)

	dialog := showSaveFileDialog(a.eng, widget.FileDialogOptions{Title: "save"}, func(string, bool) {})

	if dialog == nil {
		t.Fatal("the save dialog must open")
	}
	a.eng.CloseModal(dialog.Dialog())
}
