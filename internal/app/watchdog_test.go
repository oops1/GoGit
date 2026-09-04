package app

import (
	"context"
	"strings"
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
