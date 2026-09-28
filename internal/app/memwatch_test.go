package app

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"
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
	prev := readMemStats
	heap := uint64(memoryFirstReport - 1)
	readMemStats = func(stats *runtime.MemStats) { stats.HeapAlloc = heap }
	t.Cleanup(func() { readMemStats = prev })

	a.reportMemoryGrowth()
	if _, crossed := a.memory.crossed(memoryFirstReport - 1); crossed {
		t.Fatal("a small heap moved the threshold")
	}

	heap = memoryFirstReport * 4
	a.reportMemoryGrowth()
	if _, crossed := a.memory.crossed(memoryFirstReport * 4); crossed {
		t.Fatal("the crossed threshold was not remembered")
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
