package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/oops1/headless-gui/v3/widget"
	"github.com/oops1/headless-gui/v3/widget/datagrid"

	"github.com/oops1/gogit/internal/config"
	"github.com/oops1/gogit/internal/gitcore/diff"
	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/ui/changes"
	"github.com/oops1/gogit/internal/ui/diffview"
	"github.com/oops1/gogit/internal/ui/filesgrid"
)

func putChangesBlob(t *testing.T, db *odb.DB, content string) hash.ObjectID {
	t.Helper()
	id, err := db.PutObject(&object.Blob{Data: []byte(content)})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func putChangesTree(t *testing.T, db *odb.DB, files map[string]string) hash.ObjectID {
	t.Helper()
	tree := &object.Tree{}
	for name, content := range files {
		tree.Entries = append(tree.Entries, object.TreeEntry{
			Mode: object.ModeBlob, Name: name, ID: putChangesBlob(t, db, content),
		})
	}
	tree.Sort()
	id, err := db.PutObject(tree)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func putChangesCommit(t *testing.T, db *odb.DB, tree hash.ObjectID, parents ...hash.ObjectID) hash.ObjectID {
	t.Helper()
	sig := testCommitter()
	commit := &object.Commit{Tree: tree, Author: sig, Committer: sig, Message: "msg\n", Parents: parents}
	id, err := db.PutObject(commit)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func filesRowCountOnDispatcher(t *testing.T, a *App) int {
	t.Helper()
	result := make(chan int, 1)
	a.Post(func() { result <- a.filesItems.Count() })
	select {
	case n := <-result:
		return n
	case <-time.After(testTimeout):
		t.Fatal("post queue did not drain in time")
		return 0
	}
}

func filesRowOnDispatcher(t *testing.T, a *App, index int) changes.Row {
	t.Helper()
	result := make(chan changes.Row, 1)
	a.Post(func() {
		row, _ := a.filesItems.Get(index).(changes.Row)
		result <- row
	})
	select {
	case row := <-result:
		return row
	case <-time.After(testTimeout):
		t.Fatal("post queue did not drain in time")
		return changes.Row{}
	}
}

func filesModeOnDispatcher(a *App) filesMode {
	a.filesMu.Lock()
	defer a.filesMu.Unlock()
	return a.filesMode
}

func waitForFilesMode(t *testing.T, a *App, mode filesMode, want int) {
	t.Helper()
	deadline := time.Now().Add(testTimeout)
	for {
		if filesModeOnDispatcher(a) == mode && filesRowCountOnDispatcher(t, a) >= want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("files grid did not reach mode %v with %d rows in time", mode, want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func waitForFilesRows(t *testing.T, a *App, want int) {
	t.Helper()
	waitForFilesMode(t, a, filesModeCommit, want)
}

func waitForWorkingRows(t *testing.T, a *App, want int) {
	t.Helper()
	waitForFilesMode(t, a, filesModeWorking, want)
}

func diffDocumentOnDispatcher(t *testing.T, a *App) diffview.Document {
	t.Helper()
	result := make(chan diffview.Document, 1)
	a.Post(func() { result <- a.diffView.Document() })
	select {
	case doc := <-result:
		return doc
	case <-time.After(testTimeout):
		t.Fatal("post queue did not drain in time")
		return diffview.Document{}
	}
}

func selectJournalRow(t *testing.T, a *App, index int) {
	t.Helper()
	row := journalRowOnDispatcher(t, a, index)
	grid := a.Widget("journalGrid").(*widget.DataGridWidget)
	grid.Grid.OnSelectionChanged(datagrid.SelectionChangedEvent{SelectedIndex: index, SelectedItem: row})
}

func selectFilesRow(t *testing.T, a *App, index int) {
	t.Helper()
	row := filesRowOnDispatcher(t, a, index)
	grid := a.Widget("filesGrid").(*filesgrid.Grid)
	grid.Data().Grid.OnSelectionChanged(datagrid.SelectionChangedEvent{SelectedIndex: index, SelectedItem: row})
}

func newChangesTestApp(t *testing.T, target string) (*App, *bytes.Buffer) {
	t.Helper()
	widget.ClearStrings()
	t.Cleanup(widget.ClearStrings)
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	cfg := config.Default()
	cfg.Repositories = []config.Repository{{ID: "r1", Name: "Main", Path: target}}
	a, err := New(cfg, config.Paths{Dir: t.TempDir()}, logger)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)
	return a, &buf
}

func TestSelectingAJournalRowWithTwoChangedFilesPopulatesFilesGridAndShowsTheFirstFileDiff(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	initTestRepoWithBranch(t, target, "main")
	db, store := withJournalRepo(t, target)
	base := putChangesTree(t, db, map[string]string{"a.txt": "line one\n", "b.txt": "line two\n"})
	baseCommit := putChangesCommit(t, db, base)
	changed := putChangesTree(t, db, map[string]string{"a.txt": "line ONE\n", "b.txt": "line TWO\n"})
	tipCommit := putChangesCommit(t, db, changed, baseCommit)
	setRef(t, store, refs.BranchName("main"), tipCommit)

	cfg := config.Default()
	cfg.Repositories = []config.Repository{{ID: "r1", Name: "Main", Path: target}}
	a := newTestAppWithConfig(t, cfg)
	a.ActivateRepository("r1")
	waitForJournalRows(t, a, 2)

	selectJournalRow(t, a, 0)
	waitForFilesRows(t, a, 2)

	first := filesRowOnDispatcher(t, a, 0)
	second := filesRowOnDispatcher(t, a, 1)
	if first.RelPath != "a.txt" || second.RelPath != "b.txt" {
		t.Fatalf("rows = %+v / %+v, want a.txt then b.txt", first, second)
	}
	if first.State != "Modified" || second.State != "Modified" {
		t.Fatalf("rows = %+v / %+v, want both modified", first, second)
	}

	doc := diffDocumentOnDispatcher(t, a)
	if doc.OldName != "a.txt" {
		t.Fatalf("diff document = %+v, want it to describe a.txt", doc)
	}
}

func TestSelectingASecondRowInFilesGridSwitchesTheDisplayedDiff(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	initTestRepoWithBranch(t, target, "main")
	db, store := withJournalRepo(t, target)
	base := putChangesTree(t, db, map[string]string{"a.txt": "line one\n", "b.txt": "line two\n"})
	baseCommit := putChangesCommit(t, db, base)
	changed := putChangesTree(t, db, map[string]string{"a.txt": "line ONE\n", "b.txt": "line TWO\n"})
	tipCommit := putChangesCommit(t, db, changed, baseCommit)
	setRef(t, store, refs.BranchName("main"), tipCommit)

	cfg := config.Default()
	cfg.Repositories = []config.Repository{{ID: "r1", Name: "Main", Path: target}}
	a := newTestAppWithConfig(t, cfg)
	a.ActivateRepository("r1")
	waitForJournalRows(t, a, 2)
	selectJournalRow(t, a, 0)
	waitForFilesRows(t, a, 2)

	selectFilesRow(t, a, 1)

	doc := diffDocumentOnDispatcher(t, a)
	if doc.OldName != "b.txt" {
		t.Fatalf("diff document after selecting the second row = %+v, want it to describe b.txt", doc)
	}
}

func TestSelectingTheRootCommitShowsAllFilesAsAdded(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	initTestRepoWithBranch(t, target, "main")
	db, store := withJournalRepo(t, target)
	tree := putChangesTree(t, db, map[string]string{"a.txt": "hello\n"})
	root := putChangesCommit(t, db, tree)
	setRef(t, store, refs.BranchName("main"), root)

	cfg := config.Default()
	cfg.Repositories = []config.Repository{{ID: "r1", Name: "Main", Path: target}}
	a := newTestAppWithConfig(t, cfg)
	a.ActivateRepository("r1")
	waitForJournalRows(t, a, 1)

	selectJournalRow(t, a, 0)
	waitForFilesRows(t, a, 1)

	row := filesRowOnDispatcher(t, a, 0)
	if row.State != "Added" || row.RelPath != "a.txt" {
		t.Fatalf("row = %+v, want a.txt added", row)
	}
}

func TestCloseRepositoryClearsFilesGridAndDiffView(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	initTestRepoWithBranch(t, target, "main")
	db, store := withJournalRepo(t, target)
	tree := putChangesTree(t, db, map[string]string{"a.txt": "hello\n"})
	root := putChangesCommit(t, db, tree)
	setRef(t, store, refs.BranchName("main"), root)

	cfg := config.Default()
	cfg.Repositories = []config.Repository{{ID: "r1", Name: "Main", Path: target}}
	a := newTestAppWithConfig(t, cfg)
	a.ActivateRepository("r1")
	waitForJournalRows(t, a, 1)
	selectJournalRow(t, a, 0)
	waitForFilesRows(t, a, 1)

	a.CloseRepository()

	if got := filesRowCountOnDispatcher(t, a); got != 0 {
		t.Fatalf("files grid not cleared: %d rows", got)
	}
	if doc := diffDocumentOnDispatcher(t, a); !doc.IsEmpty() {
		t.Fatalf("diff view not cleared: %+v", doc)
	}
}

func TestActivateRepositoryOnAnotherRepositoryClearsChangesPanels(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first")
	second := filepath.Join(dir, "second")
	initTestRepoWithBranch(t, first, "main")
	initTestRepoWithBranch(t, second, "main")
	db1, store1 := withJournalRepo(t, first)
	tree1 := putChangesTree(t, db1, map[string]string{"a.txt": "hello\n"})
	root1 := putChangesCommit(t, db1, tree1)
	setRef(t, store1, refs.BranchName("main"), root1)
	seedJournalCommits(t, second, "main", 1)

	cfg := config.Default()
	cfg.Repositories = []config.Repository{
		{ID: "r1", Name: "First", Path: first},
		{ID: "r2", Name: "Second", Path: second},
	}
	a := newTestAppWithConfig(t, cfg)
	a.ActivateRepository("r1")
	waitForJournalRows(t, a, 1)
	selectJournalRow(t, a, 0)
	waitForFilesRows(t, a, 1)

	a.ActivateRepository("r2")

	if got := filesRowCountOnDispatcher(t, a); got != 0 {
		t.Fatalf("files grid not cleared on repository switch: %d rows", got)
	}
	if doc := diffDocumentOnDispatcher(t, a); !doc.IsEmpty() {
		t.Fatalf("diff view not cleared on repository switch: %+v", doc)
	}
}

func TestStartDiffIsANoOpWithoutAnOpenRepository(t *testing.T) {
	a := newTestApp(t)
	a.startDiff(hash.ObjectID{})
	waitForPostQueueDrain(t, a)
	if got := filesRowCountOnDispatcher(t, a); got != 0 {
		t.Fatalf("files grid = %d rows, want 0", got)
	}
}

func TestOnFilesRowSelectedIgnoresANonRowSelection(t *testing.T) {
	a := newTestApp(t)
	before := a.diffView.Document()

	a.onFilesRowSelected(datagrid.SelectionChangedEvent{SelectedIndex: 0, SelectedItem: "not a row"})

	got := a.diffView.Document()
	if got.OldName != before.OldName || len(got.Hunks) != len(before.Hunks) {
		t.Fatalf("document changed for a non-Row selection: %+v", got)
	}
}

func TestOnFilesRowSelectedIgnoresAnOutOfRangeIndex(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	initTestRepoWithBranch(t, target, "main")
	db, store := withJournalRepo(t, target)
	tree := putChangesTree(t, db, map[string]string{"a.txt": "hello\n"})
	root := putChangesCommit(t, db, tree)
	setRef(t, store, refs.BranchName("main"), root)
	cfg := config.Default()
	cfg.Repositories = []config.Repository{{ID: "r1", Name: "Main", Path: target}}
	a := newTestAppWithConfig(t, cfg)
	a.ActivateRepository("r1")
	waitForJournalRows(t, a, 1)
	selectJournalRow(t, a, 0)
	waitForFilesRows(t, a, 1)
	before := diffDocumentOnDispatcher(t, a)

	a.onFilesRowSelected(datagrid.SelectionChangedEvent{SelectedIndex: 5, SelectedItem: changes.Row{Name: "x"}})

	after := diffDocumentOnDispatcher(t, a)
	if after.OldName != before.OldName {
		t.Fatalf("document changed for an out-of-range index: %+v", after)
	}
}

func TestLoadCommitObjectFailsWhenTheObjectIsNotACommit(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	initTestRepo(t, target)
	db, _ := withJournalRepo(t, target)
	blobID := putChangesBlob(t, db, "not a commit")

	_, err := loadCommitObject(db, blobID)

	if !errors.Is(err, ErrNotACommit) {
		t.Fatalf("err = %v, want ErrNotACommit", err)
	}
}

func withFailingDiffTrees(t *testing.T, fn func(context.Context, diff.Objects, hash.ObjectID, hash.ObjectID, diff.Options) ([]diff.File, error)) {
	t.Helper()
	prev := diffTreesFunc
	diffTreesFunc = fn
	t.Cleanup(func() { diffTreesFunc = prev })
}

func TestRunDiffLogsAWarningForARealDiffError(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	initTestRepoWithBranch(t, target, "main")
	ids := seedJournalCommits(t, target, "main", 1)
	withFailingDiffTrees(t, func(context.Context, diff.Objects, hash.ObjectID, hash.ObjectID, diff.Options) ([]diff.File, error) {
		return nil, errors.New("boom")
	})
	a, buf := newChangesTestApp(t, target)
	a.ActivateRepository("r1")
	waitForJournalRows(t, a, 1)

	a.startDiff(ids[0])
	a.diffWG.Wait()

	if !strings.Contains(buf.String(), "load diff failed") {
		t.Fatalf("expected the diff error to be logged: %s", buf.String())
	}
}

func TestRunDiffLogsAWarningWhenTheCommitObjectIsMissing(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	initTestRepoWithBranch(t, target, "main")
	seedJournalCommits(t, target, "main", 1)
	a, buf := newChangesTestApp(t, target)
	a.ActivateRepository("r1")
	waitForJournalRows(t, a, 1)

	a.startDiff(oid(t, "ff"))
	a.diffWG.Wait()

	if !strings.Contains(buf.String(), "load diff failed") {
		t.Fatalf("expected a missing commit object to be logged: %s", buf.String())
	}
}

func TestRunDiffLogsAWarningWhenTheParentCommitObjectIsMissing(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	initTestRepoWithBranch(t, target, "main")
	db, store := withJournalRepo(t, target)
	tree := putChangesTree(t, db, map[string]string{"a.txt": "hello\n"})
	root := putChangesCommit(t, db, tree)
	setRef(t, store, refs.BranchName("main"), root)
	orphan := putChangesCommit(t, db, tree, oid(t, "ee"))
	a, buf := newChangesTestApp(t, target)
	a.ActivateRepository("r1")

	a.startDiff(orphan)
	a.diffWG.Wait()

	if !strings.Contains(buf.String(), "load diff failed") {
		t.Fatalf("expected a missing parent commit to be logged: %s", buf.String())
	}
}

func TestRunDiffTruncatesFilesToMaxFilesBeforeStoringThem(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	initTestRepoWithBranch(t, target, "main")
	ids := seedJournalCommits(t, target, "main", 1)
	many := make([]diff.File, changes.MaxFiles+5)
	for i := range many {
		many[i] = diff.File{NewPath: fmt.Sprintf("file%d.txt", i), Status: diff.StatusAdded}
	}
	withFailingDiffTrees(t, func(context.Context, diff.Objects, hash.ObjectID, hash.ObjectID, diff.Options) ([]diff.File, error) {
		return many, nil
	})
	a, _ := newChangesTestApp(t, target)
	a.ActivateRepository("r1")
	waitForJournalRows(t, a, 1)

	a.startDiff(ids[0])
	waitForFilesRows(t, a, changes.MaxFiles+1)

	if got := filesRowCountOnDispatcher(t, a); got != changes.MaxFiles+1 {
		t.Fatalf("rows = %d, want %d (MaxFiles rows plus the truncation marker)", got, changes.MaxFiles+1)
	}
}

func TestSelectingAnotherJournalRowCancelsTheSlowerPreviousDiffComputationWithoutLoggingIt(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	initTestRepoWithBranch(t, target, "main")
	ids := seedJournalCommits(t, target, "main", 2)

	firstStarted := make(chan struct{})
	firstCancelled := make(chan struct{})
	calls := 0
	withFailingDiffTrees(t, func(ctx context.Context, source diff.Objects, oldTree, newTree hash.ObjectID, opts diff.Options) ([]diff.File, error) {
		calls++
		if calls == 1 {
			close(firstStarted)
			<-ctx.Done()
			close(firstCancelled)
			return nil, ctx.Err()
		}
		return []diff.File{{NewPath: "second.txt", Status: diff.StatusAdded}}, nil
	})
	a, buf := newChangesTestApp(t, target)
	a.ActivateRepository("r1")
	waitForJournalRows(t, a, 2)

	a.startDiff(ids[1])
	<-firstStarted
	a.startDiff(ids[0])

	select {
	case <-firstCancelled:
	case <-time.After(testTimeout):
		t.Fatal("the previous computation was never cancelled")
	}
	waitForFilesRows(t, a, 1)

	row := filesRowOnDispatcher(t, a, 0)
	if row.RelPath != "second.txt" {
		t.Fatalf("row = %+v, want the second selection's result", row)
	}
	if strings.Contains(buf.String(), "load diff failed") {
		t.Fatalf("a cancelled computation must not be logged as a failure: %s", buf.String())
	}
}

func TestRestoreFilesColumnsAppliesThePersistedOrderAndVisibility(t *testing.T) {
	widget.ClearStrings()
	t.Cleanup(widget.ClearStrings)
	cfg := config.Default()
	cfg.UI.FilesColumns = []string{"size", "name"}
	cfg.UI.FilesVisibleColumns = []string{"size"}
	a, err := New(cfg, config.Paths{Dir: t.TempDir()}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)

	order, visible := a.filesGrid.Columns()
	if order[0] != filesgrid.ColSize || order[1] != filesgrid.ColName {
		t.Fatalf("order[:2] = %v, want [size name]", order[:2])
	}
	if len(visible) != 1 || visible[0] != filesgrid.ColSize {
		t.Fatalf("visible = %v, want [size]", visible)
	}
}

func TestSaveFilesColumnsPersistsTheOrderAndVisibilityToConfig(t *testing.T) {
	widget.ClearStrings()
	t.Cleanup(widget.ClearStrings)
	paths := config.Paths{Dir: t.TempDir()}
	a, err := New(config.Default(), paths, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)

	a.saveFilesColumns([]filesgrid.ColumnID{filesgrid.ColState, filesgrid.ColName}, []filesgrid.ColumnID{filesgrid.ColState})

	if len(a.cfg.UI.FilesColumns) != 2 || a.cfg.UI.FilesColumns[0] != "state" || a.cfg.UI.FilesColumns[1] != "name" {
		t.Fatalf("FilesColumns = %v", a.cfg.UI.FilesColumns)
	}
	if len(a.cfg.UI.FilesVisibleColumns) != 1 || a.cfg.UI.FilesVisibleColumns[0] != "state" {
		t.Fatalf("FilesVisibleColumns = %v", a.cfg.UI.FilesVisibleColumns)
	}
	if _, err := os.Stat(paths.ConfigFile()); err != nil {
		t.Fatalf("config file was not saved: %v", err)
	}
}

func TestSaveFilesColumnsLogsWarningWhenConfigSaveFails(t *testing.T) {
	widget.ClearStrings()
	t.Cleanup(widget.ClearStrings)
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "config.toml"), 0o700); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	a, err := New(config.Default(), config.Paths{Dir: dir}, logger)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)

	a.saveFilesColumns([]filesgrid.ColumnID{filesgrid.ColName}, []filesgrid.ColumnID{filesgrid.ColName})

	if !strings.Contains(buf.String(), "save config failed") {
		t.Fatalf("expected save failure to be logged: %s", buf.String())
	}
}
