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

	"github.com/oops1/gogit/internal/gitcore/merge"
	"github.com/oops1/gogit/internal/gitcore/ops"
	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/changes"
	"github.com/oops1/gogit/internal/ui/conflict"
	uimerge "github.com/oops1/gogit/internal/ui/merge"
)

func captureConflictViews(t *testing.T) *[]*conflict.View {
	t.Helper()
	views := &[]*conflict.View{}
	prev := newConflictView
	newConflictView = func() (*conflict.View, error) {
		view, err := prev()
		if err == nil {
			*views = append(*views, view)
		}
		return view, err
	}
	t.Cleanup(func() { newConflictView = prev })
	return views
}

func waitForConflictView(t *testing.T, a *App, views *[]*conflict.View) *conflict.View {
	t.Helper()
	waitForDialog(t, a, func() int { return len(*views) }, "the conflict dialog")
	return (*views)[len(*views)-1]
}

func conflictedApp(t *testing.T) (*App, string) {
	t.Helper()
	a, target := forkedApp(t, true)
	mergeThrough(t, a, uimerge.Request{Source: "feature"})
	waitForBanner(t, a, true)
	return a, target
}

func openedConflict(t *testing.T, a *App) *conflict.View {
	t.Helper()
	views := captureConflictViews(t)
	readOnDispatcher(t, a, func() bool { a.openConflictEditor("f.txt"); return true })
	return waitForConflictView(t, a, views)
}

func TestTheConflictMenuOffersTheEditor(t *testing.T) {
	a, _ := conflictedApp(t)

	items := readOnDispatcher(t, a, func() []widget.MenuItem {
		return a.conflictItems(changes.Row{Status: changes.RowConflict, RelPath: "f.txt"})
	})

	if !hasMenuItem(items, i18n.T("Menu.Context.ResolveConflict")) {
		t.Fatalf("items = %+v", items)
	}
}

func TestTheEditorShowsBothSidesOfTheConflict(t *testing.T) {
	a, _ := conflictedApp(t)

	view := openedConflict(t, a)

	path := readOnDispatcher(t, a, view.Path)
	unresolved := readOnDispatcher(t, a, view.Unresolved)
	if path != "f.txt" || unresolved != 1 {
		t.Fatalf("path = %q, unresolved = %d", path, unresolved)
	}
	result := readOnDispatcher(t, a, view.Result)
	if !strings.Contains(result, "<<<<<<< main") || !strings.Contains(result, ">>>>>>> feature") {
		t.Fatalf("result = %q, want the branch names in the markers", result)
	}
}

func TestResolvingInTheEditorStagesTheFile(t *testing.T) {
	a, target := conflictedApp(t)
	view := openedConflict(t, a)

	readOnDispatcher(t, a, func() bool { view.Merge().ResolveAll(widget.MergeTakeTheirs); return true })
	readOnDispatcher(t, a, func() bool { view.Merge().OnSaveRequest(); return true })
	waitForStatusText(t, a, i18n.Tf("Status.ConflictSaved", "f.txt"))

	data, err := os.ReadFile(filepath.Join(target, "f.txt"))
	if err != nil || strings.Contains(string(data), "<<<<<<<") {
		t.Fatalf("f.txt = %q, %v", data, err)
	}
	if !strings.HasSuffix(string(data), "\n") {
		t.Fatalf("f.txt = %q, want the trailing newline kept", data)
	}
}

func TestSavingAnUnfinishedResolutionKeepsTheConflict(t *testing.T) {
	a, target := conflictedApp(t)
	view := openedConflict(t, a)

	readOnDispatcher(t, a, func() bool { view.Merge().OnSaveRequest(); return true })
	waitForConflictMessage(t, a, view, i18n.T("Dialog.Conflict.SavedWithMarkers"))

	data, err := os.ReadFile(filepath.Join(target, "f.txt"))
	if err != nil || !strings.Contains(string(data), "<<<<<<<") {
		t.Fatalf("f.txt = %q, %v", data, err)
	}
	if state := a.State(); !state.Merging {
		t.Fatalf("state = %+v, want the merge still unfinished", state)
	}
}

func TestAFailedSaveIsReportedInTheWindow(t *testing.T) {
	a, _ := conflictedApp(t)
	view := openedConflict(t, a)
	prev := saveResolution
	saveResolution = func(context.Context, *gitrepo.Repository, string, []byte, ops.ResolutionOptions) error {
		return errors.New("boom")
	}
	t.Cleanup(func() { saveResolution = prev })

	readOnDispatcher(t, a, func() bool { view.Merge().OnSaveRequest(); return true })

	waitForConflictMessage(t, a, view, i18n.Tf("Dialog.Conflict.SaveFailed", errors.New("boom")))
}

func waitForConflictMessage(t *testing.T, a *App, view *conflict.View, want string) {
	t.Helper()
	deadline := time.Now().Add(testTimeout)
	for readOnDispatcher(t, a, view.Message) != want {
		if time.Now().After(deadline) {
			t.Fatalf("message = %q, want %q", readOnDispatcher(t, a, view.Message), want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestAConflictThatCannotBeReadIsReported(t *testing.T) {
	a, _ := conflictedApp(t)
	prev := readConflict
	readConflict = func(context.Context, *gitrepo.Repository, string) (ops.ConflictFile, error) {
		return ops.ConflictFile{}, errors.New("boom")
	}
	t.Cleanup(func() { readConflict = prev })

	readOnDispatcher(t, a, func() bool { a.openConflictEditor("f.txt"); return true })

	waitForStatusText(t, a, i18n.Tf("Status.ConflictOpenFailed", errors.New("boom")))
}

func TestABinaryConflictSendsTheUserToTheFilesPanel(t *testing.T) {
	a, _ := conflictedApp(t)

	readOnDispatcher(t, a, func() bool {
		a.showConflictEditor(ops.ConflictFile{Path: "bin", Binary: true})
		return true
	})

	if got := readOnDispatcher(t, a, a.statusLabel.Text); got != i18n.Tf("Status.ConflictOpenFailed", i18n.T("Dialog.Conflict.Binary")) {
		t.Fatalf("status = %q", got)
	}
}

func TestTheEditorKeepsQuietWhenItsWindowCannotOpen(t *testing.T) {
	a, _ := conflictedApp(t)
	prev := newConflictView
	newConflictView = func() (*conflict.View, error) { return nil, errors.New("boom") }
	t.Cleanup(func() { newConflictView = prev })

	readOnDispatcher(t, a, func() bool {
		a.showConflictEditor(ops.ConflictFile{Path: "f.txt"})
		return true
	})

	if got := readOnDispatcher(t, a, a.statusLabel.Text); got != i18n.Tf("Status.ConflictOpenFailed", errors.New("boom")) {
		t.Fatalf("status = %q", got)
	}
}

func TestTheEditorDoesNotOpenWithoutARepository(t *testing.T) {
	a := newTestApp(t)
	views := captureConflictViews(t)

	a.openConflictEditor("f.txt")

	if len(*views) != 0 {
		t.Fatal("a conflict window opened without a repository")
	}
}

func TestTheEditorCanBeClosed(t *testing.T) {
	a, _ := conflictedApp(t)
	view := openedConflict(t, a)

	readOnDispatcher(t, a, func() bool { view.Dialog().CancelAction(); return true })
}

func TestTheSidesAreNamedAfterTheBranchesTakingPart(t *testing.T) {
	a, _ := conflictedApp(t)

	ours := readOnDispatcher(t, a, a.oursLabel)
	theirs := readOnDispatcher(t, a, a.theirsLabel)

	if ours != "main" || theirs != "feature" {
		t.Fatalf("ours = %q, theirs = %q", ours, theirs)
	}
}

func TestTheSidesFallBackToPlainNames(t *testing.T) {
	a := newTestApp(t)

	ours := readOnDispatcher(t, a, a.oursLabel)
	theirs := readOnDispatcher(t, a, a.theirsLabel)

	if ours != i18n.T("Dialog.Conflict.Side.Ours") || theirs != i18n.T("Dialog.Conflict.Side.Theirs") {
		t.Fatalf("ours = %q, theirs = %q", ours, theirs)
	}
}

func TestAFileWithoutATrailingNewlineKeepsItThatWay(t *testing.T) {
	for _, tt := range []struct {
		name string
		file ops.ConflictFile
		want bool
	}{
		{"ours ends with one", ops.ConflictFile{Ours: []byte("a\n")}, true},
		{"ours does not", ops.ConflictFile{Ours: []byte("a")}, false},
		{"only theirs is there", ops.ConflictFile{Theirs: []byte("a\n")}, true},
		{"only the base is there", ops.ConflictFile{Base: []byte("a\n")}, true},
		{"nothing at all", ops.ConflictFile{}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := endsWithNewline(tt.file); got != tt.want {
				t.Fatalf("endsWithNewline = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTheEditorShowsTheBlocksItWasGiven(t *testing.T) {
	a, _ := conflictedApp(t)
	views := captureConflictViews(t)

	readOnDispatcher(t, a, func() bool {
		a.showConflictEditor(ops.ConflictFile{
			Path:  "f.txt",
			Ours:  []byte("one\n"),
			Style: merge.StyleDiff3,
			Blocks: []merge.Chunk{
				{Conflict: true, Ours: []string{"OURS\n"}, Base: []string{"two\n"}, Theirs: []string{"THEIRS\n"}},
			},
		})
		return true
	})

	view := (*views)[0]
	if got := readOnDispatcher(t, a, view.Result); !strings.Contains(got, "|||||||") {
		t.Fatalf("result = %q, want the diff3 markers the file asked for", got)
	}
}

func TestDoubleClickingAConflictedRowOpensTheEditor(t *testing.T) {
	a, _ := conflictedApp(t)
	views := captureConflictViews(t)

	readOnDispatcher(t, a, func() bool {
		a.onFilesRowActivated(0, changes.Row{Status: changes.RowConflict, RelPath: "f.txt"})
		return true
	})

	if view := waitForConflictView(t, a, views); view.Path() != "f.txt" {
		t.Fatalf("path = %q", view.Path())
	}
}

func TestTheirSideFallsBackWhenTheOperationHasNoName(t *testing.T) {
	for _, tt := range []struct {
		name  string
		state ops.MergeState
		want  string
	}{
		{"nothing in progress", ops.MergeState{}, i18n.T("Dialog.Conflict.Side.Theirs")},
		{"a rebase without a subject", ops.MergeState{Rebasing: true}, i18n.T("Dialog.Conflict.Side.Theirs")},
		{"a rebase with one", ops.MergeState{Rebasing: true, Message: "topic work\n\nbody"}, "topic work"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := theirsName(tt.state); got != tt.want {
				t.Fatalf("label = %q, want %q", got, tt.want)
			}
		})
	}
}
