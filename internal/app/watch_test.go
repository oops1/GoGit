package app

import (
	"context"
	"iter"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/config"
	"github.com/oops1/gogit/internal/gitcore/refs"
	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/repo/watch"
	"github.com/oops1/gogit/internal/ui/branches"
)

type fakeWatcher struct {
	changes chan watch.ChangeSet
	started chan struct{}
	stopped atomic.Bool
	pokes   atomic.Int32
	pauses  atomic.Int32
	resumes atomic.Int32
}

func newFakeWatcher() *fakeWatcher {
	return &fakeWatcher{changes: make(chan watch.ChangeSet, 4), started: make(chan struct{})}
}

func (w *fakeWatcher) Run(ctx context.Context) iter.Seq[watch.ChangeSet] {
	return func(yield func(watch.ChangeSet) bool) {
		close(w.started)
		defer w.stopped.Store(true)
		for {
			select {
			case <-ctx.Done():
				return
			case cs := <-w.changes:
				if !yield(cs) {
					return
				}
			}
		}
	}
}

func (w *fakeWatcher) Poke()   { w.pokes.Add(1) }
func (w *fakeWatcher) Pause()  { w.pauses.Add(1) }
func (w *fakeWatcher) Resume() { w.resumes.Add(1) }

func waitForPostQueueDrain(t *testing.T, a *App) {
	t.Helper()
	marker := make(chan struct{})
	a.Post(func() { close(marker) })
	select {
	case <-marker:
	case <-time.After(testTimeout):
		t.Fatal("post queue did not drain in time")
	}
}

func branchItemExistsOnDispatcher(t *testing.T, a *App, name refs.Name) bool {
	t.Helper()
	result := make(chan bool, 1)
	a.Post(func() {
		_, ok := a.branchesView.Item(name)
		result <- ok
	})
	select {
	case ok := <-result:
		return ok
	case <-time.After(testTimeout):
		t.Fatal("post queue did not drain in time")
		return false
	}
}

func TestNewRealWatcherWrapsWatchNew(t *testing.T) {
	if newRealWatcher(gitrepo.Layout{}, watch.Options{}) == nil {
		t.Fatal("newRealWatcher must return a non-nil watcher")
	}
}

func TestSyntheticRefsChangeSetTriggersRefreshRepositoryThroughPostQueue(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	initTestRepo(t, target)
	cfg := config.Default()
	cfg.Repositories = []config.Repository{{ID: "r1", Name: "Main", Path: target}}
	a := newTestAppWithConfig(t, cfg)

	fw := newFakeWatcher()
	a.newWatcher = func(gitrepo.Layout, watch.Options) watcherIface { return fw }

	called := make(chan struct{}, 1)
	prevLoad := loadBranchSnapshot
	loadBranchSnapshot = func(s *refs.Store) (branches.Snapshot, error) {
		snap, err := prevLoad(s)
		select {
		case called <- struct{}{}:
		default:
		}
		return snap, err
	}
	t.Cleanup(func() { loadBranchSnapshot = prevLoad })

	a.ActivateRepository("r1")
	select {
	case <-called:
	default:
		t.Fatal("expected loadBranchSnapshot to run while opening the repository")
	}
	<-fw.started

	store, err := refs.Open(refs.Options{GitDir: filepath.Join(target, ".git"), Committer: testCommitter})
	if err != nil {
		t.Fatal(err)
	}
	setRef(t, store, refs.BranchName("feature"), oid(t, "55"))
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	fw.changes <- watch.ChangeSet{watch.Change{Kind: watch.Refs}: struct{}{}}

	select {
	case <-called:
	case <-time.After(testTimeout):
		t.Fatal("loadBranchSnapshot was not invoked for the synthetic Refs change")
	}

	if !branchItemExistsOnDispatcher(t, a, refs.BranchName("feature")) {
		t.Fatal("branch not reflected in the tree after a synthetic Refs change")
	}
}

func TestCloseRepositoryStopsTheWatcher(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	initTestRepo(t, target)
	cfg := config.Default()
	cfg.Repositories = []config.Repository{{ID: "r1", Name: "Main", Path: target}}
	a := newTestAppWithConfig(t, cfg)
	fw := newFakeWatcher()
	a.newWatcher = func(gitrepo.Layout, watch.Options) watcherIface { return fw }
	a.ActivateRepository("r1")
	<-fw.started

	a.CloseRepository()

	if !fw.stopped.Load() {
		t.Fatal("CloseRepository must stop the watcher and wait for it to exit")
	}
}

func TestActivateRepositoryOnAnotherRepositoryRestartsTheWatcher(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first")
	second := filepath.Join(dir, "second")
	initTestRepo(t, first)
	initTestRepo(t, second)
	cfg := config.Default()
	cfg.Repositories = []config.Repository{
		{ID: "r1", Name: "First", Path: first},
		{ID: "r2", Name: "Second", Path: second},
	}
	a := newTestAppWithConfig(t, cfg)

	var watchers []*fakeWatcher
	a.newWatcher = func(gitrepo.Layout, watch.Options) watcherIface {
		fw := newFakeWatcher()
		watchers = append(watchers, fw)
		return fw
	}

	a.ActivateRepository("r1")
	<-watchers[0].started

	a.ActivateRepository("r2")
	<-watchers[1].started

	if !watchers[0].stopped.Load() {
		t.Fatal("activating another repository must stop the previous watcher")
	}
	if watchers[1].stopped.Load() {
		t.Fatal("the newly started watcher must still be running")
	}
}

func TestActivateRepositoryOnOpenFailureStopsThePreviousWatcher(t *testing.T) {
	widget.ClearStrings()
	t.Cleanup(widget.ClearStrings)
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	initTestRepo(t, target)
	cfg := config.Default()
	cfg.Repositories = []config.Repository{
		{ID: "r1", Name: "Main", Path: target},
		{ID: "r2", Name: "Broken", Path: filepath.Join(dir, "missing")},
	}
	a := newTestAppWithConfig(t, cfg)
	var watchers []*fakeWatcher
	a.newWatcher = func(gitrepo.Layout, watch.Options) watcherIface {
		fw := newFakeWatcher()
		watchers = append(watchers, fw)
		return fw
	}
	a.ActivateRepository("r1")
	<-watchers[0].started

	a.ActivateRepository("r2")

	if !watchers[0].stopped.Load() {
		t.Fatal("a failed activation must still stop the previously running watcher")
	}
	if len(watchers) != 1 {
		t.Fatalf("a failed open must not start a new watcher, got %d watchers", len(watchers))
	}
}

func TestAppCloseStopsTheWatcherAndThePostQueue(t *testing.T) {
	widget.ClearStrings()
	t.Cleanup(widget.ClearStrings)
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	initTestRepo(t, target)
	cfg := config.Default()
	cfg.Repositories = []config.Repository{{ID: "r1", Name: "Main", Path: target}}
	a, err := New(cfg, config.Paths{Dir: t.TempDir()}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)
	fw := newFakeWatcher()
	a.newWatcher = func(gitrepo.Layout, watch.Options) watcherIface { return fw }
	a.ActivateRepository("r1")
	<-fw.started

	a.Close()

	if !fw.stopped.Load() {
		t.Fatal("App.Close must stop the watcher")
	}

	done := make(chan struct{})
	go func() {
		a.Post(func() {})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(testTimeout):
		t.Fatal("Post after Close must not block")
	}
}

func TestPauseWatchAndResumeWatchDelegateToTheActiveWatcher(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	initTestRepo(t, target)
	cfg := config.Default()
	cfg.Repositories = []config.Repository{{ID: "r1", Name: "Main", Path: target}}
	a := newTestAppWithConfig(t, cfg)
	fw := newFakeWatcher()
	a.newWatcher = func(gitrepo.Layout, watch.Options) watcherIface { return fw }
	a.ActivateRepository("r1")
	<-fw.started

	a.pauseWatch()
	a.resumeWatch()

	if fw.pauses.Load() != 1 || fw.resumes.Load() != 1 {
		t.Fatalf("pauses = %d, resumes = %d, want 1 and 1", fw.pauses.Load(), fw.resumes.Load())
	}
}

func TestPauseWatchAndResumeWatchAreNoOpsWithoutAnActiveWatcher(t *testing.T) {
	a := newTestApp(t)
	a.pauseWatch()
	a.resumeWatch()
}

func TestStopWatcherIsANoOpWithoutAnActiveWatcher(t *testing.T) {
	a := newTestApp(t)
	a.stopWatcher()
}

func TestHandleChangeSetDoesNotPostAHeadRefsRefreshForIndexOrWorkTreeChanges(t *testing.T) {
	a := newTestApp(t)

	a.handleChangeSet(watch.ChangeSet{watch.Change{Kind: watch.Index}: struct{}{}})
	waitForPostQueueDrain(t, a)
	a.handleChangeSet(watch.ChangeSet{watch.Change{Kind: watch.WorkTree}: struct{}{}})
	waitForPostQueueDrain(t, a)

	if filesModeOnDispatcher(a) != filesModeWorking || filesRowCountOnDispatcher(t, a) != 0 {
		t.Fatal("Index/WorkTree changes must reload the working copy status, not the branch snapshot")
	}
}

func TestHandleChangeSetPostsAWorkingStatusReloadForIndexOrWorkTreeChanges(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	initTestRepoWithBranch(t, target, "main")
	cfg := config.Default()
	cfg.Repositories = []config.Repository{{ID: "r1", Name: "Main", Path: target}}
	a := newTestAppWithConfig(t, cfg)
	a.ActivateRepository("r1")
	waitForWorkingRows(t, a, 0)
	if err := writeFile(target, "new.txt", "hello\n"); err != nil {
		t.Fatal(err)
	}

	a.handleChangeSet(watch.ChangeSet{watch.Change{Kind: watch.Index}: struct{}{}})

	waitForWorkingRows(t, a, 1)
	row := filesRowOnDispatcher(t, a, 0)
	if row.RelPath != "new.txt" {
		t.Fatalf("row = %+v, want the untracked new.txt", row)
	}
}

func TestHandleChangeSetSkipsTheWorkingStatusReloadWhileACommitIsSelected(t *testing.T) {
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

	a.handleChangeSet(watch.ChangeSet{watch.Change{Kind: watch.WorkTree}: struct{}{}})
	waitForPostQueueDrain(t, a)

	if got := filesRowCountOnDispatcher(t, a); got != 1 || filesModeOnDispatcher(a) != filesModeCommit {
		t.Fatalf("a WorkTree change must not disturb the selected commit's diff: rows=%d mode=%v", got, filesModeOnDispatcher(a))
	}
}

func TestHandleChangeSetPostsARefreshForHeadRefsOrState(t *testing.T) {
	kinds := []struct {
		name string
		kind watch.Kind
	}{
		{"Head", watch.Head},
		{"Refs", watch.Refs},
		{"State", watch.State},
	}
	for _, tc := range kinds {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			target := filepath.Join(dir, "main")
			initTestRepo(t, target)
			cfg := config.Default()
			cfg.Repositories = []config.Repository{{ID: "r1", Name: "Main", Path: target}}
			a := newTestAppWithConfig(t, cfg)
			a.ActivateRepository("r1")

			called := make(chan struct{}, 1)
			prevLoad := loadBranchSnapshot
			loadBranchSnapshot = func(s *refs.Store) (branches.Snapshot, error) {
				snap, err := prevLoad(s)
				select {
				case called <- struct{}{}:
				default:
				}
				return snap, err
			}
			t.Cleanup(func() { loadBranchSnapshot = prevLoad })

			a.handleChangeSet(watch.ChangeSet{watch.Change{Kind: tc.kind}: struct{}{}})
			waitForPostQueueDrain(t, a)

			select {
			case <-called:
			default:
				t.Fatalf("kind %s: expected RefreshRepository to run through the post queue", tc.name)
			}
		})
	}
}

func TestActivateRepositoryWithARealWatcherPicksUpABranchCreationWithinTwoSeconds(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	initTestRepo(t, target)
	cfg := config.Default()
	cfg.Repositories = []config.Repository{{ID: "r1", Name: "Main", Path: target}}
	a := newTestAppWithConfig(t, cfg)
	a.newWatcher = func(layout gitrepo.Layout, _ watch.Options) watcherIface {
		return watch.New(layout, watch.Options{MinInterval: 20 * time.Millisecond, MaxInterval: 50 * time.Millisecond})
	}

	a.ActivateRepository("r1")
	if branchItemExistsOnDispatcher(t, a, refs.BranchName("feature")) {
		t.Fatal("feature branch must not exist yet")
	}

	store, err := refs.Open(refs.Options{GitDir: filepath.Join(target, ".git"), Committer: testCommitter})
	if err != nil {
		t.Fatal(err)
	}
	setRef(t, store, refs.BranchName("feature"), oid(t, "66"))
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(testTimeout)
	for {
		if branchItemExistsOnDispatcher(t, a, refs.BranchName("feature")) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the real watcher did not pick up the branch creation within 2s")
		}
		time.Sleep(20 * time.Millisecond)
	}
}
