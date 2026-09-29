package app

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"runtime/metrics"
	"testing"
	"time"
)

func TestTheMemoryWatchReportsEachThresholdOnce(t *testing.T) {
	watch := newMemoryWatch()

	if _, crossed := watch.crossed(memoryFirstReport - 1); crossed {
		t.Fatal("a heap below the first threshold was reported")
	}
	reached, crossed := watch.crossed(memoryFirstReport)
	if !crossed || reached != memoryFirstReport {
		t.Fatalf("first report = %d, %v", reached, crossed)
	}
	if _, crossed := watch.crossed(memoryFirstReport); crossed {
		t.Fatal("the same threshold was reported twice")
	}
	reached, crossed = watch.crossed(memoryFirstReport * 2)
	if !crossed || reached != memoryFirstReport*2 {
		t.Fatalf("second report = %d, %v", reached, crossed)
	}
}

func TestTheMemoryWatchSkipsThresholdsItJumpedOver(t *testing.T) {
	watch := newMemoryWatch()

	reached, crossed := watch.crossed(memoryFirstReport * 8)
	if !crossed || reached != memoryFirstReport {
		t.Fatalf("report = %d, %v, want the first threshold named", reached, crossed)
	}
	if _, crossed := watch.crossed(memoryFirstReport * 8); crossed {
		t.Fatal("the same heap was reported again")
	}
	if _, crossed := watch.crossed(memoryFirstReport * 16); !crossed {
		t.Fatal("the next doubling was not reported")
	}
}

func TestTheAppLogsTheHeapOnlyWhenItGrowsPastAThreshold(t *testing.T) {
	a := newTestApp(t)
	prev := readHeapInUse
	heap := uint64(memoryFirstReport - 1)
	readHeapInUse = func([]metrics.Sample) uint64 { return heap }
	t.Cleanup(func() { readHeapInUse = prev })

	at := time.Now()
	a.reportMemoryGrowth(at)
	if _, crossed := a.memory.crossed(memoryFirstReport - 1); crossed {
		t.Fatal("a small heap moved the threshold")
	}

	heap = memoryFirstReport * 4
	a.reportMemoryGrowth(at.Add(memorySampleEvery))
	if _, crossed := a.memory.crossed(memoryFirstReport * 4); crossed {
		t.Fatal("the crossed threshold was not remembered")
	}

	heap = memoryFirstReport * 64
	a.reportMemoryGrowth(at.Add(time.Second))
	if _, crossed := a.memory.crossed(memoryFirstReport * 8); !crossed {
		t.Fatal("a reading inside the interval moved the threshold")
	}
}

func TestTheMemoryReportNamesTheRepositoryAndTheRunningOperation(t *testing.T) {
	a := newTestApp(t)

	if got := a.runningTitleForLog(); got != "nothing" {
		t.Fatalf("running = %q, want the placeholder for an idle app", got)
	}
	if got := a.openedPath(); got != "" {
		t.Fatalf("repository = %q, want none open", got)
	}

	isolateGitConfig(t)
	target := filepath.Join(t.TempDir(), "repo")
	initTestRepoWithBranch(t, target, "main")
	a.setOpened(openTestRepository(t, target))
	if got := a.openedPath(); got != target {
		t.Fatalf("repository = %q, want %q", got, target)
	}

	started := make(chan struct{})
	release := make(chan struct{})
	captureOperationViews(t)
	a.RunOperation("Fetch", func(context.Context, OperationReporter) error {
		close(started)
		<-release
		return nil
	})
	<-started
	if got := a.runningTitleForLog(); got != "Fetch" {
		t.Fatalf("running = %q, want the operation title", got)
	}
	close(release)
	a.stopNetOperations()
}

func TestTheMemoryWatchSamplesNoMoreOftenThanItsInterval(t *testing.T) {
	watch := newMemoryWatch()
	reads := 0
	prev := readHeapInUse
	readHeapInUse = func([]metrics.Sample) uint64 {
		reads++
		return 1
	}
	t.Cleanup(func() { readHeapInUse = prev })

	at := time.Now()
	for range 100 {
		watch.inUse(at)
		at = at.Add(time.Millisecond)
	}
	if reads != 1 {
		t.Fatalf("the runtime was asked %d times inside one interval", reads)
	}
	if _, sampled := watch.inUse(at.Add(memorySampleEvery)); !sampled || reads != 2 {
		t.Fatalf("after the interval the watch read %d times, sampled = %v", reads, sampled)
	}
}

func TestTheHeapReadingComesFromTheRuntimeAndSurvivesAnUnknownMetric(t *testing.T) {
	if got := heapInUseOf([]metrics.Sample{{Name: heapInUseMetric}}); got == 0 {
		t.Fatal("the runtime reported an empty heap")
	}
	if got := heapInUseOf([]metrics.Sample{{Name: "/does/not/exist:bytes"}}); got != 0 {
		t.Fatalf("an unknown metric gave %d, want 0", got)
	}
}

func TestTheAppGivesUnusedMemoryBackOnlyWhenThereIsEnoughOfIt(t *testing.T) {
	a := newTestApp(t)
	freed := 0
	unused := uint64(memoryKeptUnused - 1)
	prevFree, prevStats, prevHeap := freeOSMemory, readMemStats, readHeapInUse
	prevStart := startFreeOSMemory
	startFreeOSMemory = func(free func()) { free() }
	t.Cleanup(func() { startFreeOSMemory = prevStart })
	freeOSMemory = func() { freed++ }
	readMemStats = func(stats *runtime.MemStats) { stats.HeapIdle, stats.HeapReleased = unused, 0 }
	readHeapInUse = func([]metrics.Sample) uint64 { return 1 }
	t.Cleanup(func() { freeOSMemory, readMemStats, readHeapInUse = prevFree, prevStats, prevHeap })

	at := time.Now()
	a.reportMemoryGrowth(at)
	if freed != 0 {
		t.Fatalf("memory was given back while only %d bytes lay unused", unused)
	}

	unused = memoryKeptUnused
	a.reportMemoryGrowth(at.Add(memorySampleEvery))
	if freed != 1 {
		t.Fatalf("the system got its memory back %d times, want once", freed)
	}
}

func TestTheAppCollectsTheGarbageOfAFinishedOperationWhileItIdles(t *testing.T) {
	a := newTestApp(t)
	freed := 0
	prevFree, prevStats, prevHeap := freeOSMemory, readMemStats, readHeapInUse
	prevStart := startFreeOSMemory
	startFreeOSMemory = func(free func()) { free() }
	t.Cleanup(func() { startFreeOSMemory = prevStart })
	freeOSMemory = func() { freed++ }
	readMemStats = func(stats *runtime.MemStats) {
		stats.HeapAlloc, stats.HeapIdle, stats.HeapReleased = memoryIdleHeap, 0, 0
	}
	readHeapInUse = func([]metrics.Sample) uint64 { return 1 }
	t.Cleanup(func() { freeOSMemory, readMemStats, readHeapInUse = prevFree, prevStats, prevHeap })

	a.reportMemoryGrowth(time.Now())
	if freed != 1 {
		t.Fatalf("an idle app with a large heap collected %d times, want once", freed)
	}
}

func TestTheAppWritesAHeapProfileWhenTheHeapCrossesAThreshold(t *testing.T) {
	a := newTestApp(t)
	prevWrite, prevStats, prevHeap := writeHeapProfile, readMemStats, readHeapInUse
	written := ""
	writeHeapProfile = func(w io.Writer) error {
		written = w.(*os.File).Name()
		return nil
	}
	readMemStats = func(*runtime.MemStats) {}
	readHeapInUse = func([]metrics.Sample) uint64 { return memoryFirstReport }
	t.Cleanup(func() { writeHeapProfile, readMemStats, readHeapInUse = prevWrite, prevStats, prevHeap })

	a.reportMemoryGrowth(time.Now())

	if filepath.Dir(written) != filepath.Dir(a.paths.LogFile()) {
		t.Fatalf("the profile went to %q, want it beside %q", written, a.paths.LogFile())
	}
	if _, err := os.Stat(written); err != nil {
		t.Fatalf("the profile file is missing: %v", err)
	}
}

func TestTheAppSurvivesAHeapProfileItCannotWrite(t *testing.T) {
	a := newTestApp(t)
	prevWrite, prevStats, prevHeap := writeHeapProfile, readMemStats, readHeapInUse
	writeHeapProfile = func(io.Writer) error { return errors.New("no room") }
	readMemStats = func(*runtime.MemStats) {}
	readHeapInUse = func([]metrics.Sample) uint64 { return memoryFirstReport }
	t.Cleanup(func() { writeHeapProfile, readMemStats, readHeapInUse = prevWrite, prevStats, prevHeap })

	a.reportMemoryGrowth(time.Now())
}
