package app

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/changes"
)

func TestTheFilesMenuFollowsSmartGitAndGreysWhatIsNotThereYet(t *testing.T) {
	a, _, _ := newWorktreeTestApp(t)
	clipboard := captureClipboard(t)
	started := captureStartedTools(t)
	row := changes.Row{Name: "a.txt", RelPath: "src/a.txt", Status: changes.RowUntracked, WorkingState: "untracked"}

	items := a.filesMenu(row, 0)

	key := i18n.T
	want := []string{
		key("Menu.Files.OpenFile"), key("Menu.Context.Reveal"), key("Menu.Files.Edit"), key("Menu.Files.SetExecutable"), key("Menu.Files.UnsetExecutable"), "-",
		key("Menu.Files.ShowChanges"), key("Menu.Context.FileHistory"), key("Menu.Context.Blame"), key("Menu.Files.Investigate"), "-",
		key("Menu.Edit.Commit"), key("Menu.Files.StashSelection"), "-",
		key("Menu.Edit.Stage"), key("Menu.Edit.Unstage"), key("Menu.Files.IndexEditor"), key("Menu.Files.Rename"), "-",
		key("Menu.Files.ConflictSolver"), key("Menu.Files.Resolve"), "-",
		key("Menu.Files.Ignore"), key("Menu.Edit.Discard"), key("Menu.Files.Remove"), key("Menu.Files.Delete"), "-",
		key("Menu.Files.CopyName"), key("Menu.Context.CopyPath"), key("Menu.Files.CopyRelativePath"), "-",
		key("Menu.Files.SelectDirectory"), key("Menu.Files.SelectRoot"),
	}
	if got := menuTexts(items); !slices.Equal(got, want) {
		t.Fatalf("menu = %v\nwant %v", got, want)
	}
	for _, k := range []string{
		"Menu.Files.Edit", "Menu.Files.SetExecutable", "Menu.Files.UnsetExecutable", "Menu.Files.Investigate",
		"Menu.Files.StashSelection", "Menu.Files.IndexEditor", "Menu.Files.Rename", "Menu.Files.Ignore", "Menu.Files.Remove",
		"Menu.Files.SelectDirectory", "Menu.Files.SelectRoot",
		"Menu.Context.FileHistory", "Menu.Context.Blame", "Menu.Edit.Unstage", "Menu.Edit.Discard",
		"Menu.Files.ConflictSolver", "Menu.Files.Resolve", "Menu.Files.Delete",
	} {
		if item, _ := findMenuItem(items, key(k)); !item.Disabled {
			t.Errorf("%s must be disabled for an untracked file outside the working table", k)
		}
	}
	clickMenuItem(t, items, "Menu.Files.CopyName")
	if *clipboard != "a.txt" {
		t.Fatalf("clipboard = %q, want the name", *clipboard)
	}
	clickMenuItem(t, items, "Menu.Files.CopyRelativePath")
	if *clipboard != "src/a.txt" {
		t.Fatalf("clipboard = %q, want the relative path", *clipboard)
	}
	clickMenuItem(t, items, "Menu.Context.Reveal")
	if len(*started) != 1 {
		t.Fatalf("started = %v, want the file manager", *started)
	}
}

func TestAConflictedFileOffersTheConflictSolverAndTheResolveMenu(t *testing.T) {
	a, _, _ := newWorktreeTestApp(t)

	items := a.filesMenu(changes.Row{Name: "f.txt", RelPath: "f.txt", Status: changes.RowConflict}, 0)

	solver, _ := findMenuItem(items, i18n.T("Menu.Files.ConflictSolver"))
	resolve, _ := findMenuItem(items, i18n.T("Menu.Files.Resolve"))
	if solver.Disabled || resolve.Disabled || len(resolve.SubItems) != 3 {
		t.Fatalf("solver disabled %v, resolve = %+v", solver.Disabled, resolve)
	}
	if resolve.SubItems[0].Text != i18n.T("Menu.Context.TakeOurs") {
		t.Fatalf("resolve items = %v", menuTexts(resolve.SubItems))
	}
	solver.OnClick()
	asked := false
	a.askConfirm = func(_, _ string, cb func(bool)) { asked = true; cb(false) }
	remove, _ := findMenuItem(items, i18n.T("Menu.Files.Delete"))
	remove.OnClick()
	if !asked {
		t.Fatal("deleting from the menu must ask first")
	}
}

func TestARowWithoutAPathHasNoHistoryItems(t *testing.T) {
	a := newTestApp(t)

	items := a.filesMenu(changes.Row{Status: changes.RowModified}, 0)

	for _, k := range []string{"Menu.Context.FileHistory", "Menu.Files.CopyName", "Menu.Context.CopyPath", "Menu.Files.OpenFile"} {
		if item, found := findMenuItem(items, i18n.T(k)); k == "Menu.Context.FileHistory" && found || k != "Menu.Context.FileHistory" && !item.Disabled {
			t.Fatalf("%s: found %v, disabled %v", k, found, item.Disabled)
		}
	}
}

func TestDeletingAFileAsksFirstAndReportsAFailure(t *testing.T) {
	a, main, _ := newWorktreeTestApp(t)
	path := filepath.Join(main, "gone.txt")
	if err := os.WriteFile(path, []byte("x\n"), 0o666); err != nil {
		t.Fatal(err)
	}
	answer := false
	a.askConfirm = func(_, _ string, cb func(bool)) { cb(answer) }

	a.deleteFile(path)
	a.writeWG.Wait()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("a declined deletion removed the file: %v", err)
	}

	answer = true
	a.deleteFile(path)
	a.writeWG.Wait()
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stat after deletion = %v, want the file gone", err)
	}

	prev := removePath
	removePath = func(string) error { return errors.New("locked") }
	t.Cleanup(func() { removePath = prev })
	a.deleteFile(path)
	a.writeWG.Wait()
	waitForStatusText(t, a, i18n.Tf("Status.DeleteFailed", errors.New("locked")))
}
