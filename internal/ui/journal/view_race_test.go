package journal

import (
	"strconv"
	"sync"
	"testing"
)

func TestTheAuthorNameModeCanChangeWhileRowsArrive(t *testing.T) {
	v, grid := bound(t)
	grid.Grid.SetColumns(journalColumns())
	const rounds = 50

	var wg sync.WaitGroup
	wg.Go(func() {
		for round := range rounds {
			v.SetFullAuthorName(round%2 == 0)
		}
	})
	wg.Go(func() {
		for round := range rounds {
			v.Append([]Row{previewRow("race" + strconv.Itoa(round))})
		}
	})
	wg.Wait()

	if v.Count() != rounds {
		t.Fatalf("rows = %d, want %d", v.Count(), rounds)
	}
}
