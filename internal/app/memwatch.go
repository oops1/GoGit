package app

import (
	"runtime"
	"runtime/metrics"
	"sync"
	"time"
)

const (
	memoryFirstReport = 512 << 20
	memoryStepFactor  = 2
	memorySampleEvery = 30 * time.Second

	heapInUseMetric = "/memory/classes/heap/objects:bytes"
)

type memoryWatch struct {
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
	readHeapInUse = heapInUseOf
	readMemStats  = runtime.ReadMemStats
)

func (a *App) reportMemoryGrowth(now time.Time) {
	inUse, sampled := a.memory.inUse(now)
	if !sampled {
		return
	}
	threshold, crossed := a.memory.crossed(inUse)
	if !crossed {
		return
	}
	var stats runtime.MemStats
	readMemStats(&stats)
	a.log.Warn("memory in use passed a threshold",
		"threshold_mib", threshold>>20,
		"heap_mib", stats.HeapAlloc>>20,
		"sys_mib", stats.Sys>>20,
		"objects", stats.HeapObjects,
		"collections", stats.NumGC,
		"repository", a.openedPath(),
		"running", a.runningTitleForLog())
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
