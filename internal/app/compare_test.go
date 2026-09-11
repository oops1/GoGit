package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/ui/compare"
)

func captureCompareView(t *testing.T) **compare.View {
	t.Helper()
	captured := new(*compare.View)
	prev := newCompareView
	newCompareView = func() (*compare.View, error) {
		view, err := prev()
		*captured = view
		return view, err
	}
	t.Cleanup(func() { newCompareView = prev })
	return captured
}

func stubOpenFile(t *testing.T, path string, ok bool) {
	t.Helper()
	prev := showOpenFileDialog
	showOpenFileDialog = func(_ widget.ModalShower, _ widget.FileDialogOptions, cb func(string, bool)) *widget.FileDialog {
		cb(path, ok)
		return nil
	}
	t.Cleanup(func() { showOpenFileDialog = prev })
}

func stubSaveFile(t *testing.T, path string, ok bool) *widget.FileDialogOptions {
	t.Helper()
	seen := &widget.FileDialogOptions{}
	prev := showSaveFileDialog
	showSaveFileDialog = func(_ widget.ModalShower, opts widget.FileDialogOptions, cb func(string, bool)) *widget.FileDialog {
		*seen = opts
		cb(path, ok)
		return nil
	}
	t.Cleanup(func() { showSaveFileDialog = prev })
	return seen
}

func textFile(t *testing.T, name, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func watchModalsClosing(a *App) *int {
	closed := new(int)
	a.eng.SetOnModalClosed(func(widget.ModalWidget) { *closed++ })
	return closed
}

func TestTheCompareCommandOpensTheWindow(t *testing.T) {
	a := newTestApp(t)
	captured := captureCompareView(t)

	if !a.Dispatch(CmdCompareFiles) {
		t.Fatal("comparing files must not need an open repository")
	}

	if *captured == nil {
		t.Fatal("the compare window must open")
	}
}

func TestTheSelectedFilesBecomeTheSidesOfTheComparison(t *testing.T) {
	root := filepath.Join("C:", "repo")

	left, right := comparePair(root, []string{"a/one.go", "two.go", "three.go"})
	if left != filepath.Join(root, "a", "one.go") || right != filepath.Join(root, "two.go") {
		t.Fatalf("pair = %q, %q, want the first two selected files", left, right)
	}
	if left, right := comparePair(root, []string{"one.go"}); left == "" || right != "" {
		t.Fatalf("pair = %q, %q, want one file on the left", left, right)
	}
	if left, right := comparePair("", []string{"one.go"}); left != "" || right != "" {
		t.Fatalf("pair = %q, %q, want nothing without a working copy", left, right)
	}
}

func TestTheFileSelectedInTheWorkingCopyOpensOnTheLeft(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	buildWorkingRepoFixture(t, target)
	a := activatedWorkingApp(t, target)
	waitForWorkingRows(t, a, 1)
	captured := captureCompareView(t)
	readOnDispatcher(t, a, func() bool {
		a.filesGrid.Data().Grid.SetSelectedIndex(0)
		return true
	})
	selected := a.selectedWorkingPaths()
	if len(selected) != 1 {
		t.Fatalf("selected = %v, want one file", selected)
	}

	readOnDispatcher(t, a, func() bool { return a.Dispatch(CmdCompareFiles) })

	want := filepath.Join(target, filepath.FromSlash(selected[0]))
	if got := (*captured).Diff().FilePath(widget.DiffLeft); !strings.EqualFold(got, want) {
		t.Fatalf("left = %q, want the selected file %q", got, want)
	}
}

func TestAPickedFileGoesToTheSideItWasPickedFor(t *testing.T) {
	a := newTestApp(t)
	captured := captureCompareView(t)
	a.openCompareFiles("", "")
	right := textFile(t, "right.txt", "right\n")
	stubOpenFile(t, right, true)

	(*captured).OnPick(widget.DiffRight)

	if got := (*captured).Diff().FilePath(widget.DiffRight); got != right {
		t.Fatalf("right = %q, want %q", got, right)
	}
	if got := (*captured).Diff().FilePath(widget.DiffLeft); got != "" {
		t.Fatalf("left = %q, want it untouched", got)
	}
}

func TestACancelledPickLeavesTheSideAlone(t *testing.T) {
	a := newTestApp(t)
	captured := captureCompareView(t)
	left := textFile(t, "left.txt", "left\n")
	a.openCompareFiles(left, "")
	stubOpenFile(t, textFile(t, "other.txt", "other\n"), false)

	(*captured).OnPick(widget.DiffLeft)

	if got := (*captured).Diff().FilePath(widget.DiffLeft); got != left {
		t.Fatalf("left = %q, want it kept at %q", got, left)
	}
}

func TestADraftIsSavedWhereTheUserChooses(t *testing.T) {
	a := newTestApp(t)
	captured := captureCompareView(t)
	left := textFile(t, "left.txt", "left\n")
	a.openCompareFiles(left, "")
	target := filepath.Join(t.TempDir(), "draft.txt")
	opts := stubSaveFile(t, target, true)
	view := *captured
	view.Diff().SetActiveSide(widget.DiffRight)
	view.Diff().InsertText("draft\n")

	view.SaveAll()

	if data, err := os.ReadFile(target); err != nil || string(data) != "draft\n" {
		t.Fatalf("file = %q (%v), want the draft saved where chosen", data, err)
	}
	if opts.StartDir != filepath.Dir(left) {
		t.Fatalf("start = %q, want the folder of the other side", opts.StartDir)
	}
}

func TestACancelledSaveKeepsTheDraft(t *testing.T) {
	a := newTestApp(t)
	captured := captureCompareView(t)
	a.openCompareFiles("", "")
	stubSaveFile(t, filepath.Join(t.TempDir(), "draft.txt"), false)
	view := *captured
	view.Diff().SetActiveSide(widget.DiffRight)
	view.Diff().InsertText("draft\n")

	view.SaveAll()

	if !view.Modified() {
		t.Fatal("a cancelled save must keep the draft unsaved")
	}
}

func TestAnUntouchedComparisonClosesAtOnce(t *testing.T) {
	a := newTestApp(t)
	captured := captureCompareView(t)
	a.openCompareFiles(textFile(t, "a.txt", "a\n"), textFile(t, "b.txt", "b\n"))
	closed := watchModalsClosing(a)
	asked := false
	a.askConfirm = func(string, string, func(bool)) { asked = true }

	(*captured).OnClose()

	if asked || *closed != 1 {
		t.Fatalf("asked = %v, closed = %d, want the window closed without a question", asked, *closed)
	}
}

func TestClosingOverUnsavedEditsAsksFirst(t *testing.T) {
	for _, c := range []struct {
		name    string
		confirm bool
		closed  int
	}{
		{"kept open", false, 0},
		{"closed", true, 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			a := newTestApp(t)
			captured := captureCompareView(t)
			a.openCompareFiles(textFile(t, "a.txt", "a\n"), textFile(t, "b.txt", "b\n"))
			view := *captured
			view.Diff().SetActiveSide(widget.DiffLeft)
			view.Diff().InsertText("edit")
			closed := watchModalsClosing(a)
			asked := false
			a.askConfirm = func(_, _ string, cb func(bool)) {
				asked = true
				cb(c.confirm)
			}

			view.OnClose()

			if !asked || *closed != c.closed {
				t.Fatalf("asked = %v, closed = %d, want the question and %d closes", asked, *closed, c.closed)
			}
		})
	}
}

func TestAPickerStartsWhereTheComparisonAlreadyIs(t *testing.T) {
	a := newTestApp(t)
	captured := captureCompareView(t)
	left := textFile(t, "left.txt", "left\n")
	a.openCompareFiles(left, "")
	var start string
	prev := showOpenFileDialog
	showOpenFileDialog = func(_ widget.ModalShower, opts widget.FileDialogOptions, _ func(string, bool)) *widget.FileDialog {
		start = opts.StartDir
		return nil
	}
	t.Cleanup(func() { showOpenFileDialog = prev })

	(*captured).OnPick(widget.DiffRight)

	if start != filepath.Dir(left) {
		t.Fatalf("start = %q, want the folder of the left file", start)
	}
	if got := a.compareStartDir(*captured, widget.DiffLeft); got != filepath.Dir(left) {
		t.Fatalf("start = %q, want the folder of the file already on that side", got)
	}
}

func TestAPickerWithNothingOpenStartsInTheWorkingCopy(t *testing.T) {
	a := newTestApp(t)
	captured := captureCompareView(t)
	a.openCompareFiles("", "")

	if got := a.compareStartDir(*captured, widget.DiffLeft); got != "" {
		t.Fatalf("start = %q, want the default without a repository", got)
	}
}

func TestACompareWindowThatCannotBeBuiltIsOnlyLogged(t *testing.T) {
	a, buf := newChangesTestApp(t, t.TempDir())
	prev := newCompareView
	newCompareView = func() (*compare.View, error) { return nil, errors.New("no window") }
	t.Cleanup(func() { newCompareView = prev })

	a.openCompareFiles("", "")

	if !strings.Contains(buf.String(), "open compare window failed") {
		t.Fatalf("log = %q, want the failure written down", buf.String())
	}
}
