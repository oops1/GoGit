package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/oops1/headless-gui/v3/widget"
	"github.com/oops1/headless-gui/v3/widget/datagrid"

	"github.com/oops1/gogit/internal/config"
	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/refs"
	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/gitcore/revision"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/journal"
)

func withJournalRepo(t *testing.T, path string) (*odb.DB, *refs.Store) {
	t.Helper()
	r, err := gitrepo.Open(path, gitrepo.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	db, err := odb.Open(r.ObjectsDir(), odb.Options{})
	if err != nil {
		t.Fatal(err)
	}
	store, err := refs.Open(refs.Options{GitDir: r.GitDir(), CommonDir: r.CommonDir(), Committer: testCommitter})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = store.Close()
		_ = db.Close()
		_ = r.Close()
	})
	return db, store
}

func putJournalTree(t *testing.T, db *odb.DB) hash.ObjectID {
	t.Helper()
	id, err := db.PutObject(&object.Tree{})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func putJournalCommit(t *testing.T, db *odb.DB, tree hash.ObjectID, when time.Time, message string, parents ...hash.ObjectID) hash.ObjectID {
	t.Helper()
	sig := object.Signature{Name: "ann", Email: "ann@example.com", When: when}
	commit := &object.Commit{Tree: tree, Author: sig, Committer: sig, Message: message, Parents: parents}
	id, err := db.PutObject(commit)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func seedJournalCommits(t *testing.T, path, branch string, n int) []hash.ObjectID {
	t.Helper()
	db, store := withJournalRepo(t, path)
	tree := putJournalTree(t, db)
	ids := make([]hash.ObjectID, 0, n)
	var parent, tip hash.ObjectID
	for i := range n {
		when := time.Unix(1700000000+int64(i)*60, 0).UTC()
		message := fmt.Sprintf("commit %d\n", i)
		if i == 0 {
			tip = putJournalCommit(t, db, tree, when, message)
		} else {
			tip = putJournalCommit(t, db, tree, when, message, parent)
		}
		ids = append(ids, tip)
		parent = tip
	}
	setRef(t, store, refs.BranchName(branch), tip)
	return ids
}

func addJournalCommit(t *testing.T, path, branch string, parent hash.ObjectID, message string) hash.ObjectID {
	t.Helper()
	db, store := withJournalRepo(t, path)
	tree := putJournalTree(t, db)
	id := putJournalCommit(t, db, tree, time.Now(), message, parent)
	setRef(t, store, refs.BranchName(branch), id)
	return id
}

func journalRowCountOnDispatcher(t *testing.T, a *App) int {
	t.Helper()
	result := make(chan int, 1)
	a.Post(func() {
		result <- a.Widget("journalGrid").(*widget.DataGridWidget).Grid.ItemsSource().Count()
	})
	select {
	case n := <-result:
		return n
	case <-time.After(2 * time.Second):
		t.Fatal("post queue did not drain in time")
		return 0
	}
}

func journalRowOnDispatcher(t *testing.T, a *App, index int) journal.Row {
	t.Helper()
	result := make(chan journal.Row, 1)
	a.Post(func() {
		row, _ := a.Widget("journalGrid").(*widget.DataGridWidget).Grid.ItemsSource().Get(index).(journal.Row)
		result <- row
	})
	select {
	case row := <-result:
		return row
	case <-time.After(2 * time.Second):
		t.Fatal("post queue did not drain in time")
		return journal.Row{}
	}
}

func waitForJournalRows(t *testing.T, a *App, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if journalRowCountOnDispatcher(t, a) >= want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("journal did not reach %d rows in time", want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestActivateRepositoryLoadsJournalRowsNewestFirstWithBranchDecoration(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	initTestRepoWithBranch(t, target, "main")
	ids := seedJournalCommits(t, target, "main", 3)
	cfg := config.Default()
	cfg.Repositories = []config.Repository{{ID: "r1", Name: "Main", Path: target}}
	a := newTestAppWithConfig(t, cfg)

	a.ActivateRepository("r1")
	waitForJournalRows(t, a, 3)

	if got := journalRowCountOnDispatcher(t, a); got != 3 {
		t.Fatalf("journal row count = %d, want 3", got)
	}
	first := journalRowOnDispatcher(t, a, 0)
	if first.ID != ids[2] {
		t.Fatalf("first row = %s, want tip %s", first.ID, ids[2])
	}
	if !slices.Contains(first.Refs, "main") {
		t.Fatalf("first row refs = %v, want it to contain main", first.Refs)
	}
	last := journalRowOnDispatcher(t, a, 2)
	if last.ID != ids[0] {
		t.Fatalf("last row = %s, want root %s", last.ID, ids[0])
	}
}

func TestActivateRepositoryOnUnbornHeadLeavesJournalEmpty(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	initTestRepo(t, target)
	cfg := config.Default()
	cfg.Repositories = []config.Repository{{ID: "r1", Name: "Main", Path: target}}
	a := newTestAppWithConfig(t, cfg)

	a.ActivateRepository("r1")
	waitForPostQueueDrain(t, a)

	if got := journalRowCountOnDispatcher(t, a); got != 0 {
		t.Fatalf("journal row count = %d, want 0 for an unborn HEAD", got)
	}
}

func TestOnNearEndFetchesTheNextJournalPage(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	initTestRepoWithBranch(t, target, "main")
	seedJournalCommits(t, target, "main", 5)
	cfg := config.Default()
	cfg.Repositories = []config.Repository{{ID: "r1", Name: "Main", Path: target}}
	a := newTestAppWithConfig(t, cfg)
	a.journalPageSize = 2

	a.ActivateRepository("r1")
	waitForJournalRows(t, a, 2)
	if got := journalRowCountOnDispatcher(t, a); got != 2 {
		t.Fatalf("first page = %d rows, want 2", got)
	}

	a.journalView.OnNearEnd()
	waitForJournalRows(t, a, 4)
	if got := journalRowCountOnDispatcher(t, a); got != 4 {
		t.Fatalf("after OnNearEnd = %d rows, want 4", got)
	}

	a.journalView.OnNearEnd()
	waitForJournalRows(t, a, 5)
	if got := journalRowCountOnDispatcher(t, a); got != 5 {
		t.Fatalf("after second OnNearEnd = %d rows, want 5", got)
	}
}

func TestCmdRefreshReloadsJournalShowingTheNewCommitFirst(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	initTestRepoWithBranch(t, target, "main")
	ids := seedJournalCommits(t, target, "main", 2)
	cfg := config.Default()
	cfg.Repositories = []config.Repository{{ID: "r1", Name: "Main", Path: target}}
	a := newTestAppWithConfig(t, cfg)
	a.ActivateRepository("r1")
	waitForJournalRows(t, a, 2)

	newTip := addJournalCommit(t, target, "main", ids[1], "commit 2\n")

	a.Dispatch(CmdRefresh)
	waitForJournalRows(t, a, 3)

	first := journalRowOnDispatcher(t, a, 0)
	if first.ID != newTip {
		t.Fatalf("first row after refresh = %s, want the new commit %s", first.ID, newTip)
	}
}

func TestCloseRepositoryClearsJournalAndStopsTheLoader(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	initTestRepoWithBranch(t, target, "main")
	seedJournalCommits(t, target, "main", 3)
	cfg := config.Default()
	cfg.Repositories = []config.Repository{{ID: "r1", Name: "Main", Path: target}}
	a := newTestAppWithConfig(t, cfg)
	a.ActivateRepository("r1")
	waitForJournalRows(t, a, 3)

	a.CloseRepository()

	if got := journalRowCountOnDispatcher(t, a); got != 0 {
		t.Fatalf("journal not cleared after close: %d rows", got)
	}
	a.journalMu.Lock()
	more := a.journalMore
	a.journalMu.Unlock()
	if more != nil {
		t.Fatal("journal loader must be stopped after close")
	}
}

func TestActivateRepositoryTwiceReloadsJournalForTheNewRepository(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first")
	second := filepath.Join(dir, "second")
	initTestRepoWithBranch(t, first, "main")
	initTestRepoWithBranch(t, second, "main")
	seedJournalCommits(t, first, "main", 2)
	ids := seedJournalCommits(t, second, "main", 4)
	cfg := config.Default()
	cfg.Repositories = []config.Repository{
		{ID: "r1", Name: "First", Path: first},
		{ID: "r2", Name: "Second", Path: second},
	}
	a := newTestAppWithConfig(t, cfg)

	a.ActivateRepository("r1")
	waitForJournalRows(t, a, 2)

	a.ActivateRepository("r2")
	waitForJournalRows(t, a, 4)

	if got := journalRowCountOnDispatcher(t, a); got != 4 {
		t.Fatalf("journal row count after switching repositories = %d, want 4", got)
	}
	first0 := journalRowOnDispatcher(t, a, 0)
	if first0.ID != ids[3] {
		t.Fatalf("first row after switching repositories = %s, want %s", first0.ID, ids[3])
	}
}

func TestStartJournalIsANoOpWithoutAnOpenRepository(t *testing.T) {
	a := newTestApp(t)

	a.startJournal()

	if got := journalRowCountOnDispatcher(t, a); got != 0 {
		t.Fatalf("journal row count = %d, want 0", got)
	}
}

func TestStopJournalIsANoOpWithoutAnActiveJournal(t *testing.T) {
	a := newTestApp(t)
	a.stopJournal()
}

func TestRequestMoreJournalIsANoOpWithoutAnActiveJournal(t *testing.T) {
	a := newTestApp(t)
	a.requestMoreJournal()
}

func TestJournalRowSelectionThroughTheGridUpdatesStatusTextAndSelectedCommit(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	initTestRepoWithBranch(t, target, "main")
	ids := seedJournalCommits(t, target, "main", 1)
	cfg := config.Default()
	cfg.Repositories = []config.Repository{{ID: "r1", Name: "Main", Path: target}}
	a := newTestAppWithConfig(t, cfg)
	a.ActivateRepository("r1")
	waitForJournalRows(t, a, 1)
	row := journalRowOnDispatcher(t, a, 0)

	grid := a.Widget("journalGrid").(*widget.DataGridWidget)
	grid.Grid.OnSelectionChanged(datagrid.SelectionChangedEvent{SelectedIndex: 0, SelectedItem: row})

	want := i18n.Tf("Status.CommitSelected", row.ShortHash, row.Message)
	if got := a.Widget("statusText").(*widget.Label).Text(); got != want {
		t.Fatalf("status text = %q, want %q", got, want)
	}
	if a.selectedCommit != ids[0] {
		t.Fatalf("selectedCommit = %s, want %s", a.selectedCommit, ids[0])
	}
}

func withFailingObjectsDBOpen(t *testing.T) {
	t.Helper()
	prev := openObjectsDB
	openObjectsDB = func(string, odb.Options) (*odb.DB, error) { return nil, errors.New("boom") }
	t.Cleanup(func() { openObjectsDB = prev })
}

func TestActivateRepositoryReportsErrorWhenObjectsDBFailsToOpen(t *testing.T) {
	widget.ClearStrings()
	t.Cleanup(widget.ClearStrings)
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	initTestRepo(t, target)
	cfg := config.Default()
	cfg.Repositories = []config.Repository{{ID: "r1", Name: "Main", Path: target}}
	a := newTestAppWithConfig(t, cfg)
	withFailingObjectsDBOpen(t)

	a.ActivateRepository("r1")

	if a.State().ActiveRepository != "" {
		t.Fatal("state must stay reset when the objects db fails to open")
	}
	if a.open != nil {
		t.Fatal("a failed open must not leave a repository open")
	}
	if got := journalRowCountOnDispatcher(t, a); got != 0 {
		t.Fatalf("journal must stay empty when the objects db fails to open, got %d rows", got)
	}
}

type fakeJournalPager struct {
	result    func() ([]journal.Row, bool, error)
	cancelled atomic.Bool
}

func (f *fakeJournalPager) Next(int) ([]journal.Row, bool, error) { return f.result() }

func (f *fakeJournalPager) Cancel() { f.cancelled.Store(true) }

func withFakeJournalPager(t *testing.T, fake *fakeJournalPager) {
	t.Helper()
	prev := newJournalPager
	newJournalPager = func(context.Context, revision.Context, revision.Options) journalPager { return fake }
	t.Cleanup(func() { newJournalPager = prev })
}

func TestRunJournalLogsAWarningForARealPagerError(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	initTestRepo(t, target)
	cfg := config.Default()
	cfg.Repositories = []config.Repository{{ID: "r1", Name: "Main", Path: target}}
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	a, err := New(cfg, config.Paths{Dir: t.TempDir()}, logger)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)
	fake := &fakeJournalPager{result: func() ([]journal.Row, bool, error) {
		return nil, true, errors.New("boom")
	}}
	withFakeJournalPager(t, fake)

	a.ActivateRepository("r1")
	a.journalWG.Wait()

	if !strings.Contains(buf.String(), "load journal failed") {
		t.Fatalf("expected the pager error to be logged: %s", buf.String())
	}
	if !fake.cancelled.Load() {
		t.Fatal("the pager must be released when the loader exits")
	}
}

func TestRunJournalDoesNotLogWhenThePagerReportsContextCanceled(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	initTestRepo(t, target)
	cfg := config.Default()
	cfg.Repositories = []config.Repository{{ID: "r1", Name: "Main", Path: target}}
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	a, err := New(cfg, config.Paths{Dir: t.TempDir()}, logger)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)
	fake := &fakeJournalPager{result: func() ([]journal.Row, bool, error) {
		return nil, true, context.Canceled
	}}
	withFakeJournalPager(t, fake)

	a.ActivateRepository("r1")
	a.journalWG.Wait()

	if strings.Contains(buf.String(), "load journal failed") {
		t.Fatalf("context.Canceled must not be logged as a warning: %s", buf.String())
	}
	if !fake.cancelled.Load() {
		t.Fatal("the pager must be released when the loader exits")
	}
}
