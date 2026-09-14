package app

import (
	"maps"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/oops1/gogit/internal/config"
	"github.com/oops1/gogit/internal/ui/branches"
)

func waitForBranchCache(t *testing.T, a *App) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		a.branchWG.Wait()
		close(done)
	}()
	waitForChannel(t, done, "the branch cache rebuild")
	drainPostQueue(t, a)
}

func twoRepositoryApp(t *testing.T) *App {
	t.Helper()
	dir := t.TempDir()
	first := filepath.Join(dir, "first")
	second := filepath.Join(dir, "second")
	initTestRepoWithBranch(t, first, "develop")
	initTestRepoWithBranch(t, second, "feature")
	cfg := config.Default()
	cfg.Repositories = []config.Repository{
		{ID: "r1", Name: "Main", Path: first},
		{ID: "r2", Name: "Other", Path: second},
	}
	a := newTestAppWithConfig(t, cfg)
	runOnDispatcher(t, a, func() { a.ActivateRepository("r1") })
	waitForBranchCache(t, a)
	return a
}

func stubCurrentBranch(t *testing.T, stub func(string) string) {
	t.Helper()
	prev := currentBranch
	currentBranch = stub
	t.Cleanup(func() { currentBranch = prev })
}

func cachedBranches(t *testing.T, a *App) map[string]string {
	t.Helper()
	return readOnDispatcher(t, a, func() map[string]string {
		a.branchMu.Lock()
		defer a.branchMu.Unlock()
		return maps.Clone(a.branchCache)
	})
}

func TestARefreshReadsNoOtherRepositoryFromDisk(t *testing.T) {
	a := twoRepositoryApp(t)
	var calls atomic.Int32
	stubCurrentBranch(t, func(string) string {
		calls.Add(1)
		return "other"
	})

	runOnDispatcher(t, a, a.RefreshRepository)
	runOnDispatcher(t, a, a.RefreshRepository)

	if n := calls.Load(); n != 0 {
		t.Fatalf("a refresh opened %d repositories, want none", n)
	}
	if got := cachedBranches(t, a); got["r1"] != "develop" || got["r2"] != "feature" {
		t.Fatalf("cache = %v", got)
	}
}

func TestTheBranchesOfAllRepositoriesAreReadOffTheDispatcher(t *testing.T) {
	a := twoRepositoryApp(t)
	release := make(chan struct{})
	stubCurrentBranch(t, func(string) string {
		<-release
		return "moved"
	})

	runOnDispatcher(t, a, a.refreshBranchCache)
	close(release)
	waitForBranchCache(t, a)

	text := readOnDispatcher(t, a, func() string {
		item, _ := a.reposView.Item("r2")
		return item.DisplayText()
	})
	if text != "Other (moved)" {
		t.Fatalf("r2 text = %q, want the rebuilt branch shown", text)
	}
}

func TestTheActiveBranchSeenDuringARebuildWins(t *testing.T) {
	a := twoRepositoryApp(t)
	release := make(chan struct{})
	stubCurrentBranch(t, func(string) string {
		<-release
		return "stale"
	})

	runOnDispatcher(t, a, func() {
		a.refreshBranchCache()
		a.setActiveBranch("r1", branches.Snapshot{Current: "fresh"})
	})
	close(release)
	waitForBranchCache(t, a)

	if got := cachedBranches(t, a); got["r1"] != "fresh" || got["r2"] != "stale" {
		t.Fatalf("cache = %v, want the fresh active branch and the rebuilt others", got)
	}
}

func TestAnOlderRebuildDoesNotReplaceANewerOne(t *testing.T) {
	a := twoRepositoryApp(t)

	older := a.beginBranchRebuild()
	a.beginBranchRebuild()

	if a.installBranchCache(older, map[string]string{"r1": "stale"}) {
		t.Fatal("an older rebuild replaced the cache")
	}
	if got := cachedBranches(t, a); got["r1"] != "develop" {
		t.Fatalf("cache = %v", got)
	}
}

func TestAnUnchangedRebuildRendersNothing(t *testing.T) {
	a := twoRepositoryApp(t)
	same := cachedBranches(t, a)

	if a.installBranchCache(a.beginBranchRebuild(), same) {
		t.Fatal("an identical rebuild asked for a new render")
	}
}
