package journal

import (
	"image"
	"testing"

	"github.com/oops1/headless-gui/v3/widget"
	"github.com/oops1/headless-gui/v3/widget/datagrid"

	"github.com/oops1/gogit/internal/gitcore/hash"
)

const (
	gridWidth  = 800
	gridHeight = 400
)

func bound(t *testing.T) (*View, *widget.DataGridWidget) {
	t.Helper()
	v := NewView()
	grid := widget.NewDataGridWidget()
	grid.SetBounds(image.Rect(0, 0, gridWidth, gridHeight))
	v.Bind(grid)
	return v, grid
}

func clickRow(grid *widget.DataGridWidget, row int) {
	b := grid.Grid.Bounds()
	x := b.Min.X + 10
	y := b.Min.Y + grid.Grid.HeaderHeight + row*grid.Grid.RowHeight + grid.Grid.RowHeight/2
	grid.Grid.OnMouseButton(x, y, 0, true)
}

func idFor(n byte) hash.ObjectID {
	var id hash.ObjectID
	id[0] = n
	return id
}

func sampleRows(n int) []Row {
	rows := make([]Row, n)
	for i := range n {
		rows[i] = Row{
			Graph:     "*",
			Message:   "commit",
			Author:    "ann",
			Date:      "2026-09-03 12:00",
			ShortHash: "abcdef0",
			ID:        idFor(byte(i + 1)),
		}
	}
	return rows
}

func TestBindSetsTheViewCollectionAsItemsSource(t *testing.T) {
	v, grid := bound(t)
	if grid.Grid.ItemsSource() == nil {
		t.Fatal("ItemsSource() = nil, want the view's collection")
	}
	if grid.Grid.ItemsSource().Count() != 0 {
		t.Fatalf("ItemsSource().Count() = %d, want 0", grid.Grid.ItemsSource().Count())
	}
	if v.Count() != 0 {
		t.Fatalf("Count() = %d, want 0", v.Count())
	}
}

func TestAppendAddsRowsToTheGrid(t *testing.T) {
	v, grid := bound(t)
	rows := sampleRows(3)

	v.Append(rows)

	if v.Count() != 3 {
		t.Fatalf("Count() = %d, want 3", v.Count())
	}
	if grid.Grid.ItemsSource().Count() != 3 {
		t.Fatalf("ItemsSource().Count() = %d, want 3", grid.Grid.ItemsSource().Count())
	}
	for i, want := range rows {
		got, ok := grid.Grid.ItemsSource().Get(i).(Row)
		if !ok {
			t.Fatalf("item %d is not a Row: %v", i, grid.Grid.ItemsSource().Get(i))
		}
		if got.ID != want.ID {
			t.Fatalf("item %d ID = %s, want %s", i, got.ID, want.ID)
		}
	}
}

func TestAppendAccumulatesAcrossCalls(t *testing.T) {
	v, _ := bound(t)
	v.Append(sampleRows(2))
	v.Append(sampleRows(3))

	if v.Count() != 5 {
		t.Fatalf("Count() = %d, want 5", v.Count())
	}
}

func TestResetClearsPreviouslyAppendedRows(t *testing.T) {
	v, grid := bound(t)
	v.Append(sampleRows(4))

	v.Reset()

	if v.Count() != 0 {
		t.Fatalf("Count() = %d, want 0", v.Count())
	}
	if grid.Grid.ItemsSource().Count() != 0 {
		t.Fatalf("ItemsSource().Count() = %d, want 0", grid.Grid.ItemsSource().Count())
	}
}

func TestOnSelectFiresWithTheClickedRow(t *testing.T) {
	v, grid := bound(t)
	rows := sampleRows(3)
	v.Append(rows)

	var got Row
	called := false
	v.OnSelect = func(row Row) { got, called = row, true }

	clickRow(grid, 1)

	if !called {
		t.Fatal("OnSelect was not called")
	}
	if got.ID != rows[1].ID {
		t.Fatalf("OnSelect row ID = %s, want %s", got.ID, rows[1].ID)
	}
}

func TestOnSelectNotSetDoesNotPanicOnClick(t *testing.T) {
	v, grid := bound(t)
	v.Append(sampleRows(2))
	clickRow(grid, 0)
}

func TestOnSelectIgnoresNonRowSelectedItems(t *testing.T) {
	v, grid := bound(t)
	called := false
	v.OnSelect = func(Row) { called = true }

	grid.Grid.OnSelectionChanged(datagrid.SelectionChangedEvent{SelectedIndex: 0, SelectedItem: "not-a-row"})

	if called {
		t.Fatal("OnSelect fired for a non-Row selected item")
	}
}

func TestOnNearEndFiresWhenSelectionReachesTheLastRows(t *testing.T) {
	v, grid := bound(t)
	v.Append(sampleRows(10))

	called := false
	v.OnNearEnd = func() { called = true }

	clickRow(grid, 9)

	if !called {
		t.Fatal("OnNearEnd was not called when the last row was selected")
	}
}

func TestOnNearEndDoesNotFireWhenSelectionIsFarFromTheEnd(t *testing.T) {
	v, grid := bound(t)
	v.Append(sampleRows(20))

	called := false
	v.OnNearEnd = func() { called = true }

	clickRow(grid, 0)

	if called {
		t.Fatal("OnNearEnd fired for a selection far from the end")
	}
}

func TestOnNearEndNotSetDoesNotPanicOnClick(t *testing.T) {
	v, grid := bound(t)
	v.Append(sampleRows(3))
	clickRow(grid, 2)
}

func TestEnsureLoadedFiresOnNearEndWhenBelowTheRequestedCount(t *testing.T) {
	v, _ := bound(t)
	v.Append(sampleRows(2))

	called := false
	v.OnNearEnd = func() { called = true }

	v.EnsureLoaded(5)

	if !called {
		t.Fatal("EnsureLoaded did not call OnNearEnd although fewer rows are loaded")
	}
}

func TestEnsureLoadedDoesNotFireWhenEnoughRowsAreLoaded(t *testing.T) {
	v, _ := bound(t)
	v.Append(sampleRows(5))

	called := false
	v.OnNearEnd = func() { called = true }

	v.EnsureLoaded(5)

	if called {
		t.Fatal("EnsureLoaded called OnNearEnd although enough rows are loaded")
	}
}

func TestEnsureLoadedNotSetDoesNotPanic(t *testing.T) {
	v, _ := bound(t)
	v.EnsureLoaded(5)
}
