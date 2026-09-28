package app

import (
	"runtime"
	"sync"
)

const (
	memoryFirstReport = 512 << 20
	memoryStepFactor  = 2
)

type memoryWatch struct {
	mu   sync.Mutex
	next uint64
}

func newMemoryWatch() *memoryWatch {
	return &memoryWatch{next: memoryFirstReport}
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

var readMemStats = runtime.ReadMemStats

func (a *App) reportMemoryGrowth() {
	var stats runtime.MemStats
	readMemStats(&stats)
	threshold, crossed := a.memory.crossed(stats.HeapAlloc)
	if !crossed {
		return
	}
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
