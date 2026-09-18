package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/gitcore/ops"
	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/changes"
	"github.com/oops1/gogit/internal/ui/indexeditor"
)

const stagedLines = "l1\nl2\nl3\nl4\nl5\nl6\nl7\nl8\nl9\nl10\nl11\nSTAGED\n"

func captureIndexEditorViews(t *testing.T) *[]*indexeditor.View {
	t.Helper()
	views := &[]*indexeditor.View{}
	prev := newIndexEditorView
	newIndexEditorView = func() (*indexeditor.View, error) {
		view, err := prev()
		if err == nil {
			*views = append(*views, view)
		}
		return view, err
	}
	t.Cleanup(func() { newIndexEditorView = prev })
	return views
}

func appWithStagedAndWorkingChanges(t *testing.T) (*App, string) {
	t.Helper()
	a, target := appWithCommittedFile(t, twelveLines)
	writeWorkingText(t, target, stagedLines)
	if err := ops.Stage(t.Context(), openTestRepository(t, target).repo, []string{"f.txt"}, ops.StageOptions{}); err != nil {
		t.Fatal(err)
	}
	writeWorkingText(t, target, "L1\nl2\nl3\nl4\nl5\nl6\nl7\nl8\nl9\nl10\nl11\nSTAGED\n")
	return a, target
}

func openedIndexEditor(t *testing.T, a *App) *indexeditor.View {
	t.Helper()
	views := captureIndexEditorViews(t)
	readOnDispatcher(t, a, func() bool { a.openIndexEditor("f.txt"); return true })
	waitForDialog(t, a, func() int { return len(*views) }, "the index editor")
	return (*views)[len(*views)-1]
}

func readWorkingText(t *testing.T, target string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(target, "f.txt"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func waitForViewMessage(t *testing.T, a *App, view *indexeditor.View, want string) {
	t.Helper()
	deadline := time.Now().Add(testTimeout)
	for readOnDispatcher(t, a, view.Message) != want {
		if time.Now().After(deadline) {
			t.Fatalf("message = %q, want %q", readOnDispatcher(t, a, view.Message), want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestTheIndexEditorShowsHeadTheIndexAndTheWorkingCopy(t *testing.T) {
	a, _ := appWithStagedAndWorkingChanges(t)

	view := openedIndexEditor(t, a)

	if path := readOnDispatcher(t, a, view.Path); path != "f.txt" {
		t.Fatalf("path = %q", path)
	}
	if result := readOnDispatcher(t, a, view.Result); result != stagedLines {
		t.Fatalf("result = %q, want what is staged now", result)
	}
	if changesFound := readOnDispatcher(t, a, view.Changes); changesFound != 2 {
		t.Fatalf("changes = %d, want the staged line and the working copy line", changesFound)
	}
	sides := readOnDispatcher(t, a, view.Merge().Sides)
	if sides[widget.MergeOurs].Title != "main" {
		t.Fatalf("head side = %+v, want the current branch", sides[widget.MergeOurs])
	}
}

func TestSavingInTheIndexEditorChangesTheIndexOnly(t *testing.T) {
	a, target := appWithStagedAndWorkingChanges(t)
	view := openedIndexEditor(t, a)

	readOnDispatcher(t, a, func() bool { view.Merge().ResolveCurrent(widget.MergeTakeTheirs); return true })
	readOnDispatcher(t, a, func() bool { view.Merge().OnSaveRequest(); return true })
	waitForStatusText(t, a, i18n.Tf("Status.IndexEditorSaved", "f.txt"))

	if got := stagedContent(t, target); got != "L1\nl2\nl3\nl4\nl5\nl6\nl7\nl8\nl9\nl10\nl11\nSTAGED\n" {
		t.Fatalf("staged = %q", got)
	}
	if got := readWorkingText(t, target); got != "L1\nl2\nl3\nl4\nl5\nl6\nl7\nl8\nl9\nl10\nl11\nSTAGED\n" {
		t.Fatalf("the working copy changed: %q", got)
	}
	if readOnDispatcher(t, a, view.Modified) {
		t.Fatal("a saved result still counts as a change")
	}
}

func TestTheIndexEditorReportsASaveItCouldNotDo(t *testing.T) {
	a, _ := appWithStagedAndWorkingChanges(t)
	view := openedIndexEditor(t, a)
	prev := saveIndexContent
	saveIndexContent = func(context.Context, *gitrepo.Repository, string, []byte) error { return errors.New("disk is full") }
	t.Cleanup(func() { saveIndexContent = prev })

	readOnDispatcher(t, a, func() bool { view.Merge().OnSaveRequest(); return true })

	waitForViewMessage(t, a, view, i18n.Tf("Dialog.IndexEditor.SaveFailed", errors.New("disk is full")))
}

func TestTheIndexEditorReportsAFileItCannotRead(t *testing.T) {
	a, _ := appWithStagedAndWorkingChanges(t)
	views := captureIndexEditorViews(t)
	prev := readIndexSides
	readIndexSides = func(context.Context, *gitrepo.Repository, string) (ops.IndexSides, error) {
		return ops.IndexSides{}, errors.New("no such path")
	}
	t.Cleanup(func() { readIndexSides = prev })

	readOnDispatcher(t, a, func() bool { a.openIndexEditor("f.txt"); return true })
	waitForStatusText(t, a, i18n.Tf("Status.IndexEditorFailed", errors.New("no such path")))

	if len(*views) != 0 {
		t.Fatal("a window opened for a file that could not be read")
	}
}

func TestABinaryFileIsNotOpenedInTheIndexEditor(t *testing.T) {
	a, _ := appWithStagedAndWorkingChanges(t)
	views := captureIndexEditorViews(t)

	readOnDispatcher(t, a, func() bool {
		a.showIndexEditor(ops.IndexSides{Path: "b.bin", Binary: true})
		return true
	})

	if status := readOnDispatcher(t, a, a.statusLabel.Text); status != i18n.Tf("Status.IndexEditorBinary", "b.bin") {
		t.Fatalf("status = %q", status)
	}
	if len(*views) != 0 {
		t.Fatal("a binary file opened in the index editor")
	}
}

func TestTheIndexEditorReportsAWindowItCannotOpen(t *testing.T) {
	a, _ := appWithStagedAndWorkingChanges(t)
	prev := newIndexEditorView
	newIndexEditorView = func() (*indexeditor.View, error) { return nil, errors.New("no xaml") }
	t.Cleanup(func() { newIndexEditorView = prev })

	readOnDispatcher(t, a, func() bool { a.openIndexEditor("f.txt"); return true })

	waitForStatusText(t, a, i18n.Tf("Status.IndexEditorFailed", errors.New("no xaml")))
}

func TestWithoutARepositoryTheIndexEditorStaysShut(t *testing.T) {
	a := newTestApp(t)
	views := captureIndexEditorViews(t)

	readOnDispatcher(t, a, func() bool { a.openIndexEditor("f.txt"); return true })
	readOnDispatcher(t, a, func() bool { a.editSelectedInIndex(); return true })

	if len(*views) != 0 {
		t.Fatal("an index editor opened without a repository")
	}
}

func TestTheCommandOpensTheIndexEditorForTheSelectedFile(t *testing.T) {
	a, _ := appWithStagedAndWorkingChanges(t)
	views := captureIndexEditorViews(t)
	selectWorkingFile(t, a, "f.txt")

	readOnDispatcher(t, a, func() bool { return a.Dispatch(CmdIndexEditor) })
	waitForDialog(t, a, func() int { return len(*views) }, "the index editor")

	if path := readOnDispatcher(t, a, (*views)[0].Path); path != "f.txt" {
		t.Fatalf("path = %q", path)
	}
}

func TestTheFilesMenuOpensTheIndexEditor(t *testing.T) {
	a, _ := appWithStagedAndWorkingChanges(t)
	views := captureIndexEditorViews(t)
	selectWorkingFile(t, a, "f.txt")

	items := readOnDispatcher(t, a, func() []widget.MenuItem {
		return a.filesMenu(changes.Row{Name: "f.txt", RelPath: "f.txt", Status: changes.RowModified, IndexState: "modified"}, 0)
	})
	item, ok := findMenuItem(items, i18n.T("Menu.Files.IndexEditor"))
	if !ok || item.Disabled {
		t.Fatalf("item = %+v, want it offered for a selected file", item)
	}
	readOnDispatcher(t, a, func() bool { item.OnClick(); return true })
	waitForDialog(t, a, func() int { return len(*views) }, "the index editor")
}

func TestAnUntouchedIndexEditorClosesWithoutAsking(t *testing.T) {
	a, _ := appWithStagedAndWorkingChanges(t)
	view := openedIndexEditor(t, a)
	asked := 0
	a.askSave = func(string, string, func(widget.MessageBoxResult)) { asked++ }

	allowed := readOnDispatcher(t, a, func() bool { return view.Dialog().OnClosing() })
	readOnDispatcher(t, a, func() bool { view.OnClose(); return true })

	if !allowed || asked != 0 {
		t.Fatalf("closing allowed = %v, asked %d times", allowed, asked)
	}
}

func TestClosingAChangedIndexEditorAsksToSave(t *testing.T) {
	for _, tt := range []struct {
		name   string
		answer widget.MessageBoxResult
		saved  bool
	}{
		{"cancel keeps the window", widget.MBResultCancel, false},
		{"no closes without saving", widget.MBResultNo, false},
		{"yes saves first", widget.MBResultYes, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			a, target := appWithStagedAndWorkingChanges(t)
			view := openedIndexEditor(t, a)
			question := ""
			a.askSave = func(_, message string, cb func(widget.MessageBoxResult)) {
				question = message
				cb(tt.answer)
			}
			readOnDispatcher(t, a, func() bool { view.Merge().ResolveCurrent(widget.MergeTakeTheirs); return true })

			allowed := readOnDispatcher(t, a, func() bool { return view.Dialog().OnClosing() })

			if allowed {
				t.Fatal("a changed window closed without asking")
			}
			if question != i18n.Tf("Dialog.IndexEditor.Unsaved.Message", "f.txt") {
				t.Fatalf("question = %q", question)
			}
			if tt.saved {
				waitForStatusText(t, a, i18n.Tf("Status.IndexEditorSaved", "f.txt"))
				return
			}
			if got := stagedContent(t, target); got != stagedLines {
				t.Fatalf("staged = %q, want it left as it was", got)
			}
		})
	}
}

func TestOnADetachedHeadTheFirstSideIsCalledHead(t *testing.T) {
	a := newTestApp(t)

	if got := readOnDispatcher(t, a, a.headLabel); got != i18n.T("Dialog.IndexEditor.Side.Head") {
		t.Fatalf("label = %q", got)
	}
}
