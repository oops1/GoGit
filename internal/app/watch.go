package app

import (
	"context"
	"iter"
	"slices"

	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/repo/watch"
)

type watcherIface interface {
	Run(ctx context.Context) iter.Seq[watch.ChangeSet]
	Poke()
	Pause()
	Resume()
}

func newRealWatcher(layout gitrepo.Layout, opts watch.Options) watcherIface {
	return watch.New(layout, opts)
}

func (a *App) startWatcher(layout gitrepo.Layout) {
	a.watchRunMu.Lock()
	defer a.watchRunMu.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	skip := a.ignoredDirectories()
	w := a.newWatcher(layout, watch.Options{WorkTreeDepth: a.cfg.Git.WorkTreeDepth, SkipDirs: skip})
	a.watchMu.Lock()
	a.watchCancel = cancel
	a.watcher = w
	a.watchSkip = skip
	a.watchMu.Unlock()
	a.watchWG.Go(func() { a.runWatcher(ctx, w) })
}

func (a *App) restartWatcherForCurrentRepository() {
	o := a.opened()
	if o == nil {
		return
	}
	a.stopWatcher()
	a.startWatcher(o.repo.Layout())
}

func (a *App) ignoredDirectories() []string {
	a.filesMu.Lock()
	defer a.filesMu.Unlock()
	return slices.Clone(a.mutedDirs)
}

func (a *App) syncWatcherSkips() {
	a.watchMu.Lock()
	current := a.watchSkip
	running := a.watcher != nil
	a.watchMu.Unlock()
	if !running || slices.Equal(current, a.ignoredDirectories()) {
		return
	}
	a.restartWatcherForCurrentRepository()
}

func (a *App) runWatcher(ctx context.Context, w watcherIface) {
	for set := range w.Run(ctx) {
		a.handleChangeSet(set)
	}
}

func (a *App) handleChangeSet(set watch.ChangeSet) {
	if set.Has(watch.Head) || set.Has(watch.Refs) || set.Has(watch.State) {
		a.Post(a.RefreshRepository)
	}
	if set.Has(watch.Index) || set.Has(watch.WorkTree) {
		a.Post(a.refreshWorkingStatus)
	}
}

func (a *App) stopWatcher() {
	a.watchRunMu.Lock()
	defer a.watchRunMu.Unlock()
	a.watchMu.Lock()
	cancel := a.watchCancel
	a.watchCancel = nil
	a.watcher = nil
	a.watchMu.Unlock()
	if cancel != nil {
		cancel()
	}
	a.watchWG.Wait()
}

func (a *App) pauseWatch() {
	a.watchMu.Lock()
	w := a.watcher
	a.watchMu.Unlock()
	if w != nil {
		w.Pause()
	}
}

func (a *App) resumeWatch() {
	a.watchMu.Lock()
	w := a.watcher
	a.watchMu.Unlock()
	if w != nil {
		w.Resume()
	}
}
