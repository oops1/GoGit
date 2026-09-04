package filesgrid

import (
	"image"
	"slices"
	"testing"

	"github.com/oops1/headless-gui/v3/engine"
	"github.com/oops1/headless-gui/v3/widget"
	"github.com/oops1/headless-gui/v3/widget/datagrid"
)

const (
	gridWidth  = 300
	gridHeight = 100
)

func newBoundGrid(t *testing.T, itemCount int) (*engine.Engine, *Grid) {
	t.Helper()
	eng := engine.New(gridWidth, gridHeight, 30)
	t.Cleanup(eng.Stop)
	g := New()
	if itemCount > 0 {
		oc := datagrid.NewObservableCollection()
		for i := range itemCount {
			oc.Add(i)
		}
		g.SetItemsSource(oc)
	}
	eng.SetRoot(g)
	eng.RenderOnce()
	return eng, g
}

func TestNewUsesCompactRowAndHeaderHeights(t *testing.T) {
	g := New()
	if g.Data().Grid.RowHeight != DefaultRowHeight {
		t.Fatalf("RowHeight = %d, want %d", g.Data().Grid.RowHeight, DefaultRowHeight)
	}
	if g.Data().Grid.HeaderHeight != DefaultHeaderHeight {
		t.Fatalf("HeaderHeight = %d, want %d", g.Data().Grid.HeaderHeight, DefaultHeaderHeight)
	}
	if g.Data().Grid.CanUserSortColumns {
		t.Fatal("sorting must be disabled: header clicks open the column menu instead")
	}
}

func TestNewDefaultsToNameStateRelDirVisible(t *testing.T) {
	g := New()
	order, visible := g.Columns()
	if len(order) != len(columnDefs) {
		t.Fatalf("len(order) = %d, want %d", len(order), len(columnDefs))
	}
	want := []ColumnID{ColName, ColState, ColRelDir}
	if len(visible) != len(want) {
		t.Fatalf("visible = %v, want %v", visible, want)
	}
	for i := range want {
		if visible[i] != want[i] {
			t.Fatalf("visible = %v, want %v", visible, want)
		}
	}
	if len(g.Data().Grid.Columns()) != len(want) {
		t.Fatalf("underlying grid has %d columns, want %d", len(g.Data().Grid.Columns()), len(want))
	}
}

func TestSetColumnsRebuildsTheUnderlyingGridColumns(t *testing.T) {
	g := New()
	g.SetColumns([]ColumnID{ColSize, ColName}, []ColumnID{ColSize})
	order, visible := g.Columns()
	if order[0] != ColSize || order[1] != ColName {
		t.Fatalf("order[:2] = %v, want [Size Name]", order[:2])
	}
	if len(visible) != 1 || visible[0] != ColSize {
		t.Fatalf("visible = %v, want [Size]", visible)
	}
	cols := g.Data().Grid.Columns()
	if len(cols) != 1 {
		t.Fatalf("underlying grid has %d columns, want 1", len(cols))
	}
}

func TestRetranslateRefreshesColumnHeaderText(t *testing.T) {
	widget.RegisterString("en", "Files.Column.Name", "Name-EN")
	widget.SetLanguage("en")
	t.Cleanup(widget.ClearStrings)

	g := New()
	g.Retranslate()
	cols := g.Data().Grid.Columns()
	if cols[0].Header() != "Name-EN" {
		t.Fatalf("header = %q, want Name-EN", cols[0].Header())
	}
}

func TestApplyThemeKeepsAlternatingRowBackgroundEqualToTheBaseBackground(t *testing.T) {
	g := New()
	g.ApplyTheme(widget.Win11DarkTheme())
	if g.Data().Grid.AlternateBG != g.Data().Grid.Background {
		t.Fatalf("AlternateBG = %v, want %v (no zebra striping)", g.Data().Grid.AlternateBG, g.Data().Grid.Background)
	}
}

func TestSetItemsSourceAndSelectionChangedForwardToTheInnerGrid(t *testing.T) {
	_, g := newBoundGrid(t, 5)
	var got datagrid.SelectionChangedEvent
	fired := false
	g.SetOnSelectionChanged(func(e datagrid.SelectionChangedEvent) { got, fired = e, true })
	rowY := DefaultHeaderHeight + 2*DefaultRowHeight + 2
	g.OnMouseButton(widget.MouseEvent{X: 10, Y: rowY, Button: widget.MouseLeft, Pressed: true})
	g.OnMouseButton(widget.MouseEvent{X: 10, Y: rowY, Button: widget.MouseLeft})
	if !fired || got.SelectedIndex != 2 {
		t.Fatalf("fired=%v event=%+v, want SelectedIndex=2", fired, got)
	}
}

func TestClickBelowTheHeaderSelectsARow(t *testing.T) {
	_, g := newBoundGrid(t, 5)
	got := -1
	g.SetOnSelectionChanged(func(e datagrid.SelectionChangedEvent) { got = e.SelectedIndex })
	g.OnMouseButton(widget.MouseEvent{X: 10, Y: 30, Button: widget.MouseLeft, Pressed: true})
	g.OnMouseButton(widget.MouseEvent{X: 10, Y: 30, Button: widget.MouseLeft})
	if got != 0 {
		t.Fatalf("SelectedIndex = %d, want 0", got)
	}
}

func TestHeaderClickWithoutMovementOpensTheColumnMenu(t *testing.T) {
	_, g := newBoundGrid(t, 0)
	if g.HasOverlay() {
		t.Fatal("menu must start closed")
	}
	if !g.OnMouseButton(widget.MouseEvent{X: 10, Y: 10, Button: widget.MouseLeft, Pressed: true}) {
		t.Fatal("header press must be consumed")
	}
	if !g.OnMouseButton(widget.MouseEvent{X: 10, Y: 10, Button: widget.MouseLeft}) {
		t.Fatal("header release must be consumed")
	}
	if !g.HasOverlay() {
		t.Fatal("clicking a header without moving must open the column menu")
	}
	if g.Bounds() == g.BaseBounds() {
		t.Fatal("Bounds must expand to include the open menu")
	}
}

func TestColumnMenuTogglesVisibilityAndKeepsAtLeastOneColumn(t *testing.T) {
	g := New()
	g.SetBounds(image.Rect(0, 0, gridWidth, gridHeight))
	g.openColumnMenu(image.Pt(10, 10))
	items := g.menu.Items()
	if len(items) != len(columnDefs) {
		t.Fatalf("menu has %d items, want %d", len(items), len(columnDefs))
	}
	stateItemIndex := slices.Index(DefaultOrder(), ColState)
	items[stateItemIndex].OnClick()
	_, visible := g.Columns()
	if slicesContain(visible, ColState) {
		t.Fatal("clicking a visible column's menu item must hide it")
	}

	g.toggleColumn(ColName)
	g.toggleColumn(ColRelDir)
	_, visible = g.Columns()
	if len(visible) != 1 || visible[0] != ColRelDir {
		t.Fatalf("visible = %v, want [RelDir] (at least one column must stay visible)", visible)
	}
}

func slicesContain(ids []ColumnID, id ColumnID) bool {
	for _, v := range ids {
		if v == id {
			return true
		}
	}
	return false
}

func TestHeaderDragReordersVisibleColumns(t *testing.T) {
	changed := false
	_, g := newBoundGrid(t, 0)
	g.OnColumnsChanged = func([]ColumnID, []ColumnID) { changed = true }

	g.OnMouseButton(widget.MouseEvent{X: 10, Y: 10, Button: widget.MouseLeft, Pressed: true})
	g.OnMouseMove(250, 10)
	g.OnMouseButton(widget.MouseEvent{X: 250, Y: 10, Button: widget.MouseLeft})

	if g.HasOverlay() {
		t.Fatal("a drag must not open the column menu")
	}
	if !changed {
		t.Fatal("OnColumnsChanged must fire after a reorder")
	}
	_, visible := g.Columns()
	want := []ColumnID{ColState, ColName, ColRelDir}
	if len(visible) != len(want) {
		t.Fatalf("visible = %v, want %v", visible, want)
	}
	for i := range want {
		if visible[i] != want[i] {
			t.Fatalf("visible = %v, want %v", visible, want)
		}
	}
}

func TestHeaderDragFromTheScrollbarDeadZoneDoesNothing(t *testing.T) {
	_, g := newBoundGrid(t, 40)
	before, _ := g.Columns()
	g.OnMouseButton(widget.MouseEvent{X: 295, Y: 10, Button: widget.MouseLeft, Pressed: true})
	g.OnMouseMove(50, 10)
	g.OnMouseButton(widget.MouseEvent{X: 50, Y: 10, Button: widget.MouseLeft})
	after, _ := g.Columns()
	if len(before) != len(after) {
		t.Fatalf("order changed unexpectedly: %v -> %v", before, after)
	}
	for i := range before {
		if before[i] != after[i] {
			t.Fatalf("order changed unexpectedly: %v -> %v", before, after)
		}
	}
}

func TestDisabledGridIgnoresHeaderClicks(t *testing.T) {
	_, g := newBoundGrid(t, 0)
	g.SetEnabled(false)
	if g.OnMouseButton(widget.MouseEvent{X: 10, Y: 10, Button: widget.MouseLeft, Pressed: true}) {
		t.Fatal("a disabled grid must not consume mouse input")
	}
	if g.HasOverlay() {
		t.Fatal("a disabled grid must not open the column menu")
	}
}

func TestRightClickOnAnOpenMenuIsConsumedWithoutClosingIt(t *testing.T) {
	_, g := newBoundGrid(t, 0)
	g.openColumnMenu(image.Pt(10, 10))
	if !g.OnMouseButton(widget.MouseEvent{X: 10, Y: 10, Button: widget.MouseRight}) {
		t.Fatal("right release over an open menu must be consumed")
	}
	if !g.HasOverlay() {
		t.Fatal("right release must not close the menu")
	}
}

func TestClickOutsideAnOpenMenuClosesItAndFallsThrough(t *testing.T) {
	eng := engine.New(gridWidth, 400, 30)
	t.Cleanup(eng.Stop)
	g := New()
	eng.SetRoot(g)
	eng.RenderOnce()
	g.openColumnMenu(image.Pt(10, 10))
	if !g.HasOverlay() {
		t.Fatal("menu must be open before the outside click")
	}
	g.OnMouseButton(widget.MouseEvent{X: 10, Y: 350, Button: widget.MouseLeft, Pressed: true})
	if g.HasOverlay() {
		t.Fatal("a click outside the menu must close it")
	}
}

func TestClickingAMenuItemThroughOnMouseButtonTogglesTheColumn(t *testing.T) {
	eng := engine.New(gridWidth, 400, 30)
	t.Cleanup(eng.Stop)
	g := New()
	eng.SetRoot(g)
	eng.RenderOnce()
	g.openColumnMenu(image.Pt(10, 10))

	g.OnMouseButton(widget.MouseEvent{X: 15, Y: 40, Button: widget.MouseLeft, Pressed: true})
	if !g.HasOverlay() {
		t.Fatal("pressing inside the menu must not close it")
	}
	g.OnMouseButton(widget.MouseEvent{X: 15, Y: 40, Button: widget.MouseLeft})
	if g.HasOverlay() {
		t.Fatal("choosing an item must close the menu")
	}
	_, visible := g.Columns()
	if slicesContain(visible, ColName) {
		t.Fatal("clicking the Name item inside the menu must hide the Name column")
	}
}

func TestNonLeftButtonWhileTheMenuIsClosedForwardsToTheInnerGrid(t *testing.T) {
	_, g := newBoundGrid(t, 40)
	if !g.OnMouseButton(widget.MouseEvent{X: 10, Y: 30, Button: widget.MouseWheelDown, Pressed: true}) {
		t.Fatal("wheel scrolling must be forwarded to the inner grid")
	}
}

func TestIdleMouseMoveForwardsToTheInnerGrid(t *testing.T) {
	_, g := newBoundGrid(t, 5)
	g.OnMouseMove(10, 50)
}

func TestReorderColumnIsANoOpWhenTheDropPositionMatchesTheOrigin(t *testing.T) {
	_, g := newBoundGrid(t, 0)
	before, _ := g.Columns()
	g.OnMouseButton(widget.MouseEvent{X: 100, Y: 10, Button: widget.MouseLeft, Pressed: true})
	g.OnMouseMove(110, 10)
	g.OnMouseButton(widget.MouseEvent{X: 100, Y: 10, Button: widget.MouseLeft})
	after, _ := g.Columns()
	if len(before) != len(after) {
		t.Fatalf("visible order changed unexpectedly: %v -> %v", before, after)
	}
	for i := range before {
		if before[i] != after[i] {
			t.Fatalf("visible order changed unexpectedly: %v -> %v", before, after)
		}
	}
}

func TestDismissClosesAnOpenMenu(t *testing.T) {
	g := New()
	g.SetBounds(image.Rect(0, 0, gridWidth, gridHeight))
	g.openColumnMenu(image.Pt(10, 10))
	if !g.HasOverlay() {
		t.Fatal("menu must be open before Dismiss")
	}
	g.Dismiss()
	if g.HasOverlay() {
		t.Fatal("Dismiss must close the menu")
	}
	g.Dismiss()
}

func TestOnKeyEventGoesToTheMenuWhileOpenThenToTheGrid(t *testing.T) {
	_, g := newBoundGrid(t, 5)
	g.openColumnMenu(image.Pt(10, 10))
	g.OnKeyEvent(widget.KeyEvent{Code: widget.KeyEscape, Pressed: true})
	if g.HasOverlay() {
		t.Fatal("Escape must close the open menu")
	}
	g.OnKeyEvent(widget.KeyEvent{Code: widget.KeyDown, Pressed: true})
}

func TestFocusIsTracked(t *testing.T) {
	g := New()
	if g.IsFocused() {
		t.Fatal("a new grid must not be focused")
	}
	g.SetFocused(true)
	if !g.IsFocused() {
		t.Fatal("SetFocused did not apply")
	}
}

type stubWidget struct {
	widget.Base
}

func (*stubWidget) Draw(widget.DrawContext) {}

func TestChildrenAndAddChildDelegateToTheInnerWidget(t *testing.T) {
	g := New()
	child := &stubWidget{}
	g.AddChild(child)
	found := false
	for _, c := range g.Children() {
		if c == widget.Widget(child) {
			found = true
		}
	}
	if !found {
		t.Fatal("AddChild/Children must delegate to the inner DataGridWidget")
	}
}

type fakeCapture struct {
	captured int
	released int
}

func (c *fakeCapture) SetCapture(widget.Widget) { c.captured++ }

func (c *fakeCapture) ReleaseCapture() { c.released++ }

func TestSetCaptureManagerIsUsedDuringAHeaderPress(t *testing.T) {
	g := New()
	g.SetBounds(image.Rect(0, 0, gridWidth, gridHeight))
	fc := &fakeCapture{}
	g.SetCaptureManager(fc)
	g.OnMouseButton(widget.MouseEvent{X: 10, Y: 10, Button: widget.MouseLeft, Pressed: true})
	g.OnMouseButton(widget.MouseEvent{X: 10, Y: 10, Button: widget.MouseLeft})
	if fc.captured != 1 || fc.released != 1 {
		t.Fatalf("captured=%d released=%d, want 1 and 1", fc.captured, fc.released)
	}
}

func TestIsEnabledDelegatesToTheInnerGrid(t *testing.T) {
	g := New()
	if !g.IsEnabled() {
		t.Fatal("a new grid must be enabled")
	}
	g.SetEnabled(false)
	if g.IsEnabled() {
		t.Fatal("SetEnabled(false) did not apply")
	}
}

func TestOverlayBoundsIsEmptyUnlessTheMenuIsOpen(t *testing.T) {
	g := New()
	g.SetBounds(image.Rect(0, 0, gridWidth, gridHeight))
	if !g.OverlayBounds().Empty() {
		t.Fatal("OverlayBounds must be empty while the menu is closed")
	}
	g.openColumnMenu(image.Pt(10, 10))
	if g.OverlayBounds().Empty() {
		t.Fatal("OverlayBounds must be non-empty while the menu is open")
	}
}

func TestOnMouseMoveForwardsToTheMenuWhileItIsOpen(t *testing.T) {
	_, g := newBoundGrid(t, 0)
	g.openColumnMenu(image.Pt(10, 10))
	g.OnMouseMove(15, 30)
	if !g.HasOverlay() {
		t.Fatal("moving over an open menu must not close it")
	}
}

func TestSmallHeaderMovementBelowTheThresholdStillCountsAsAClick(t *testing.T) {
	_, g := newBoundGrid(t, 0)
	g.OnMouseButton(widget.MouseEvent{X: 10, Y: 10, Button: widget.MouseLeft, Pressed: true})
	g.OnMouseMove(11, 10)
	g.OnMouseButton(widget.MouseEvent{X: 11, Y: 10, Button: widget.MouseLeft})
	if !g.HasOverlay() {
		t.Fatal("a movement within the drag threshold must still open the column menu")
	}
}

func TestTargetIndexForXFallsBackToTheLastPositionPastAllColumns(t *testing.T) {
	_, g := newBoundGrid(t, 40)
	_, before := g.Columns()
	g.OnMouseButton(widget.MouseEvent{X: 10, Y: 10, Button: widget.MouseLeft, Pressed: true})
	g.OnMouseMove(295, 10)
	g.OnMouseButton(widget.MouseEvent{X: 295, Y: 10, Button: widget.MouseLeft})
	_, after := g.Columns()
	if after[len(after)-1] != before[0] {
		t.Fatalf("dragging past every column must move it to the end: before=%v after=%v", before, after)
	}
}

func TestNeedsAnimationDelegatesToTheInnerGrid(t *testing.T) {
	g := New()
	if g.NeedsAnimation() {
		t.Fatal("a fresh grid must not need animation")
	}
}

func TestDrawDoesNotPanic(t *testing.T) {
	_, g := newBoundGrid(t, 3)
	g.openColumnMenu(image.Pt(10, 10))
	eng := engine.New(gridWidth, gridHeight, 30)
	t.Cleanup(eng.Stop)
	eng.SetRoot(g)
	eng.RenderOnce()
}

func TestGridRefusesCellEditing(t *testing.T) {
	g := New()
	if !g.Data().Grid.IsReadOnly {
		t.Fatal("the files grid must not let a click open a cell editor")
	}
}
