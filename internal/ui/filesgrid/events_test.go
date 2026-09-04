package filesgrid

import (
	"image"
	"testing"

	"github.com/oops1/headless-gui/v3/engine"
	"github.com/oops1/headless-gui/v3/widget"
	"github.com/oops1/headless-gui/v3/widget/datagrid"

	"github.com/oops1/gogit/internal/ui/changes"
)

func newBoundRowGrid(t *testing.T, rows []changes.Row) (*engine.Engine, *Grid) {
	t.Helper()
	eng := engine.New(gridWidth, gridHeight, 30)
	t.Cleanup(eng.Stop)
	g := New()
	oc := datagrid.NewObservableCollection()
	for _, r := range rows {
		oc.Add(r)
	}
	g.SetItemsSource(oc)
	eng.SetRoot(g)
	eng.RenderOnce()
	return eng, g
}

func TestOnMouseMoveSetsTheToolTipToTheHoveredRowState(t *testing.T) {
	_, g := newBoundRowGrid(t, []changes.Row{
		{Name: "main.go", State: "Modified"},
		{Name: "new.go", State: "Added"},
	})
	rowY := DefaultHeaderHeight + DefaultRowHeight/2
	g.OnMouseMove(10, rowY)
	if got := g.GetToolTip(); got != "Modified" {
		t.Fatalf("ToolTip = %q, want Modified", got)
	}
	rowY = DefaultHeaderHeight + DefaultRowHeight + DefaultRowHeight/2
	g.OnMouseMove(10, rowY)
	if got := g.GetToolTip(); got != "Added" {
		t.Fatalf("ToolTip = %q, want Added", got)
	}
}

func TestOnMouseMoveClearsTheToolTipOverTheHeader(t *testing.T) {
	_, g := newBoundRowGrid(t, []changes.Row{{Name: "main.go", State: "Modified"}})
	g.OnMouseMove(10, DefaultHeaderHeight+2)
	g.SetToolTip("stale")
	g.OnMouseMove(10, 2)
	if got := g.GetToolTip(); got != "" {
		t.Fatalf("ToolTip = %q, want empty over the header", got)
	}
}

func TestOnMouseMoveClearsTheToolTipBelowTheLastRow(t *testing.T) {
	_, g := newBoundRowGrid(t, []changes.Row{{Name: "main.go", State: "Modified"}})
	g.SetToolTip("stale")
	g.OnMouseMove(10, gridHeight-1)
	if got := g.GetToolTip(); got != "" {
		t.Fatalf("ToolTip = %q, want empty below the last row", got)
	}
}

func TestOnMouseMoveClearsTheToolTipWithoutAnItemsSource(t *testing.T) {
	_, g := newBoundGrid(t, 0)
	g.SetToolTip("stale")
	g.OnMouseMove(10, DefaultHeaderHeight+2)
	if got := g.GetToolTip(); got != "" {
		t.Fatalf("ToolTip = %q, want empty without an items source", got)
	}
}

func TestOnMouseMoveClearsTheToolTipForNonRowItems(t *testing.T) {
	_, g := newBoundGrid(t, 3)
	g.SetToolTip("stale")
	g.OnMouseMove(10, DefaultHeaderHeight+2)
	if got := g.GetToolTip(); got != "" {
		t.Fatalf("ToolTip = %q, want empty for non-Row items", got)
	}
}

func TestRowAtReturnsFalseWhenRowHeightIsNotPositive(t *testing.T) {
	_, g := newBoundRowGrid(t, []changes.Row{{Name: "main.go", State: "Modified"}})
	g.Data().Grid.RowHeight = 0
	if _, ok := g.rowAt(DefaultHeaderHeight + 2); ok {
		t.Fatal("rowAt must return false when RowHeight is not positive")
	}
}

func TestOnMouseMoveWhileTheColumnMenuIsOpenDoesNotTouchTheToolTip(t *testing.T) {
	_, g := newBoundRowGrid(t, []changes.Row{{Name: "main.go", State: "Modified"}})
	g.openColumnMenu(image.Pt(10, 10))
	g.SetToolTip("stale")
	g.OnMouseMove(10, DefaultHeaderHeight+2)
	if got := g.GetToolTip(); got != "stale" {
		t.Fatalf("ToolTip = %q, want unchanged while the menu is open", got)
	}
}

func TestOnMouseMoveDuringAHeaderDragDoesNotTouchTheToolTip(t *testing.T) {
	_, g := newBoundRowGrid(t, []changes.Row{{Name: "main.go", State: "Modified"}})
	g.SetToolTip("stale")
	g.OnMouseButton(widget.MouseEvent{X: 10, Y: 10, Button: widget.MouseLeft, Pressed: true})
	g.OnMouseMove(10, DefaultHeaderHeight+2)
	if got := g.GetToolTip(); got != "stale" {
		t.Fatalf("ToolTip = %q, want unchanged during a header drag", got)
	}
}
