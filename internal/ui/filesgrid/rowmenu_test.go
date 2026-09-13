package filesgrid

import (
	"slices"
	"testing"

	"github.com/oops1/headless-gui/v3/widget"
)

func openRowMenu(t *testing.T, g *Grid, x, y int) *widget.PopupMenu {
	t.Helper()
	menu := g.ContextMenuAt(x, y)
	if menu == nil {
		t.Fatal("the row under the cursor offers no menu")
	}
	menu.Show(x, y)
	return menu
}

func TestTheRowMenuComesFromTheDataGridAndOwnsTheInput(t *testing.T) {
	eng, g := newBoundGrid(t, 3)
	var asked []int
	g.Data().RowContextMenu = func(_ any, row int) []widget.MenuItem {
		asked = append(asked, row)
		return []widget.MenuItem{{Text: "Stage"}}
	}
	x, y := 20, DefaultHeaderHeight+DefaultRowHeight/2

	openRowMenu(t, g, x, y)
	if !slices.Equal(asked, []int{0}) || !g.HasOverlay() || g.OverlayBounds().Empty() {
		t.Fatalf("asked rows %v, overlay %v", asked, g.HasOverlay())
	}
	eng.RenderOnce()
	g.OnMouseMove(x, y)
	if !g.OnMouseButton(widget.MouseEvent{X: x, Y: y, Button: widget.MouseRight}) {
		t.Fatal("the release that opened the menu must stay with the menu")
	}
	g.OnMouseButton(widget.MouseEvent{X: gridWidth - 1, Y: gridHeight - 1, Button: widget.MouseLeft, Pressed: true})
	if g.HasOverlay() {
		t.Fatal("a click outside must close the row menu")
	}

	openRowMenu(t, g, x, y)
	g.Dismiss()
	if g.HasOverlay() {
		t.Fatal("Dismiss must close the row menu")
	}
}

func TestNoRowMenuIsOfferedWithoutItems(t *testing.T) {
	_, g := newBoundGrid(t, 1)
	g.Data().RowContextMenu = func(any, int) []widget.MenuItem { return nil }

	if menu := g.ContextMenuAt(20, DefaultHeaderHeight+DefaultRowHeight/2); menu != nil || g.HasOverlay() {
		t.Fatal("an empty item list must not open a menu")
	}
}
