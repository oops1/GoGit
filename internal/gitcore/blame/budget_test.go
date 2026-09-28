package blame

import (
	"runtime"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
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

const (
	blameBranches          = 300
	blameCommits           = 300
	blameLines             = 1000
	blameAllocationBudget  = 16 << 20
	blameLinearAllocBudget = 8 << 20
)

func blamedFile(t *testing.T, src Objects, head hash.ObjectID, path string) {
	t.Helper()
	if _, err := File(t.Context(), src, head, path, Options{}); err != nil {
		t.Fatal(err)
	}
}

func TestBlameOfAWideHistoryStaysWithinItsAllocationBudget(t *testing.T) {
	s, head := benchmarkWideHistory(t, blameBranches, blameLines)
	spent := allocatedBy(t, func() { blamedFile(t, s, head, "target.go") })
	t.Logf("a wide history of %d branches over %d lines allocated %d bytes", blameBranches, blameLines, spent)
	if spent > blameAllocationBudget {
		t.Errorf("blame allocated %d bytes, the budget is %d", spent, uint64(blameAllocationBudget))
	}
}

func TestBlameOfALongHistoryStaysWithinItsAllocationBudget(t *testing.T) {
	s, head := benchmarkHistory(t, blameCommits, blameLines)
	spent := allocatedBy(t, func() { blamedFile(t, s, head, "src/target.go") })
	t.Logf("a long history of %d commits over %d lines allocated %d bytes", blameCommits, blameLines, spent)
	if spent > blameLinearAllocBudget {
		t.Errorf("blame allocated %d bytes, the budget is %d", spent, uint64(blameLinearAllocBudget))
	}
}
