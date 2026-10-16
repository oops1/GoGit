package app

import (
	"context"
	"errors"
	"sync"

	"github.com/oops1/gogit/internal/gitcore/ops"
)

var ensureCommitGraph = ops.EnsureCommitGraph

type graphWriter struct {
	mu     sync.Mutex
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func (a *App) startGraphWrite(o *openedRepository) {
	ctx, cancel := context.WithCancel(context.Background())
	a.graphWrite.mu.Lock()
	a.graphWrite.cancel = cancel
	a.graphWrite.mu.Unlock()
	a.graphWrite.wg.Go(func() {
		written, err := ensureCommitGraph(ctx, o.repo)
		switch {
		case errors.Is(err, context.Canceled):
		case err != nil:
			a.log.Warn("write commit graph failed", "path", o.path, "error", err)
		case written > 0:
			a.log.Info("commit graph written", "path", o.path, "commits", written)
		}
	})
}

func (a *App) stopGraphWrite() {
	a.graphWrite.mu.Lock()
	cancel := a.graphWrite.cancel
	a.graphWrite.cancel = nil
	a.graphWrite.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	a.graphWrite.wg.Wait()
}
