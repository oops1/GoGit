package app

import (
	"context"
	"errors"
	"time"

	"github.com/oops1/gogit/internal/gitcore/ops"
	"github.com/oops1/gogit/internal/gitcore/remote"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/repo"
)

var autoFetchOpsFetch = ops.Fetch

type divergenceState struct {
	repo.Divergence
	HasUpstream bool
}

func (a *App) setDivergence(d repo.Divergence, hasUpstream bool) {
	a.stateMu.Lock()
	a.divergence = d
	a.divergenceHasUpstream = hasUpstream
	a.stateMu.Unlock()
}

func (a *App) getDivergence() divergenceState {
	a.stateMu.RLock()
	defer a.stateMu.RUnlock()
	return divergenceState{Divergence: a.divergence, HasUpstream: a.divergenceHasUpstream}
}

func divergenceStatusSuffix(d divergenceState) string {
	switch {
	case !d.HasUpstream:
		return ""
	case d.Ahead > 0 && d.Behind > 0:
		return " " + i18n.Tf("Status.Diverged", d.Ahead, d.Behind)
	case d.Ahead > 0:
		return " " + i18n.Tf("Status.Ahead", d.Ahead)
	case d.Behind > 0:
		return " " + i18n.Tf("Status.Behind", d.Behind)
	default:
		return ""
	}
}

func (a *App) computeDivergence(o *openedRepository) (repo.Divergence, bool) {
	div, hasUpstream, err := repo.AheadBehind(o.repo)
	if err != nil {
		a.log.Warn("compute ahead/behind failed", "path", o.path, "error", err)
		return repo.Divergence{}, false
	}
	return div, hasUpstream
}

func (a *App) refreshDivergence(o *openedRepository) {
	div, hasUpstream := a.computeDivergence(o)
	a.setDivergence(div, hasUpstream)
}

func (a *App) refreshDivergenceAfterFetch(o *openedRepository) {
	if a.opened() != o {
		return
	}
	a.refreshDivergence(o)
	snap, err := loadBranchSnapshot(o.store)
	if err != nil {
		return
	}
	a.statusBranchLabel.SetText(a.branchStatusTextWithDivergence(snap))
	a.reposView.Render(a.registry, a.repoTreeState())
}

func (a *App) startAutoFetch() {
	a.autoFetchRunMu.Lock()
	defer a.autoFetchRunMu.Unlock()
	a.stopAutoFetchLocked()
	o := a.opened()
	if o == nil || !a.hasRemotes() {
		return
	}
	if !a.autoFetchEnabled(o) {
		return
	}
	interval := time.Duration(a.cfg.Git.FetchInterval) * time.Second
	if interval <= 0 {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.autoFetchMu.Lock()
	a.autoFetchCancel = cancel
	a.autoFetchMu.Unlock()
	a.autoFetchWG.Go(func() { a.runAutoFetch(ctx, interval) })
}

func (a *App) restartAutoFetch() {
	a.startAutoFetch()
}

func (a *App) stopAutoFetch() {
	a.autoFetchRunMu.Lock()
	defer a.autoFetchRunMu.Unlock()
	a.stopAutoFetchLocked()
}

func (a *App) stopAutoFetchLocked() {
	a.autoFetchMu.Lock()
	cancel := a.autoFetchCancel
	a.autoFetchCancel = nil
	a.autoFetchMu.Unlock()
	if cancel != nil {
		cancel()
	}
	a.autoFetchWG.Wait()
}

func (a *App) runAutoFetch(ctx context.Context, interval time.Duration) {
	timer := time.NewTimer(interval)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			a.performAutoFetch(ctx)
			timer.Reset(interval)
		}
	}
}

func (a *App) autoFetchBusy() bool {
	a.netMu.Lock()
	defer a.netMu.Unlock()
	return a.netCancel != nil
}

func (a *App) performAutoFetch(ctx context.Context) {
	o := a.opened()
	if o == nil || a.autoFetchBusy() {
		return
	}
	r, err := a.freshRepo(o)
	if err != nil {
		a.log.Warn("auto-fetch open repository failed", "path", o.path, "error", err)
		return
	}
	defer func() { _ = r.Close() }()
	_, err = autoFetchOpsFetch(ctx, r, a.effectiveDefaultRemote(r), remote.FetchOptions{Transport: a.transportOptions(nil)})
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			a.log.Warn("auto-fetch failed", "path", o.path, "error", err)
		}
		return
	}
	a.Post(func() { a.refreshDivergenceAfterFetch(o) })
}
