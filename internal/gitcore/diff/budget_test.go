package diff

import (
	"runtime"
	"testing"
)

func allocatedBy(t *testing.T, work func()) uint64 {
	t.Helper()
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	work()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

const blobsAllocationBudget = 8 << 20

func TestBlobsOfTenThousandLinesStaysWithinItsAllocationBudget(t *testing.T) {
	old, updated := benchmarkPair(10000)
	opts := Defaults()
	for _, c := range []struct {
		name      string
		algorithm Algorithm
	}{
		{"myers", AlgorithmMyers},
		{"histogram", AlgorithmHistogram},
		{"patience", AlgorithmPatience},
	} {
		opts.Algorithm = c.algorithm
		spent := allocatedBy(t, func() { Blobs(old, updated, opts) })
		if spent > blobsAllocationBudget {
			t.Errorf("%s allocated %d bytes, the budget is %d", c.name, spent, uint64(blobsAllocationBudget))
		}
		t.Logf("%s allocated %d bytes", c.name, spent)
	}
}
