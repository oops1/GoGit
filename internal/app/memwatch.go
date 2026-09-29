package app

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"runtime/metrics"
	"runtime/pprof"
	"sync"
	"sync/atomic"
	"time"
)

const (
	memoryFirstReport = 512 << 20
	memoryStepFactor  = 2
	memorySampleEvery = 30 * time.Second
	memoryKeptUnused  = 256 << 20
	memoryIdleHeap    = 512 << 20

	heapInUseMetric = "/memory/classes/heap/objects:bytes"
)

type memoryWatch struct {
	freeing atomic.Bool
	mu      sync.Mutex
	next    uint64
	sample  []metrics.Sample
	sampled time.Time
}

func newMemoryWatch() *memoryWatch {
	return &memoryWatch{
		next:   memoryFirstReport,
		sample: []metrics.Sample{{Name: heapInUseMetric}},
	}
}

func (m *memoryWatch) inUse(now time.Time) (uint64, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.sampled.IsZero() && now.Sub(m.sampled) < memorySampleEvery {
		return 0, false
	}
	m.sampled = now
	return readHeapInUse(m.sample), true
}

func (m *memoryWatch) crossed(inUse uint64) (uint64, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if inUse < m.next {
		return 0, false
	}
	reached := m.next
	for m.next <= inUse {
		m.next *= memoryStepFactor
	}
	return reached, true
}

func heapInUseOf(sample []metrics.Sample) uint64 {
	metrics.Read(sample)
	if sample[0].Value.Kind() != metrics.KindUint64 {
		return 0
	}
	return sample[0].Value.Uint64()
}

var (
	readHeapInUse     = heapInUseOf
	readMemStats      = runtime.ReadMemStats
	freeOSMemory      = debug.FreeOSMemory
	startFreeOSMemory = func(free func()) { go free() }
	writeHeapProfile  = pprof.WriteHeapProfile
)

func (a *App) reportMemoryGrowth(now time.Time) {
	inUse, sampled := a.memory.inUse(now)
	if !sampled {
		return
	}
	var stats runtime.MemStats
	readMemStats(&stats)
	a.returnUnusedMemory(stats)
	threshold, crossed := a.memory.crossed(inUse)
	if !crossed {
		return
	}
	a.dumpHeapProfile(now)
	a.log.Warn("memory in use passed a threshold",
		"threshold_mib", threshold>>20,
		"heap_mib", stats.HeapAlloc>>20,
		"sys_mib", stats.Sys>>20,
		"objects", stats.HeapObjects,
		"collections", stats.NumGC,
		"repository", a.openedPath(),
		"running", a.runningTitleForLog())
}

func (a *App) dumpHeapProfile(now time.Time) {
	path := filepath.Join(filepath.Dir(a.paths.LogFile()), "heap-"+now.Format("20060102-150405")+".pprof")
	file, err := os.Create(path)
	if err != nil {
		a.log.Warn("heap profile could not be created", "path", path, "error", err)
		return
	}
	err = errors.Join(writeHeapProfile(file), file.Close())
	if err != nil {
		a.log.Warn("heap profile could not be written", "path", path, "error", err)
		return
	}
	a.log.Warn("heap profile written", "path", path)
}

func (a *App) returnUnusedMemory(stats runtime.MemStats) {
	unused := stats.HeapIdle - stats.HeapReleased
	idle := stats.HeapAlloc >= memoryIdleHeap && !a.operationRunning()
	if unused < memoryKeptUnused && !idle {
		return
	}
	if !a.memory.freeing.CompareAndSwap(false, true) {
		return
	}
	startFreeOSMemory(func() {
		defer a.memory.freeing.Store(false)
		freeOSMemory()
		a.log.Info("unused memory was given back to the system",
			"unused_mib", unused>>20, "heap_mib", stats.HeapAlloc>>20, "idle", idle)
	})
}

func (a *App) operationRunning() bool {
	title, _ := a.runningOperation()
	return title != ""
}

func (a *App) openedPath() string {
	if o := a.opened(); o != nil {
		return o.path
	}
	return ""
}

func (a *App) runningTitleForLog() string {
	title, _ := a.runningOperation()
	if title == "" {
		return "nothing"
	}
	return title
}
