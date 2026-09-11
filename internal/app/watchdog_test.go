package app

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

func contextWithTimeout(t *testing.T, d time.Duration) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithTimeout(t.Context(), d)
}

func TestWatchdogBeatsWhileTheQueueRuns(t *testing.T) {
	a := newTestApp(t)
	a.watchdogInterval = time.Millisecond
	a.watchdogStall = 50 * time.Millisecond
	dumps := make(chan string, 1)
	restoreStacks(t, func() string {
		select {
		case dumps <- "dumped":
		default:
		}
		return "stack"
	})

	queue := a.watches()[0]
	if queue.name != "post queue" {
		t.Fatalf("first watch = %q", queue.name)
	}
	ctx, cancel := contextWithTimeout(t, 300*time.Millisecond)
	defer cancel()
	a.runWatchdog(ctx, []*stallWatch{queue})

	select {
	case <-dumps:
		t.Fatal("watchdog reported a stall while the queue was draining")
	default:
	}
}

func TestWatchdogDumpsStacksOnceWhenAWatchStalls(t *testing.T) {
	a := newTestApp(t)
	a.watchdogInterval = time.Millisecond
	a.watchdogStall = 10 * time.Millisecond
	dumps := make(chan struct{}, 8)
	restoreStacks(t, func() string {
		dumps <- struct{}{}
		return "stack"
	})

	stuck := &stallWatch{name: "stuck", post: func(func()) {}}
	ctx, cancel := contextWithTimeout(t, 200*time.Millisecond)
	defer cancel()
	a.runWatchdog(ctx, []*stallWatch{stuck})

	if len(dumps) == 0 {
		t.Fatal("watchdog did not report the stalled watch")
	}
	if len(dumps) > 1 {
		t.Fatalf("watchdog dumped stacks %d times for one stall", len(dumps))
	}
	if !stuck.warned {
		t.Fatal("the stalled watch must stay marked as reported")
	}
}

func TestWatchdogRearmsAfterALateBeat(t *testing.T) {
	beats := make(chan func(), 4)
	w := &stallWatch{name: "late", post: func(fn func()) { beats <- fn }}
	now := time.Now()

	w.send(now)
	if _, running := w.stalledFor(now); !running {
		t.Fatal("a sent beat must count as running")
	}
	w.send(now)
	if len(beats) != 1 {
		t.Fatalf("watch posted %d beats while one was in flight", len(beats))
	}
	(<-beats)()
	if _, running := w.stalledFor(now); running {
		t.Fatal("an acknowledged beat must clear the watch")
	}
}

func TestGoroutineStacksMentionsTheRunningTest(t *testing.T) {
	if !strings.Contains(goroutineStacks(), "goroutine") {
		t.Fatal("stack dump does not look like a goroutine dump")
	}
}

func restoreStacks(t *testing.T, fn func() string) {
	t.Helper()
	prev := goroutineStacks
	goroutineStacks = fn
	t.Cleanup(func() { goroutineStacks = prev })
}

type lockedLog struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (l *lockedLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

func (l *lockedLog) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.String()
}

func TestAHungCommandLeavesItsStacksInTheLog(t *testing.T) {
	buf := &lockedLog{}
	a := newTestApp(t)
	a.log = slog.New(slog.NewTextHandler(buf, nil))
	a.watchdogInterval = time.Millisecond
	a.watchdogStall = 10 * time.Millisecond
	restoreStacks(t, func() string { return "the stacks of every goroutine" })
	release := make(chan struct{})
	a.handlers[CmdSearch] = func() { <-release }
	go a.Dispatch(CmdSearch)

	ctx, cancel := contextWithTimeout(t, 200*time.Millisecond)
	defer cancel()
	a.runWatchdog(ctx, nil)
	close(release)

	log := buf.String()
	if strings.Count(log, "command stalled") != 1 {
		t.Fatalf("log = %q, want one report of the hung command", log)
	}
	if !strings.Contains(log, string(CmdSearch)) || !strings.Contains(log, "the stacks of every goroutine") {
		t.Fatalf("log = %q, want the command and the stacks", log)
	}
}

func TestACommandInsideACommandIsTimedAsOne(t *testing.T) {
	var watch commandWatch
	start := time.Unix(1700000000, 0)

	watch.begin(CmdRefresh, start)
	watch.begin(CmdFetch, start.Add(time.Second))
	watch.end()

	id, running, late := watch.overdue(start.Add(10*time.Second), 5*time.Second)
	if !late || id != CmdRefresh || running != 10*time.Second {
		t.Fatalf("overdue = %q %v %v, want the outer command, timed from its start", id, running, late)
	}
	if _, _, again := watch.overdue(start.Add(20*time.Second), 5*time.Second); again {
		t.Fatal("a hung command must be reported once")
	}

	watch.end()
	if _, _, late := watch.overdue(start.Add(30*time.Second), 5*time.Second); late {
		t.Fatal("a finished command must not be reported")
	}
}

func TestAQuickCommandIsNotReported(t *testing.T) {
	var watch commandWatch
	start := time.Unix(1700000000, 0)

	watch.begin(CmdRefresh, start)

	if _, _, late := watch.overdue(start.Add(time.Second), 5*time.Second); late {
		t.Fatal("a command still within the limit must not be reported")
	}
}
