package app

import (
	"context"
	"runtime"
	"sync"
	"time"
)

const (
	defaultWatchdogInterval = 2 * time.Second
	defaultWatchdogStall    = 8 * time.Second
	watchdogStackLimit      = 1 << 20
)

type commandWatch struct {
	mu      sync.Mutex
	command CommandID
	started time.Time
	depth   int
	warned  bool
}

func (c *commandWatch) begin(id CommandID, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.depth == 0 {
		c.command = id
		c.started = now
		c.warned = false
	}
	c.depth++
}

func (c *commandWatch) end() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.depth--
}

func (c *commandWatch) overdue(now time.Time, limit time.Duration) (CommandID, time.Duration, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.depth == 0 || c.warned {
		return "", 0, false
	}
	running := now.Sub(c.started)
	if running < limit {
		return "", 0, false
	}
	c.warned = true
	return c.command, running, true
}

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
		if id, running, late := a.commands.overdue(now, a.watchdogStall); late {
			a.log.Error("command stalled", "command", string(id), "for", running, "stacks", goroutineStacks())
		}
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
