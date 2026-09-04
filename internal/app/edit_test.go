package app

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/oops1/headless-gui/v3/widget"
	"github.com/oops1/headless-gui/v3/widget/datagrid"

	"github.com/oops1/gogit/internal/config"
	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/index"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/refs"
	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/gitcore/worktree"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/repo/watch"
	"github.com/oops1/gogit/internal/ui/changes"
	"github.com/oops1/gogit/internal/ui/commit"
	"github.com/oops1/gogit/internal/ui/filesgrid"
	"github.com/oops1/gogit/internal/ui/journal"
)

func setTestUserIdentity(t *testing.T, target string) {
	t.Helper()
	r, err := gitrepo.Open(target, gitrepo.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	data, err := r.CommonRoot().ReadFile("config")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, []byte("[user]\n\tname = Go Git\n\temail = gogit@example.com\n")...)
	if err := r.CommonRoot().WriteFile("config", data, 0o666); err != nil {
		t.Fatal(err)
	}
}

func selectAndActivateFilesRow(t *testing.T, a *App, idx int) changes.Row {
	t.Helper()
	row := filesRowOnDispatcher(t, a, idx)
	grid := a.Widget("filesGrid").(*filesgrid.Grid)
	grid.Data().Grid.SetSelectedIndex(idx)
	grid.Data().Grid.OnSelectionChanged(datagrid.SelectionChangedEvent{SelectedIndex: idx, SelectedItem: row})
	return row
}

func buildStagedFileFixture(t *testing.T, target string) {
	t.Helper()
	initTestRepoWithBranch(t, target, "main")
	r, err := gitrepo.Open(target, gitrepo.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	db, err := odb.Open(r.ObjectsDir(), odb.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	store, err := refs.Open(refs.Options{GitDir: r.GitDir(), CommonDir: r.CommonDir(), Committer: testCommitter})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()

	baseID := putChangesBlob(t, db, "base\n")
	tree := &object.Tree{Entries: []object.TreeEntry{{Mode: object.ModeBlob, Name: "base.txt", ID: baseID}}}
	tree.Sort()
	treeID, err := db.PutObject(tree)
	if err != nil {
		t.Fatal(err)
	}
	commitID := putChangesCommit(t, db, treeID)
	setRef(t, store, refs.BranchName("main"), commitID)

	stagedID := putChangesBlob(t, db, "new file\n")
	idx := index.New(index.Version2)
	idx.Add(index.Entry{Path: "base.txt", Mode: object.ModeBlob, ID: baseID, Stage: index.StageMerged})
	idx.Add(index.Entry{Path: "staged.txt", Mode: object.ModeBlob, ID: stagedID, Stage: index.StageMerged})
	if err := idx.WriteFile(r.IndexFile(), index.Version2); err != nil {
		t.Fatal(err)
	}

	if err := writeFile(target, "base.txt", "base\n"); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(target, "staged.txt", "new file\n"); err != nil {
		t.Fatal(err)
	}
}

func stubShowCommit(a *App, m commit.Model, ok bool) {
	a.showCommit = func(_ commit.Model, cb func(commit.Model, bool)) { cb(m, ok) }
}

func waitForFileRowState(t *testing.T, a *App, path, want string) {
	t.Helper()
	deadline := time.Now().Add(testTimeout)
	for {
		n := filesRowCountOnDispatcher(t, a)
		for i := range n {
			row := filesRowOnDispatcher(t, a, i)
			if row.RelPath == path && row.State == want {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("file %q did not reach state %q in time", path, want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func waitForFileRowAbsent(t *testing.T, a *App, path string) {
	t.Helper()
	deadline := time.Now().Add(testTimeout)
	for {
		n := filesRowCountOnDispatcher(t, a)
		present := false
		for i := range n {
			if filesRowOnDispatcher(t, a, i).RelPath == path {
				present = true
				break
			}
		}
		if !present {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("file %q was still present in time", path)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func waitForJournalHeadChange(t *testing.T, a *App, prev hash.ObjectID) journal.Row {
	t.Helper()
	deadline := time.Now().Add(testTimeout)
	for {
		if journalRowCountOnDispatcher(t, a) > 0 {
			row := journalRowOnDispatcher(t, a, 0)
			if !row.ID.IsZero() && row.ID != prev {
				return row
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("journal head did not change in time")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestFilesGridUsesExtendedSelectionMode(t *testing.T) {
	a := newTestApp(t)
	grid := a.Widget("filesGrid").(*filesgrid.Grid)
	if grid.Data().Grid.SelectionMode != datagrid.SelectionExtended {
		t.Fatal("filesGrid must use SelectionExtended so multi-select works once the engine forwards Ctrl/Shift")
	}
}

func TestEditMenuStructure(t *testing.T) {
	a := newTestApp(t)
	items := a.menu.Items()
	if len(items[editMenuIndex].Items) != len(editMenuTree) {
		t.Fatalf("edit sub items = %d, want %d", len(items[editMenuIndex].Items), len(editMenuTree))
	}
	for i, entry := range editMenuTree {
		item := items[editMenuIndex].Items[i]
		if entry.Separator {
			if !item.Separator {
				t.Fatalf("item %d should be a separator", i)
			}
			continue
		}
		if item.Separator {
			t.Fatalf("item %d should not be a separator", i)
		}
		if entry.Leaf == nil {
			t.Fatalf("item %d must be a leaf", i)
		}
		if item.Text != widget.Tr(entry.Leaf.Key) {
			t.Fatalf("item %d text = %q, want %q", i, item.Text, widget.Tr(entry.Leaf.Key))
		}
	}
}

func TestEditCommandsDisabledWithoutAnActiveRepository(t *testing.T) {
	a := newTestApp(t)
	for _, cmd := range []CommandID{CmdStage, CmdUnstage, CmdDiscard, CmdCommit} {
		if _, enabled, ok := a.MenuItemByCommand(cmd); !ok || enabled {
			t.Fatalf("%s must be disabled without an active repository", cmd)
		}
	}
}

func TestStageUnstageDiscardFollowFilesSelection(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	buildWorkingRepoFixture(t, target)
	a := activatedWorkingApp(t, target)
	waitForWorkingRows(t, a, 4)

	for _, cmd := range []CommandID{CmdStage, CmdUnstage, CmdDiscard} {
		if _, enabled, ok := a.MenuItemByCommand(cmd); !ok || enabled {
			t.Fatalf("%s must be disabled without a files selection", cmd)
		}
	}

	selectAndActivateFilesRow(t, a, 3)

	for _, cmd := range []CommandID{CmdStage, CmdUnstage, CmdDiscard} {
		if _, enabled, ok := a.MenuItemByCommand(cmd); !ok || !enabled {
			t.Fatalf("%s must be enabled once a working row is selected", cmd)
		}
	}
}

func TestCommitDisabledWithoutStagedChangesEvenWhenActive(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	initTestRepoWithBranch(t, target, "main")
	a := activatedWorkingApp(t, target)
	waitForWorkingRows(t, a, 0)

	if _, enabled, ok := a.MenuItemByCommand(CmdCommit); !ok || enabled {
		t.Fatal("commit must be disabled without staged changes")
	}
}

func TestSelectingACommitRowDisablesStageUnstageDiscard(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	initTestRepoWithBranch(t, target, "main")
	db, store := withJournalRepo(t, target)
	tree := putChangesTree(t, db, map[string]string{"a.txt": "hello\n"})
	root := putChangesCommit(t, db, tree)
	setRef(t, store, refs.BranchName("main"), root)
	a := activatedWorkingApp(t, target)
	waitForJournalRows(t, a, 1)
	selectJournalRow(t, a, 0)
	waitForFilesRows(t, a, 1)

	for _, cmd := range []CommandID{CmdStage, CmdUnstage, CmdDiscard} {
		if _, enabled, ok := a.MenuItemByCommand(cmd); !ok || enabled {
			t.Fatalf("%s must be disabled while a commit's diff is shown", cmd)
		}
	}
}

func TestStageSelectedUntrackedFileMovesItToAddedState(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	buildWorkingRepoFixture(t, target)
	a := activatedWorkingApp(t, target)
	waitForWorkingRows(t, a, 4)
	selectAndActivateFilesRow(t, a, 3)

	if !a.Dispatch(CmdStage) {
		t.Fatal("stage command must dispatch")
	}
	a.writeWG.Wait()

	waitForFileRowState(t, a, "untracked.txt", "Added")
	if a.State().FilesSelected {
		t.Fatal("a successful stage must clear the selection")
	}
}

func TestUnstageSelectedFileReturnsItToUntrackedState(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	buildWorkingRepoFixture(t, target)
	a := activatedWorkingApp(t, target)
	waitForWorkingRows(t, a, 4)
	selectAndActivateFilesRow(t, a, 1)

	if !a.Dispatch(CmdUnstage) {
		t.Fatal("unstage command must dispatch")
	}
	a.writeWG.Wait()

	waitForFileRowState(t, a, "staged.txt", "Untracked")
}

func TestDiscardWithConfirmationRestoresFileContent(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	buildWorkingRepoFixture(t, target)
	a := activatedWorkingApp(t, target)
	waitForWorkingRows(t, a, 4)
	a.askConfirm = func(_, _ string, cb func(bool)) { cb(true) }
	selectAndActivateFilesRow(t, a, 2)

	if !a.Dispatch(CmdDiscard) {
		t.Fatal("discard command must dispatch")
	}
	a.writeWG.Wait()

	waitForFileRowAbsent(t, a, "modified.txt")
	data, err := os.ReadFile(filepath.Join(target, "modified.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "old\n" {
		t.Fatalf("content = %q, want the original content restored", data)
	}
}

func TestDiscardWithoutConfirmationLeavesTheFileUnchanged(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	buildWorkingRepoFixture(t, target)
	a := activatedWorkingApp(t, target)
	waitForWorkingRows(t, a, 4)
	askCalled := 0
	a.askConfirm = func(_, _ string, cb func(bool)) { askCalled++; cb(false) }
	selectAndActivateFilesRow(t, a, 2)

	if !a.Dispatch(CmdDiscard) {
		t.Fatal("discard command must dispatch")
	}
	waitForPostQueueDrain(t, a)

	if askCalled != 1 {
		t.Fatalf("askConfirm called = %d, want 1", askCalled)
	}
	data, err := os.ReadFile(filepath.Join(target, "modified.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "new content\n" {
		t.Fatalf("content = %q, must stay unchanged without confirmation", data)
	}
}

func TestDiscardConfirmedRemovesAnUntrackedFile(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	buildWorkingRepoFixture(t, target)
	a := activatedWorkingApp(t, target)
	waitForWorkingRows(t, a, 4)
	a.askConfirm = func(_, _ string, cb func(bool)) { cb(true) }
	selectAndActivateFilesRow(t, a, 3)

	if !a.Dispatch(CmdDiscard) {
		t.Fatal("discard command must dispatch")
	}
	a.writeWG.Wait()

	waitForFileRowAbsent(t, a, "untracked.txt")
	if _, err := os.Stat(filepath.Join(target, "untracked.txt")); !os.IsNotExist(err) {
		t.Fatalf("untracked.txt must be removed, stat err = %v", err)
	}
}

func TestStageUnstageDiscardWithNoSelectionAreNoOps(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	buildWorkingRepoFixture(t, target)
	a := activatedWorkingApp(t, target)
	waitForWorkingRows(t, a, 4)

	a.stageSelected()
	a.unstageSelected()
	a.discardSelected()
	waitForPostQueueDrain(t, a)
}

func TestSelectedWorkingPathsIgnoresCommitModeSelections(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	initTestRepoWithBranch(t, target, "main")
	db, store := withJournalRepo(t, target)
	tree := putChangesTree(t, db, map[string]string{"a.txt": "hello\n"})
	root := putChangesCommit(t, db, tree)
	setRef(t, store, refs.BranchName("main"), root)
	a := activatedWorkingApp(t, target)
	waitForJournalRows(t, a, 1)
	selectJournalRow(t, a, 0)
	waitForFilesRows(t, a, 1)

	if got := a.selectedWorkingPaths(); len(got) != 0 {
		t.Fatalf("selectedWorkingPaths = %v, want none in commit mode", got)
	}
}

func TestSelectedWorkingPathsIsEmptyWithoutASelection(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	initTestRepoWithBranch(t, target, "main")
	a := activatedWorkingApp(t, target)
	waitForWorkingRows(t, a, 0)

	a.filesMu.Lock()
	a.filesMode = filesModeWorking
	a.filesMu.Unlock()
	a.filesItems.SetItems([]interface{}{
		changes.Row{Name: "a.txt", RelPath: "a.txt"},
		changes.Row{Name: "truncated"},
	})

	if got := a.selectedWorkingPaths(); len(got) != 0 {
		t.Fatalf("selectedWorkingPaths = %v, want none without a selection", got)
	}
}

func TestSelectedWorkingPathsSkipsARowWithABlankRelPath(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	initTestRepoWithBranch(t, target, "main")
	a := activatedWorkingApp(t, target)
	waitForWorkingRows(t, a, 0)

	a.filesMu.Lock()
	a.filesMode = filesModeWorking
	a.filesMu.Unlock()
	a.filesItems.SetItems([]interface{}{changes.Row{Name: "truncated"}})
	grid := a.Widget("filesGrid").(*filesgrid.Grid)
	grid.Data().Grid.SetSelectedIndex(0)

	if got := a.selectedWorkingPaths(); len(got) != 0 {
		t.Fatalf("selectedWorkingPaths = %v, want none for a blank RelPath", got)
	}
}

func TestStageFailedLogsWarningAndSetsStatus(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	buildWorkingRepoFixture(t, target)
	a := activatedWorkingApp(t, target)
	waitForWorkingRows(t, a, 4)

	a.filesMu.Lock()
	a.filesMode = filesModeWorking
	a.filesMu.Unlock()
	a.filesItems.SetItems([]interface{}{changes.Row{Name: "evil", RelPath: "../evil"}})
	grid := a.Widget("filesGrid").(*filesgrid.Grid)
	grid.Data().Grid.SetSelectedIndex(0)
	a.setFilesSelected(true)

	if !a.Dispatch(CmdStage) {
		t.Fatal("stage command must dispatch when the grid reports a selection")
	}
	a.writeWG.Wait()
	waitForPostQueueDrain(t, a)

	got := a.Widget("statusText").(*widget.Label).Text()
	want := "Could not stage the selected files"
	if len(got) < len(want) || got[:len(want)] != want {
		t.Fatalf("status text = %q, want prefix %q", got, want)
	}
}

func TestUnstageFailedLogsWarningAndSetsStatus(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	buildWorkingRepoFixture(t, target)
	a := activatedWorkingApp(t, target)
	waitForWorkingRows(t, a, 4)

	a.filesMu.Lock()
	a.filesMode = filesModeWorking
	a.filesMu.Unlock()
	a.filesItems.SetItems([]interface{}{changes.Row{Name: "evil", RelPath: "../evil"}})
	grid := a.Widget("filesGrid").(*filesgrid.Grid)
	grid.Data().Grid.SetSelectedIndex(0)
	a.setFilesSelected(true)

	if !a.Dispatch(CmdUnstage) {
		t.Fatal("unstage command must dispatch when the grid reports a selection")
	}
	a.writeWG.Wait()
	waitForPostQueueDrain(t, a)

	got := a.Widget("statusText").(*widget.Label).Text()
	want := "Could not unstage the selected files"
	if len(got) < len(want) || got[:len(want)] != want {
		t.Fatalf("status text = %q, want prefix %q", got, want)
	}
}

func TestDiscardFailedLogsWarningAndSetsStatus(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	buildWorkingRepoFixture(t, target)
	a := activatedWorkingApp(t, target)
	waitForWorkingRows(t, a, 4)
	a.askConfirm = func(_, _ string, cb func(bool)) { cb(true) }

	a.filesMu.Lock()
	a.filesMode = filesModeWorking
	a.filesMu.Unlock()
	a.filesItems.SetItems([]interface{}{changes.Row{Name: "evil", RelPath: "../evil"}})
	grid := a.Widget("filesGrid").(*filesgrid.Grid)
	grid.Data().Grid.SetSelectedIndex(0)
	a.setFilesSelected(true)

	if !a.Dispatch(CmdDiscard) {
		t.Fatal("discard command must dispatch when the grid reports a selection")
	}
	a.writeWG.Wait()
	waitForPostQueueDrain(t, a)

	got := a.Widget("statusText").(*widget.Label).Text()
	want := "Could not discard the selected changes"
	if len(got) < len(want) || got[:len(want)] != want {
		t.Fatalf("status text = %q, want prefix %q", got, want)
	}
}

func TestOnlyOneWriteOperationRunsAtATime(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	initTestRepoWithBranch(t, target, "main")
	a := activatedWorkingApp(t, target)
	waitForWorkingRows(t, a, 0)

	started := make(chan struct{})
	release := make(chan struct{})
	firstDone := make(chan struct{})
	firstStarted := a.startWrite(func(ctx context.Context, r *gitrepo.Repository) error {
		close(started)
		<-release
		return nil
	}, func(error) { close(firstDone) })
	if !firstStarted {
		t.Fatal("first write must start")
	}
	<-started

	secondCalled := false
	secondStarted := a.startWrite(func(ctx context.Context, r *gitrepo.Repository) error {
		secondCalled = true
		return nil
	}, func(error) {})
	if secondStarted {
		t.Fatal("startWrite must refuse a second concurrent write")
	}

	close(release)
	<-firstDone
	waitForPostQueueDrain(t, a)
	if secondCalled {
		t.Fatal("the refused write must never run")
	}
}

func TestStartWriteIsANoOpWithoutAnOpenRepository(t *testing.T) {
	a := newTestApp(t)
	called := false
	if a.startWrite(func(context.Context, *gitrepo.Repository) error { called = true; return nil }, func(error) {}) {
		t.Fatal("startWrite must refuse without an open repository")
	}
	if called {
		t.Fatal("the write function must not run without an open repository")
	}
}

func TestStopWriteCancelsAndWaitsForTheRunningOperation(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	initTestRepoWithBranch(t, target, "main")
	a := activatedWorkingApp(t, target)
	waitForWorkingRows(t, a, 0)

	started := make(chan struct{})
	var sawCancel error
	a.startWrite(func(ctx context.Context, r *gitrepo.Repository) error {
		close(started)
		<-ctx.Done()
		sawCancel = ctx.Err()
		return ctx.Err()
	}, func(error) {})
	<-started

	a.stopWrite()

	if !errors.Is(sawCancel, context.Canceled) {
		t.Fatalf("write function must observe cancellation, got %v", sawCancel)
	}
}

func TestStopWriteIsANoOpWithoutARunningOperation(t *testing.T) {
	a := newTestApp(t)
	a.stopWrite()
}

func TestWriteOperationPausesAndResumesTheWatcherAndPokesItAfterward(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	buildWorkingRepoFixture(t, target)
	cfg := config.Default()
	cfg.Repositories = []config.Repository{{ID: "r1", Name: "Main", Path: target}}
	a := newTestAppWithConfig(t, cfg)
	fw := newFakeWatcher()
	a.newWatcher = func(gitrepo.Layout, watch.Options) watcherIface { return fw }
	a.ActivateRepository("r1")
	<-fw.started
	waitForWorkingRows(t, a, 4)
	selectAndActivateFilesRow(t, a, 3)

	if !a.Dispatch(CmdStage) {
		t.Fatal("stage command must dispatch")
	}
	a.writeWG.Wait()

	if fw.pauses.Load() != 1 || fw.resumes.Load() != 1 || fw.pokes.Load() != 1 {
		t.Fatalf("pauses=%d resumes=%d pokes=%d, want 1 each", fw.pauses.Load(), fw.resumes.Load(), fw.pokes.Load())
	}
}

func TestOpenCommitDoesNothingWithoutAnOpenRepository(t *testing.T) {
	a := newTestApp(t)
	called := false
	a.showCommit = func(commit.Model, func(commit.Model, bool)) { called = true }
	a.openCommit()
	if called {
		t.Fatal("openCommit must not show the dialog without an open repository")
	}
}

func TestOpenCommitPassesStagedCountAndLastMessageToShowCommit(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	buildStagedFileFixture(t, target)
	a := activatedWorkingApp(t, target)
	waitForWorkingRows(t, a, 1)

	var got commit.Model
	called := 0
	a.showCommit = func(m commit.Model, _ func(commit.Model, bool)) { got = m; called++ }

	a.openCommit()

	if called != 1 {
		t.Fatalf("showCommit called = %d, want 1", called)
	}
	if got.Staged != 1 {
		t.Fatalf("staged = %d, want 1", got.Staged)
	}
	if got.LastMessage != "msg\n" {
		t.Fatalf("lastMessage = %q, want %q", got.LastMessage, "msg\n")
	}
}

func TestLastCommitMessageReturnsEmptyWithoutAnOpenRepository(t *testing.T) {
	a := newTestApp(t)
	if got := a.lastCommitMessage(); got != "" {
		t.Fatalf("lastCommitMessage = %q, want empty", got)
	}
}

func TestLastCommitMessageReturnsEmptyOnUnbornHead(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	initTestRepoWithBranch(t, target, "main")
	a := activatedWorkingApp(t, target)
	waitForWorkingRows(t, a, 0)

	if got := a.lastCommitMessage(); got != "" {
		t.Fatalf("lastCommitMessage = %q, want empty on an unborn HEAD", got)
	}
}

func TestLastCommitMessageReturnsEmptyWhenLoadingTheCommitFails(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	buildStagedFileFixture(t, target)
	a := activatedWorkingApp(t, target)
	waitForWorkingRows(t, a, 1)

	prev := loadCommitObject
	loadCommitObject = func(*odb.DB, hash.ObjectID) (*object.Commit, error) { return nil, errors.New("boom") }
	t.Cleanup(func() { loadCommitObject = prev })

	if got := a.lastCommitMessage(); got != "" {
		t.Fatalf("lastCommitMessage = %q, want empty when loading the commit fails", got)
	}
}

func TestReloadWorktreeIsANoOpWithoutAnOpenRepository(t *testing.T) {
	a := newTestApp(t)
	a.reloadWorktree()
}

func TestReloadWorktreeIsANoOpForABareRepository(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "bare.git")
	bare, err := gitrepo.Init(target, gitrepo.InitOptions{Bare: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := bare.Close(); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Repositories = []config.Repository{{ID: "r1", Name: "Bare", Path: target}}
	a := newTestAppWithConfig(t, cfg)
	a.ActivateRepository("r1")
	waitForPostQueueDrain(t, a)

	a.reloadWorktree()
}

func TestReloadWorktreeLogsWarningWhenOpenWorktreeFails(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	initTestRepoWithBranch(t, target, "main")
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	cfg := config.Default()
	cfg.Repositories = []config.Repository{{ID: "r1", Name: "Main", Path: target}}
	a, err := New(cfg, config.Paths{Dir: t.TempDir()}, logger)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)
	a.ActivateRepository("r1")
	waitForWorkingRows(t, a, 0)
	a.workingWG.Wait()

	prev := openWorktree
	openWorktree = func(*gitrepo.Repository, worktree.Options) (*worktree.Worktree, error) {
		return nil, errors.New("boom")
	}
	t.Cleanup(func() { openWorktree = prev })

	a.reloadWorktree()

	if !strings.Contains(buf.String(), "reload working tree failed") {
		t.Fatalf("expected reload failure to be logged: %s", buf.String())
	}
}

func TestReloadWorktreeLogsWarningWhenClosingThePreviousWorktreeFails(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	initTestRepoWithBranch(t, target, "main")
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	cfg := config.Default()
	cfg.Repositories = []config.Repository{{ID: "r1", Name: "Main", Path: target}}
	a, err := New(cfg, config.Paths{Dir: t.TempDir()}, logger)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)
	a.ActivateRepository("r1")
	waitForWorkingRows(t, a, 0)
	a.workingWG.Wait()

	prev := closeWorktree
	closeWorktree = func(w *worktree.Worktree) error {
		_ = prev(w)
		return errors.New("boom")
	}
	t.Cleanup(func() { closeWorktree = prev })

	a.reloadWorktree()

	if !strings.Contains(buf.String(), "close previous working tree failed") {
		t.Fatalf("expected close failure to be logged: %s", buf.String())
	}
}

func TestApplyCommitDoesNothingWhenNotOKOrWithoutAnOpenRepository(t *testing.T) {
	a := newTestApp(t)
	a.applyCommit(commit.Model{Message: "x"}, false)
	a.applyCommit(commit.Model{Message: "x"}, true)
	waitForPostQueueDrain(t, a)
}

func TestApplyCommitCreatesACommitUpdatesJournalAndStatus(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	buildStagedFileFixture(t, target)
	setTestUserIdentity(t, target)
	a := activatedWorkingApp(t, target)
	waitForWorkingRows(t, a, 1)
	waitForJournalRows(t, a, 1)
	original := journalRowOnDispatcher(t, a, 0)

	stubShowCommit(a, commit.Model{Message: "add staged file\n"}, true)

	if !a.Dispatch(CmdCommit) {
		t.Fatal("commit command must dispatch when there are staged changes")
	}
	a.writeWG.Wait()

	head := waitForJournalHeadChange(t, a, original.ID)
	if head.Message != "add staged file" {
		t.Fatalf("journal head message = %q", head.Message)
	}
	wantStatus := i18n.Tf("Status.Committed", head.ShortHash)
	deadline := time.Now().Add(testTimeout)
	for {
		if got := a.Widget("statusText").(*widget.Label).Text(); got == wantStatus {
			break
		} else if time.Now().After(deadline) {
			t.Fatalf("status text = %q, want %q", got, wantStatus)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if a.State().FilesSelected {
		t.Fatal("a successful commit must clear the files selection")
	}
}

func TestApplyCommitWithAmendReplacesTheLastCommit(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	buildStagedFileFixture(t, target)
	setTestUserIdentity(t, target)
	a := activatedWorkingApp(t, target)
	waitForWorkingRows(t, a, 1)
	waitForJournalRows(t, a, 1)
	original := journalRowOnDispatcher(t, a, 0)

	stubShowCommit(a, commit.Model{Message: "amended message\n", Amend: true}, true)

	if !a.Dispatch(CmdCommit) {
		t.Fatal("commit command must dispatch")
	}
	a.writeWG.Wait()

	amended := waitForJournalHeadChange(t, a, original.ID)
	if amended.Message != "amended message" {
		t.Fatalf("message = %q, want amended message", amended.Message)
	}
	if got := journalRowCountOnDispatcher(t, a); got != 1 {
		t.Fatalf("journal row count = %d, want 1 after amend", got)
	}
}

func TestApplyCommitFailedWithABlankMessageLogsWarningAndSetsStatus(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	buildStagedFileFixture(t, target)
	setTestUserIdentity(t, target)
	a := activatedWorkingApp(t, target)
	waitForWorkingRows(t, a, 1)

	stubShowCommit(a, commit.Model{Message: "   \n\t"}, true)

	if !a.Dispatch(CmdCommit) {
		t.Fatal("commit command must dispatch")
	}
	a.writeWG.Wait()
	waitForPostQueueDrain(t, a)

	got := a.Widget("statusText").(*widget.Label).Text()
	want := "Could not commit"
	if len(got) < len(want) || got[:len(want)] != want {
		t.Fatalf("status text = %q, want prefix %q", got, want)
	}
}

func TestApplyCommitSkipsWhenAnotherWriteIsInProgress(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	buildStagedFileFixture(t, target)
	setTestUserIdentity(t, target)
	a := activatedWorkingApp(t, target)
	waitForWorkingRows(t, a, 1)

	started := make(chan struct{})
	release := make(chan struct{})
	a.startWrite(func(ctx context.Context, r *gitrepo.Repository) error {
		close(started)
		<-release
		return nil
	}, func(error) {})
	<-started
	defer close(release)

	a.applyCommit(commit.Model{Message: "queued\n"}, true)
	waitForPostQueueDrain(t, a)
}

func TestDefaultShowCommitShowsModalWithoutPanicking(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	buildWorkingRepoFixture(t, target)
	a := activatedWorkingApp(t, target)
	waitForWorkingRows(t, a, 4)

	a.showCommit(commit.Model{Staged: 1}, func(commit.Model, bool) {})
}

func TestDefaultShowCommitLogsWarningWhenViewCreationFails(t *testing.T) {
	a := newTestApp(t)
	prev := newCommitView
	wantErr := errors.New("boom")
	newCommitView = func(widget.ModalShower, commit.Model) (*commit.View, error) {
		return nil, wantErr
	}
	t.Cleanup(func() { newCommitView = prev })

	called := false
	a.showCommit(commit.Model{}, func(commit.Model, bool) { called = true })

	if called {
		t.Fatal("callback must not run when the dialog fails to open")
	}
}

func TestWireCommitViewInvokesCallbackOnSuccessfulOpen(t *testing.T) {
	a := newTestApp(t)
	view, err := commit.NewView(a.Engine(), commit.Model{})
	if err != nil {
		t.Fatal(err)
	}

	var got commit.Model
	var gotOK bool
	a.wireCommitView(view, func(m commit.Model, ok bool) { got, gotOK = m, ok })

	want := commit.Model{Message: "hello\n"}
	view.OnOK(want)

	if !gotOK || got != want {
		t.Fatalf("result = %+v, ok = %v", got, gotOK)
	}
}

func TestWireCommitViewReportsNotOKOnCancel(t *testing.T) {
	a := newTestApp(t)
	view, err := commit.NewView(a.Engine(), commit.Model{})
	if err != nil {
		t.Fatal(err)
	}

	gotOK := true
	a.wireCommitView(view, func(_ commit.Model, ok bool) { gotOK = ok })

	view.OnCancel()

	if gotOK {
		t.Fatal("cancel must report ok = false")
	}
}
