package app

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/oops1/gogit/internal/gitcore/ops"
	"github.com/oops1/gogit/internal/gitcore/refs"
	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/gitcore/revision"
	"github.com/oops1/gogit/internal/repo"
)

func waitForBranchLabel(t *testing.T, a *App, ref refs.Name, want string) {
	t.Helper()
	deadline := time.Now().Add(testTimeout)
	var last string
	for {
		last = readOnDispatcher(t, a, func() string {
			item, ok := a.branchesView.Item(ref)
			if !ok {
				return ""
			}
			if item.Detail == "" {
				return item.DisplayText()
			}
			return item.DisplayText() + " " + item.Detail
		})
		if last == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("branch %s label = %q, want %q", ref, last, want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func cloneForDivergence(t *testing.T) (a *App, server, local string) {
	t.Helper()
	dir := t.TempDir()
	server = filepath.Join(dir, "server")
	local = filepath.Join(dir, "local")
	initRemoteServerRepo(t, server, "main")
	a = newRemoteTestApp(t)
	cloneIntoRegistry(t, a, server, local)
	return a, server, local
}

func TestBranchesPaneShowsNoSuffixRightAfterCloneMatchesUpstream(t *testing.T) {
	a, _, _ := cloneForDivergence(t)

	waitForBranchLabel(t, a, refs.BranchName("main"), "main = origin")
}

func TestBranchesPaneShowsAheadCountForUnpushedLocalCommits(t *testing.T) {
	a, _, local := cloneForDivergence(t)

	commitTestFiles(t, local, map[string]string{"extra.txt": "extra\n"}, nil)
	a.RefreshRepository()

	waitForBranchLabel(t, a, refs.BranchName("main"), "main = origin ↑1")
}

func TestBranchesPaneShowsBehindCountAfterFetchingNewServerCommits(t *testing.T) {
	a, server, _ := cloneForDivergence(t)

	addRemoteServerCommit(t, server, "main", "second.txt", "second\n")
	views := captureOperationViews(t)
	if !a.Dispatch(CmdFetch) {
		t.Fatal("fetch must be dispatched once a remote exists")
	}
	waitForFinishedOperation(t, a, lastOperationView(t, views))

	waitForBranchLabel(t, a, refs.BranchName("main"), "main = origin ↓1")
}

func TestBranchesPaneShowsBothCountsWhenHistoriesDiverge(t *testing.T) {
	a, server, local := cloneForDivergence(t)

	addRemoteServerCommit(t, server, "main", "second.txt", "second\n")
	views := captureOperationViews(t)
	if !a.Dispatch(CmdFetch) {
		t.Fatal("fetch must be dispatched once a remote exists")
	}
	waitForFinishedOperation(t, a, lastOperationView(t, views))
	commitTestFiles(t, local, map[string]string{"extra.txt": "extra\n"}, nil)
	a.RefreshRepository()

	waitForBranchLabel(t, a, refs.BranchName("main"), "main = origin ↑1 ↓1")
}

func TestBranchesPaneShowsNoSuffixForABranchWithoutUpstream(t *testing.T) {
	a, _, local := cloneForDivergence(t)

	r, err := gitrepo.Open(local, gitrepo.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runStartBranch(t.Context(), r, "scratch", "main", ops.StartBranchOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	a.RefreshRepository()

	waitForBranchLabel(t, a, refs.BranchName("scratch"), "scratch")
}

func TestBranchDivergenceComputationRunsInTheBackgroundWithoutBlockingRefresh(t *testing.T) {
	a, _, local := cloneForDivergence(t)
	addLocalCommitWithMessage(t, local, "main", "second.txt", "second\n", "second")
	o := a.opened()
	o.divergence.keepOnly(nil)
	snap, err := loadBranchSnapshot(o.store)
	if err != nil {
		t.Fatal(err)
	}
	a.enrichBranchSnapshot(o, &snap)

	release := make(chan struct{})
	entered := make(chan struct{}, 1)
	prev := manyAheadBehind
	manyAheadBehind = func(source revision.Context, pairs []repo.BranchPair) (map[refs.Name]repo.Divergence, error) {
		select {
		case entered <- struct{}{}:
		default:
		}
		<-release
		return prev(source, pairs)
	}
	t.Cleanup(func() {
		manyAheadBehind = prev
		close(release)
	})

	done := make(chan struct{})
	go func() {
		a.refreshBranchDivergence(o, snap)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(testTimeout):
		t.Fatal("refreshBranchDivergence did not return; it must not wait for the divergence computation")
	}

	select {
	case <-entered:
	case <-time.After(testTimeout):
		t.Fatal("the background computation was never started")
	}
}

func TestBranchDivergenceLoaderReturnsNothingWhenCountingFails(t *testing.T) {
	a, _, _ := cloneForDivergence(t)
	o := a.opened()
	o.divergence.keepOnly(nil)
	loader := branchDivergenceLoader{
		many: func(revision.Context, []repo.BranchPair) (map[refs.Name]repo.Divergence, error) {
			return nil, errors.New("boom")
		},
		log: a.log,
	}
	pair := memoPair(t, "main", "11", "22")

	if got := loader.load(o, []repo.BranchPair{pair}); got != nil {
		t.Fatalf("load returned %v, want nothing after a failed count", got)
	}
}
