package app

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/oops1/headless-gui/v3/widget/datagrid"

	"github.com/oops1/gogit/internal/config"
	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/index"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/refs"
	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/gitcore/worktree"
	"github.com/oops1/gogit/internal/repo/watch"
	"github.com/oops1/gogit/internal/ui/changes"
	"github.com/oops1/gogit/internal/ui/diffview"
)

func TestOpenRepositoryAtFailsAndCleansUpWhenWorktreeOpenFails(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	initTestRepoWithBranch(t, target, "main")
	prev := openWorktree
	openWorktree = func(*gitrepo.Repository, worktree.Options) (*worktree.Worktree, error) {
		return nil, errors.New("boom")
	}
	t.Cleanup(func() { openWorktree = prev })

	if _, _, err := openRepositoryAt("r1", target); err == nil || err.Error() != "boom" {
		t.Fatalf("openRepositoryAt returned error %v, want boom", err)
	}
}

func buildWorkingRepoFixture(t *testing.T, target string) {
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

	cleanID := putChangesBlob(t, db, "same\n")
	modifiedID := putChangesBlob(t, db, "old\n")
	tree := &object.Tree{Entries: []object.TreeEntry{
		{Mode: object.ModeBlob, Name: "clean.txt", ID: cleanID},
		{Mode: object.ModeBlob, Name: "modified.txt", ID: modifiedID},
	}}
	tree.Sort()
	treeID, err := db.PutObject(tree)
	if err != nil {
		t.Fatal(err)
	}
	commitID := putChangesCommit(t, db, treeID)
	setRef(t, store, refs.BranchName("main"), commitID)

	stagedID := putChangesBlob(t, db, "new file\n")
	ancestorID := putChangesBlob(t, db, "ancestor\n")
	oursID := putChangesBlob(t, db, "ours\n")
	theirsID := putChangesBlob(t, db, "theirs\n")

	idx := index.New(index.Version2)
	idx.Add(index.Entry{Path: "clean.txt", Mode: object.ModeBlob, ID: cleanID, Stage: index.StageMerged})
	idx.Add(index.Entry{Path: "modified.txt", Mode: object.ModeBlob, ID: modifiedID, Stage: index.StageMerged})
	idx.Add(index.Entry{Path: "staged.txt", Mode: object.ModeBlob, ID: stagedID, Stage: index.StageMerged})
	idx.Add(index.Entry{Path: "conflict.txt", Mode: object.ModeBlob, ID: ancestorID, Stage: index.StageAncestor})
	idx.Add(index.Entry{Path: "conflict.txt", Mode: object.ModeBlob, ID: oursID, Stage: index.StageOurs})
	idx.Add(index.Entry{Path: "conflict.txt", Mode: object.ModeBlob, ID: theirsID, Stage: index.StageTheirs})
	if err := idx.WriteFile(r.IndexFile(), index.Version2); err != nil {
		t.Fatal(err)
	}

	for name, content := range map[string]string{
		"clean.txt":     "same\n",
		"modified.txt":  "new content\n",
		"staged.txt":    "new file\n",
		"untracked.txt": "untracked\n",
		"conflict.txt":  "conflict marker\n",
	} {
		if err := writeFile(target, name, content); err != nil {
			t.Fatal(err)
		}
	}
}

func activatedWorkingApp(t *testing.T, target string) *App {
	t.Helper()
	cfg := config.Default()
	cfg.Repositories = []config.Repository{{ID: "r1", Name: "Main", Path: target}}
	a := newTestAppWithConfig(t, cfg)
	a.ActivateRepository("r1")
	return a
}

func TestActivateRepositoryShowsWorkingCopyStatusInFilesGrid(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	buildWorkingRepoFixture(t, target)

	a := activatedWorkingApp(t, target)
	waitForWorkingRows(t, a, 4)

	rows := []changes.Row{
		filesRowOnDispatcher(t, a, 0),
		filesRowOnDispatcher(t, a, 1),
		filesRowOnDispatcher(t, a, 2),
		filesRowOnDispatcher(t, a, 3),
	}
	want := []changes.Row{
		{State: "Conflict", Name: "conflict.txt", RelPath: "conflict.txt"},
		{State: "Added", Name: "staged.txt", RelPath: "staged.txt"},
		{State: "Modified", Name: "modified.txt", RelPath: "modified.txt"},
		{State: "Untracked", Name: "untracked.txt", RelPath: "untracked.txt"},
	}
	for i, w := range want {
		got := rows[i]
		if got.State != w.State || got.Name != w.Name || got.RelPath != w.RelPath {
			t.Fatalf("row %d = %+v, want State/Name/RelPath of %+v", i, got, w)
		}
	}
	if n := filesRowCountOnDispatcher(t, a); n != 4 {
		t.Fatalf("clean.txt must not appear as a row: got %d rows", n)
	}
}

func TestSelectingAModifiedWorkingRowShowsTheIndexVsWorktreeDiff(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	buildWorkingRepoFixture(t, target)
	a := activatedWorkingApp(t, target)
	waitForWorkingRows(t, a, 4)

	selectFilesRow(t, a, 2)
	waitForPostQueueDrain(t, a)
	a.diffWG.Wait()

	doc := diffDocumentOnDispatcher(t, a)
	if doc.OldName != "modified.txt" || doc.NewName != "modified.txt" {
		t.Fatalf("doc = %+v, want it to describe modified.txt", doc)
	}
	if doc.IsEmpty() {
		t.Fatal("expected hunks describing the working tree change")
	}
}

func TestSelectingAStagedWorkingRowShowsTheHeadVsIndexDiff(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	buildWorkingRepoFixture(t, target)
	a := activatedWorkingApp(t, target)
	waitForWorkingRows(t, a, 4)

	selectFilesRow(t, a, 1)
	waitForPostQueueDrain(t, a)
	a.diffWG.Wait()

	doc := diffDocumentOnDispatcher(t, a)
	if doc.OldName != "staged.txt" || doc.NewName != "staged.txt" {
		t.Fatalf("doc = %+v, want it to describe staged.txt", doc)
	}
	if len(doc.Hunks) == 0 || len(doc.Hunks[0].Lines) == 0 {
		t.Fatalf("doc = %+v, want an addition hunk", doc)
	}
	for _, line := range doc.Hunks[0].Lines {
		if line.Kind != diffview.Added {
			t.Fatalf("line kind = %v, want an added line for a new file", line.Kind)
		}
	}
}

func TestSelectingAConflictWorkingRowShowsAnOursVsWorktreeDiff(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	buildWorkingRepoFixture(t, target)
	a := activatedWorkingApp(t, target)
	waitForWorkingRows(t, a, 4)

	selectFilesRow(t, a, 0)
	waitForPostQueueDrain(t, a)
	a.diffWG.Wait()

	doc := diffDocumentOnDispatcher(t, a)
	if doc.OldName != "conflict.txt" || doc.NewName != "conflict.txt" {
		t.Fatalf("doc = %+v, want it to describe conflict.txt", doc)
	}
}

func TestSelectingAnUntrackedWorkingRowShowsAnAddedFileDiff(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	buildWorkingRepoFixture(t, target)
	a := activatedWorkingApp(t, target)
	waitForWorkingRows(t, a, 4)

	selectFilesRow(t, a, 3)
	waitForPostQueueDrain(t, a)
	a.diffWG.Wait()

	doc := diffDocumentOnDispatcher(t, a)
	if doc.OldName != "untracked.txt" || doc.NewName != "untracked.txt" {
		t.Fatalf("doc = %+v, want it to describe untracked.txt", doc)
	}
	if doc.IsEmpty() {
		t.Fatal("expected the untracked file's content as an addition")
	}
}

func TestActivateRepositoryOnABareRepositoryLeavesFilesGridEmpty(t *testing.T) {
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

	if got := filesRowCountOnDispatcher(t, a); got != 0 {
		t.Fatalf("files grid = %d rows, want 0 for a bare repository", got)
	}
	if a.opened() == nil || a.opened().currentWorktree() != nil {
		t.Fatal("a bare repository must not open a worktree")
	}
}

func TestWatcherIndexChangeReloadsTheWorkingCopyStatus(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	initTestRepoWithBranch(t, target, "main")
	cfg := config.Default()
	cfg.Repositories = []config.Repository{{ID: "r1", Name: "Main", Path: target}}
	a := newTestAppWithConfig(t, cfg)
	fw := newFakeWatcher()
	a.newWatcher = func(gitrepo.Layout, watch.Options) watcherIface { return fw }

	a.ActivateRepository("r1")
	<-fw.started

	if err := writeFile(target, "new.txt", "hello\n"); err != nil {
		t.Fatal(err)
	}
	fw.changes <- watch.ChangeSet{watch.Change{Kind: watch.Index}: struct{}{}}

	waitForWorkingRows(t, a, 1)
	row := filesRowOnDispatcher(t, a, 0)
	if row.State != "Untracked" || row.RelPath != "new.txt" {
		t.Fatalf("row = %+v, want the untracked new.txt", row)
	}
}

func TestSelectingAJournalRowSwitchesTheFilesGridToCommitMode(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	initTestRepoWithBranch(t, target, "main")
	db, store := withJournalRepo(t, target)
	tree := putChangesTree(t, db, map[string]string{"a.txt": "hello\n"})
	root := putChangesCommit(t, db, tree)
	setRef(t, store, refs.BranchName("main"), root)
	a := activatedWorkingApp(t, target)
	waitForJournalRows(t, a, 1)
	waitForWorkingRows(t, a, 0)

	selectJournalRow(t, a, 0)
	waitForFilesRows(t, a, 1)

	if filesModeOnDispatcher(a) != filesModeCommit {
		t.Fatal("selecting a commit must switch the files grid to commit mode")
	}
	row := filesRowOnDispatcher(t, a, 0)
	if row.State != "Added" || row.RelPath != "a.txt" {
		t.Fatalf("row = %+v, want a.txt added", row)
	}
}

func TestDiskChangesDoNotTouchThePanelsWhileACommitIsSelected(t *testing.T) {
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
	before := diffDocumentOnDispatcher(t, a)

	a.refreshWorkingStatus()
	waitForPostQueueDrain(t, a)

	if got := filesRowCountOnDispatcher(t, a); got != 1 {
		t.Fatalf("commit mode rows must stay untouched by disk refreshes: got %d rows", got)
	}
	if filesModeOnDispatcher(a) != filesModeCommit {
		t.Fatal("commit mode must be preserved across a disk refresh")
	}
	after := diffDocumentOnDispatcher(t, a)
	if after.OldName != before.OldName {
		t.Fatalf("diff document changed across a disk refresh: %+v -> %+v", before, after)
	}
}

func TestRefreshRepositoryReloadsWorkingCopyWhenNoCommitIsSelected(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	initTestRepoWithBranch(t, target, "main")
	a := activatedWorkingApp(t, target)
	waitForWorkingRows(t, a, 0)
	if err := writeFile(target, "new.txt", "hello\n"); err != nil {
		t.Fatal(err)
	}

	a.Dispatch(CmdRefresh)

	waitForWorkingRows(t, a, 1)
	row := filesRowOnDispatcher(t, a, 0)
	if row.State != "Untracked" || row.RelPath != "new.txt" {
		t.Fatalf("row = %+v, want the untracked new.txt", row)
	}
}

func TestRefreshRepositoryKeepsTheSelectedCommitDiffUnchanged(t *testing.T) {
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

	a.Dispatch(CmdRefresh)
	waitForPostQueueDrain(t, a)

	if got := filesRowCountOnDispatcher(t, a); got != 1 {
		t.Fatalf("files grid must stay populated with the commit diff: got %d rows", got)
	}
	if filesModeOnDispatcher(a) != filesModeCommit {
		t.Fatal("refresh must not switch away from commit mode")
	}
}

func TestCloseRepositoryResetsFilesModeAndCommitSelection(t *testing.T) {
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

	a.CloseRepository()

	if a.commitSelected {
		t.Fatal("closing the repository must reset commitSelected")
	}
	if filesModeOnDispatcher(a) != filesModeWorking {
		t.Fatal("closing the repository must reset the files mode to working")
	}
	if a.selectedCommit != (hash.ObjectID{}) {
		t.Fatal("closing the repository must reset the selected commit")
	}
}

func TestOnFilesRowSelectedIgnoresAnOutOfRangeIndexInWorkingMode(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	buildWorkingRepoFixture(t, target)
	a := activatedWorkingApp(t, target)
	waitForWorkingRows(t, a, 4)
	before := diffDocumentOnDispatcher(t, a)

	a.onFilesRowSelected(datagrid.SelectionChangedEvent{SelectedIndex: 99, SelectedItem: changes.Row{Name: "x"}})

	after := diffDocumentOnDispatcher(t, a)
	if after.OldName != before.OldName {
		t.Fatalf("document changed for an out-of-range working index: %+v", after)
	}
}
