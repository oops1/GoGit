package revision

import (
	"runtime"
	"testing"
)

const walkAllocationBudget = 32 << 20

func allocatedBy(t *testing.T, work func()) uint64 {
	t.Helper()
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	work()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

func TestWalkOfTenThousandCommitsStaysWithinItsAllocationBudget(t *testing.T) {
	built, tip := benchmarkHistory(t)
	opts := built.options(tip)
	spent := allocatedBy(t, func() {
		count := 0
		for _, err := range Walk(t.Context(), opts) {
			if err != nil {
				t.Fatal(err)
			}
			count++
		}
		if count != benchmarkCommits {
			t.Fatalf("the walk saw %d commits instead of %d", count, benchmarkCommits)
		}
	})
	t.Logf("walking %d commits allocated %d bytes", benchmarkCommits, spent)
	if spent > walkAllocationBudget {
		t.Errorf("the walk allocated %d bytes, the budget is %d", spent, uint64(walkAllocationBudget))
	}
}
