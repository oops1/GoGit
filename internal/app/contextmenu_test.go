package app

import (
	"errors"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/changes"
	"github.com/oops1/gogit/internal/ui/journal"
	"github.com/oops1/gogit/internal/ui/repos"
)

func captureClipboard(t *testing.T) *string {
	t.Helper()
	copied := new(string)
	prev := setClipboard
	setClipboard = func(text string) { *copied = text }
	t.Cleanup(func() { setClipboard = prev })
	return copied
}

func captureStartedTools(t *testing.T) *[]string {
	t.Helper()
	started := new([]string)
	prev := startCommand
	startCommand = func(cmd *exec.Cmd) error {
		*started = append(*started, cmd.String())
		return nil
	}
	t.Cleanup(func() { startCommand = prev })
	return started
}

func menuTexts(items []widget.MenuItem) []string {
	texts := make([]string, 0, len(items))
	for _, item := range items {
		if item.Separator {
			texts = append(texts, "-")
			continue
		}
		texts = append(texts, item.Text)
	}
	return texts
}

func clickMenuItem(t *testing.T, items []widget.MenuItem, key string) {
	t.Helper()
	want := i18n.T(key)
	for _, item := range items {
		if item.Text == want {
			item.OnClick()
			return
		}
	}
	t.Fatalf("menu %v has no item %q", menuTexts(items), want)
}

func TestTheTreeMenuOfARepositoryOffersThePathActions(t *testing.T) {
	a, main, _ := newWorktreeTestApp(t)
	clipboard := captureClipboard(t)
	started := captureStartedTools(t)

	items := a.treeMenu(repos.MenuTarget{ID: "r1"})

	clickMenuItem(t, items, "Menu.Context.CopyPath")
	if *clipboard != main {
		t.Fatalf("clipboard = %q, want the repository path", *clipboard)
	}
	clickMenuItem(t, items, "Menu.Context.Reveal")
	clickMenuItem(t, items, "Menu.Context.Terminal")
	if len(*started) != 2 {
		t.Fatalf("started = %v, want the file manager and the terminal", *started)
	}
}

func TestTheMenuOfTheOpenRepositoryOffersToCloseIt(t *testing.T) {
	a, _, _ := newWorktreeTestApp(t)

	items := a.treeMenu(repos.MenuTarget{ID: "r1"})

	clickMenuItem(t, items, "Menu.Repository.CloseRepository")
	if a.State().ActiveRepository != "" {
		t.Fatal("the repository must close")
	}
	if _, has := findMenuItem(a.treeMenu(repos.MenuTarget{ID: "r1"}), i18n.T("Menu.Repository.CloseRepository")); has {
		t.Fatal("a repository that is not open cannot be closed")
	}
}

func findMenuItem(items []widget.MenuItem, text string) (widget.MenuItem, bool) {
	for _, item := range items {
		if item.Text == text {
			return item, true
		}
	}
	return widget.MenuItem{}, false
}

func TestTheMenuOfARepositoryOpensIt(t *testing.T) {
	a, _, _ := newWorktreeTestApp(t)
	a.CloseRepository()

	clickMenuItem(t, a.treeMenu(repos.MenuTarget{ID: "r1"}), "Menu.Context.Open")

	if a.State().ActiveRepository != "r1" {
		t.Fatal("the repository must open")
	}
}

func TestTheMenuOfAGroupAndOfEmptySpaceOffersToAdd(t *testing.T) {
	a, _ := appWithGroups(t)

	for _, target := range []repos.MenuTarget{{ID: "work"}, {}, {ID: "missing"}} {
		items := a.treeMenu(target)
		if len(items) != 2 {
			t.Fatalf("menu = %v, want the two add items", menuTexts(items))
		}
		if _, ok := findMenuItem(items, i18n.T("Menu.Repository.AddGroup")); !ok {
			t.Fatalf("menu = %v, want the group item", menuTexts(items))
		}
	}
}

func TestTheMenuOfADirectoryRowActsOnThatDirectory(t *testing.T) {
	a, _ := appWithGroups(t)
	clipboard := captureClipboard(t)
	dir := filepath.Join(t.TempDir(), "src")

	clickMenuItem(t, a.treeMenu(repos.MenuTarget{RepoID: "r1", Directory: dir}), "Menu.Context.CopyPath")

	if *clipboard != dir {
		t.Fatalf("clipboard = %q, want the directory", *clipboard)
	}
}

func TestTheFilesMenuActsOnTheFileUnderTheCursor(t *testing.T) {
	a, main, _ := newWorktreeTestApp(t)
	clipboard := captureClipboard(t)
	started := captureStartedTools(t)
	row := changes.Row{Name: "a.txt", RelPath: "src/a.txt"}

	items := a.filesMenu(row, 0)

	clickMenuItem(t, items, "Menu.Context.CopyPath")
	if want := filepath.Join(main, "src", "a.txt"); *clipboard != want {
		t.Fatalf("clipboard = %q, want %q", *clipboard, want)
	}
	clickMenuItem(t, items, "Menu.Context.Terminal")
	if len(*started) != 1 {
		t.Fatalf("started = %v, want the terminal alone", *started)
	}
	if _, ok := findMenuItem(items, i18n.T("Menu.Edit.Stage")); !ok {
		t.Fatalf("menu = %v, want the staging items", menuTexts(items))
	}
}

func TestTheFilesMenuIsEmptyForAnythingButAFileRow(t *testing.T) {
	a, _, _ := newWorktreeTestApp(t)

	if items := a.filesMenu("not a row", -1); items != nil {
		t.Fatalf("menu = %v, want none", menuTexts(items))
	}
}

func TestTheStagingItemsFollowTheState(t *testing.T) {
	a, _, _ := newWorktreeTestApp(t)

	items := a.editItems()

	for _, item := range items {
		if !item.Disabled {
			t.Fatalf("item %q must be disabled without a selection", item.Text)
		}
	}
	a.setFilesSelected(true)
	if a.editItems()[0].Disabled {
		t.Fatal("staging must be offered once a file is selected")
	}
}

func TestAFilePathNeedsAnOpenRepository(t *testing.T) {
	a := newTestApp(t)

	if path := a.filePathOf(changes.Row{RelPath: "a.txt"}); path != "" {
		t.Fatalf("path = %q, want none without an open repository", path)
	}
}

func TestTheJournalMenuCopiesTheCommit(t *testing.T) {
	a, _, _ := newWorktreeTestApp(t)
	clipboard := captureClipboard(t)
	id, err := hash.Parse("1111111111111111111111111111111111111111")
	if err != nil {
		t.Fatal(err)
	}
	row := journal.Row{ID: id, Message: "first commit"}

	items := a.journalMenu(row, 0)

	clickMenuItem(t, items, "Menu.Context.CopyHash")
	if *clipboard != id.String() {
		t.Fatalf("clipboard = %q, want the hash", *clipboard)
	}
	clickMenuItem(t, items, "Menu.Context.CopyMessage")
	if *clipboard != "first commit" {
		t.Fatalf("clipboard = %q, want the message", *clipboard)
	}
}

func TestTheJournalMenuIsEmptyForAnythingButACommitRow(t *testing.T) {
	a := newTestApp(t)

	if items := a.journalMenu(42, -1); items != nil {
		t.Fatalf("menu = %v, want none", menuTexts(items))
	}
}

func TestTheMenusAreWiredToTheWidgets(t *testing.T) {
	a := newTestApp(t)

	if a.reposView.OnMenu == nil {
		t.Fatal("the repository tree must offer a menu")
	}
	if a.filesGrid.Data().RowContextMenu == nil {
		t.Fatal("the files table must offer a menu")
	}
	if a.journalGrid().RowContextMenu == nil {
		t.Fatal("the journal must offer a menu")
	}
}

func TestNothingIsCopiedOrOpenedForAnEmptyPath(t *testing.T) {
	a := newTestApp(t)
	clipboard := captureClipboard(t)
	started := captureStartedTools(t)

	a.copyToClipboard("")
	a.revealPath("")
	a.openTerminalAt("")

	if *clipboard != "" || len(*started) != 0 {
		t.Fatalf("clipboard = %q, started = %v, want nothing to happen", *clipboard, *started)
	}
	if dir := containingDirectory(""); dir != "" {
		t.Fatalf("directory = %q, want none", dir)
	}
}

func TestAToolThatDoesNotStartIsLogged(t *testing.T) {
	a := newTestApp(t)
	prev := startCommand
	startCommand = func(*exec.Cmd) error { return errors.New("no such tool") }
	t.Cleanup(func() { startCommand = prev })

	a.revealPath(filepath.Join(t.TempDir(), "a.txt"))
	a.openTerminalAt(t.TempDir())
}

func TestTheFilesMenuShowsTheFileInItsFolder(t *testing.T) {
	a, _, _ := newWorktreeTestApp(t)
	started := captureStartedTools(t)

	clickMenuItem(t, a.filesMenu(changes.Row{Name: "a.txt", RelPath: "a.txt"}, 0), "Menu.Context.Reveal")

	if len(*started) != 1 {
		t.Fatalf("started = %v, want the file manager", *started)
	}
}
