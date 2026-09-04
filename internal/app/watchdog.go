package app

import (
	"context"
	"runtime"
	"sync"
	"time"
)

const (
	defaultWatchdogInterval = 5 * time.Second
	defaultWatchdogStall    = 20 * time.Second
	watchdogStackLimit      = 1 << 20
)

type stallWatch struct {
	name    string
	post    func(func())
	mu      sync.Mutex
	sent    time.Time
	running bool
	warned  bool
}

func (w *stallWatch) beat() {
	w.mu.Lock()
	w.running = false
	w.mu.Unlock()
}

func (w *stallWatch) stalledFor(now time.Time) (time.Duration, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.running {
		return 0, false
	}
	return now.Sub(w.sent), true
}

func (w *stallWatch) send(now time.Time) {
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return
	}
	w.running = true
	w.sent = now
	w.mu.Unlock()
	w.post(w.beat)
}

func (a *App) watches() []*stallWatch {
	return []*stallWatch{
		{name: "post queue", post: a.Post},
		{name: "frame loop", post: a.eng.Post},
	}
}

func (a *App) runWatchdog(ctx context.Context, watches []*stallWatch) {
	ticker := time.NewTicker(a.watchdogInterval)
	defer ticker.Stop()
	for {
		now := time.Now()
		for _, w := range watches {
			stalled, running := w.stalledFor(now)
			switch {
			case running && stalled >= a.watchdogStall:
				if !w.warned {
					w.warned = true
					a.log.Error("goroutine stalled", "watch", w.name, "for", stalled, "stacks", goroutineStacks())
				}
			case running:
			default:
				w.warned = false
				w.send(now)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

var goroutineStacks = func() string {
	buf := make([]byte, watchdogStackLimit)
	return string(buf[:runtime.Stack(buf, true)])
}
