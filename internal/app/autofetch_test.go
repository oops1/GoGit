package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/remote"
	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/repo"
	"github.com/oops1/gogit/internal/ui/branches"
	"github.com/oops1/gogit/internal/ui/settings"
)

var errAutoFetchInjected = errors.New("autofetch: injected failure")

func stubAutoFetchCounting(t *testing.T) *atomic.Int32 {
	t.Helper()
	var calls atomic.Int32
	prev := autoFetchOpsFetch
	autoFetchOpsFetch = func(context.Context, *gitrepo.Repository, string, remote.FetchOptions) (remote.FetchResult, error) {
		calls.Add(1)
		return remote.FetchResult{}, nil
	}
	t.Cleanup(func() { autoFetchOpsFetch = prev })
	return &calls
}

func stubAutoFetchFailing(t *testing.T) *atomic.Int32 {
	t.Helper()
	var calls atomic.Int32
	prev := autoFetchOpsFetch
	autoFetchOpsFetch = func(context.Context, *gitrepo.Repository, string, remote.FetchOptions) (remote.FetchResult, error) {
		calls.Add(1)
		return remote.FetchResult{}, errAutoFetchInjected
	}
	t.Cleanup(func() { autoFetchOpsFetch = prev })
	return &calls
}

func newAutoFetchClone(t *testing.T) (a *App, server, local string) {
	t.Helper()
	dir := t.TempDir()
	server = filepath.Join(dir, "server")
	local = filepath.Join(dir, "local")
	initRemoteServerRepo(t, server, "main")
	a = newRemoteTestApp(t)
	cloneIntoRegistry(t, a, server, local)
	return a, server, local
}

func autoFetchRunning(a *App) bool {
	a.autoFetchMu.Lock()
	defer a.autoFetchMu.Unlock()
	return a.autoFetchCancel != nil
}

func TestAutoFetchFiresAfterTheConfiguredInterval(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		calls := stubAutoFetchCounting(t)
		a, _, _ := newAutoFetchClone(t)
		a.cfg.Git.AutoFetch = true
		a.cfg.Git.FetchInterval = 5
		a.restartAutoFetch()

		time.Sleep(4 * time.Second)
		synctest.Wait()
		if calls.Load() != 0 {
			t.Fatalf("calls = %d before the interval elapsed, want 0", calls.Load())
		}

		time.Sleep(2 * time.Second)
		synctest.Wait()
		if calls.Load() != 1 {
			t.Fatalf("calls = %d after the interval elapsed, want 1", calls.Load())
		}
	})
}

func TestAutoFetchDoesNotFireWhenDisabled(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		calls := stubAutoFetchCounting(t)
		a, _, _ := newAutoFetchClone(t)
		a.cfg.Git.AutoFetch = false
		a.cfg.Git.FetchInterval = 5
		a.restartAutoFetch()

		time.Sleep(30 * time.Second)
		synctest.Wait()
		if calls.Load() != 0 {
			t.Fatalf("calls = %d, want 0 while auto-fetch is disabled", calls.Load())
		}
		if autoFetchRunning(a) {
			t.Fatal("no timer should be running while auto-fetch is disabled")
		}
	})
}

func TestAutoFetchRestartsOnIntervalChange(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		calls := stubAutoFetchCounting(t)
		a, _, _ := newAutoFetchClone(t)
		a.cfg.Git.AutoFetch = true
		a.cfg.Git.FetchInterval = 10
		a.restartAutoFetch()

		time.Sleep(6 * time.Second)
		synctest.Wait()

		a.cfg.Git.FetchInterval = 2
		a.restartAutoFetch()

		time.Sleep(2 * time.Second)
		synctest.Wait()
		if calls.Load() != 1 {
			t.Fatalf("calls = %d after restarting with a shorter interval, want 1", calls.Load())
		}
	})
}

func TestAutoFetchStopsWhenTheRepositoryCloses(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		calls := stubAutoFetchCounting(t)
		a, _, _ := newAutoFetchClone(t)
		a.cfg.Git.AutoFetch = true
		a.cfg.Git.FetchInterval = 5
		a.restartAutoFetch()

		a.CloseRepository()
		if autoFetchRunning(a) {
			t.Fatal("closing the repository must stop the auto-fetch timer")
		}

		time.Sleep(30 * time.Second)
		synctest.Wait()
		if calls.Load() != 0 {
			t.Fatalf("calls = %d after the repository closed, want 0", calls.Load())
		}
	})
}

func TestAutoFetchDoesNotStartWithoutAnOpenRepository(t *testing.T) {
	a := newRemoteTestApp(t)
	a.cfg.Git.AutoFetch = true
	a.cfg.Git.FetchInterval = 5
	a.startAutoFetch()
	if autoFetchRunning(a) {
		t.Fatal("auto-fetch must not start without an open repository")
	}
}

func TestAutoFetchDoesNotStartWithoutRemotes(t *testing.T) {
	dir := t.TempDir()
	plain := filepath.Join(dir, "plain")
	initTestRepoWithBranch(t, plain, "main")
	a := newRemoteTestApp(t)
	a.cfg.Git.AutoFetch = true
	a.cfg.Git.FetchInterval = 5
	node, err := a.registry.AddRepository("plain", plain, "")
	if err != nil {
		t.Fatal(err)
	}
	a.ActivateRepository(node.ID)
	if autoFetchRunning(a) {
		t.Fatal("auto-fetch must not start for a repository without remotes")
	}
}

func TestAutoFetchDoesNotStartWithANonPositiveInterval(t *testing.T) {
	a, _, _ := newAutoFetchClone(t)
	a.cfg.Git.AutoFetch = true
	a.cfg.Git.FetchInterval = 0
	a.startAutoFetch()
	if autoFetchRunning(a) {
		t.Fatal("auto-fetch must not start with a non-positive interval")
	}
}

func TestPerformAutoFetchSkipsATickWhileAManualOperationIsRunning(t *testing.T) {
	calls := stubAutoFetchCounting(t)
	a, _, _ := newAutoFetchClone(t)

	a.netMu.Lock()
	_, a.netCancel = context.WithCancel(context.Background())
	a.netMu.Unlock()
	t.Cleanup(func() {
		a.netMu.Lock()
		a.netCancel = nil
		a.netMu.Unlock()
	})

	a.performAutoFetch(context.Background())
	if calls.Load() != 0 {
		t.Fatalf("calls = %d while a manual operation was running, want 0", calls.Load())
	}
}

func TestPerformAutoFetchDoesNothingWithoutAnOpenRepository(t *testing.T) {
	calls := stubAutoFetchCounting(t)
	a := newRemoteTestApp(t)
	a.performAutoFetch(context.Background())
	if calls.Load() != 0 {
		t.Fatalf("calls = %d without an open repository, want 0", calls.Load())
	}
}

func TestPerformAutoFetchLogsAndReturnsOnFetchFailure(t *testing.T) {
	calls := stubAutoFetchFailing(t)
	a, _, _ := newAutoFetchClone(t)
	before := a.getDivergence()

	a.performAutoFetch(context.Background())
	if calls.Load() != 1 {
		t.Fatalf("calls = %d, want 1", calls.Load())
	}
	waitForPostQueueDrain(t, a)
	if div := a.getDivergence(); div != before {
		t.Fatalf("a failed fetch must not change the cached divergence: got %+v, want %+v", div, before)
	}
}

func TestPerformAutoFetchTreatsCancellationQuietly(t *testing.T) {
	prev := autoFetchOpsFetch
	autoFetchOpsFetch = func(context.Context, *gitrepo.Repository, string, remote.FetchOptions) (remote.FetchResult, error) {
		return remote.FetchResult{}, context.Canceled
	}
	t.Cleanup(func() { autoFetchOpsFetch = prev })
	a, _, _ := newAutoFetchClone(t)

	a.performAutoFetch(context.Background())
}

func TestPerformAutoFetchUpdatesStatusBarAndRepositoriesPanel(t *testing.T) {
	a, server, _ := newAutoFetchClone(t)

	addRemoteServerCommit(t, server, "main", "second.txt", "second\n")

	a.performAutoFetch(context.Background())
	waitForPostQueueDrain(t, a)

	text := readOnDispatcher(t, a, a.statusBranchLabel.Text)
	want := "main " + i18n.Tf("Status.Behind", 1)
	if text != want {
		t.Fatalf("status branch text = %q, want %q", text, want)
	}

	div := a.getDivergence()
	if !div.HasUpstream || div.Ahead != 0 || div.Behind != 1 {
		t.Fatalf("divergence = %+v, want upstream with 0 ahead and 1 behind", div)
	}
}

func TestRefreshDivergenceAfterFetchIgnoresAStaleRepository(t *testing.T) {
	a, _, _ := newAutoFetchClone(t)
	stale := a.opened()

	a.CloseRepository()

	a.refreshDivergenceAfterFetch(stale)
	if a.getDivergence().HasUpstream {
		t.Fatal("a stale repository must not update the cached divergence")
	}
}

func TestComputeDivergenceLogsAndReturnsZeroOnFailure(t *testing.T) {
	a, _, local := newAutoFetchClone(t)

	headFile := filepath.Join(local, ".git", "HEAD")
	if err := os.WriteFile(headFile, []byte("ref: refs/heads/.bad\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	div, hasUpstream := a.computeDivergence(a.opened())
	if hasUpstream || div != (repo.Divergence{}) {
		t.Fatalf("computeDivergence = %+v/%v, want zero/false on failure", div, hasUpstream)
	}
}

func TestDivergenceStatusSuffixCoversEveryCombination(t *testing.T) {
	cases := []struct {
		name string
		d    divergenceState
		want string
	}{
		{"no upstream", divergenceState{}, ""},
		{"up to date", divergenceState{HasUpstream: true}, ""},
		{"ahead only", divergenceState{Divergence: repo.Divergence{Ahead: 2}, HasUpstream: true}, " " + i18n.Tf("Status.Ahead", 2)},
		{"behind only", divergenceState{Divergence: repo.Divergence{Behind: 3}, HasUpstream: true}, " " + i18n.Tf("Status.Behind", 3)},
		{"diverged", divergenceState{Divergence: repo.Divergence{Ahead: 1, Behind: 2}, HasUpstream: true}, " " + i18n.Tf("Status.Diverged", 1, 2)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := divergenceStatusSuffix(tc.d); got != tc.want {
				t.Fatalf("divergenceStatusSuffix(%+v) = %q, want %q", tc.d, got, tc.want)
			}
		})
	}
}

func TestRefreshDivergenceAfterFetchIgnoresASnapshotLoadFailure(t *testing.T) {
	a, _, _ := newAutoFetchClone(t)
	o := a.opened()
	before := readOnDispatcher(t, a, a.statusBranchLabel.Text)

	prev := loadBranchSnapshot
	loadBranchSnapshot = func(*refs.Store) (branches.Snapshot, error) { return branches.Snapshot{}, errAutoFetchInjected }
	t.Cleanup(func() { loadBranchSnapshot = prev })

	a.refreshDivergenceAfterFetch(o)
	if got := readOnDispatcher(t, a, a.statusBranchLabel.Text); got != before {
		t.Fatalf("status branch text = %q, want unchanged %q", got, before)
	}
}

func TestPerformAutoFetchLogsWhenOpeningTheRepositoryFails(t *testing.T) {
	calls := stubAutoFetchCounting(t)
	a, _, _ := newAutoFetchClone(t)

	prev := openGitRepository
	openGitRepository = func(string, gitrepo.OpenOptions) (*gitrepo.Repository, error) { return nil, errAutoFetchInjected }
	t.Cleanup(func() { openGitRepository = prev })

	a.performAutoFetch(context.Background())
	if calls.Load() != 0 {
		t.Fatalf("calls = %d, want 0 when the repository cannot be reopened", calls.Load())
	}
}

func TestSettingsApplyRestartsAutoFetch(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		calls := stubAutoFetchCounting(t)
		a, _, _ := newAutoFetchClone(t)
		a.cfg.Git.AutoFetch = true
		a.cfg.Git.FetchInterval = 3600

		model := settings.FromConfig(a.cfg)
		model.AutoFetch = true
		model.FetchInterval = settings.MinFetchInterval
		a.applySettings(model, true)

		time.Sleep(settings.MinFetchInterval * time.Second)
		synctest.Wait()
		if calls.Load() != 1 {
			t.Fatalf("calls = %d after applying a shorter fetch interval, want 1", calls.Load())
		}
	})
}
