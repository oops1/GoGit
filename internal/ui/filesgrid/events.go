package filesgrid

import (
	"image"
	"slices"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/i18n"
)

const checkedPrefix = "✓ "

func (g *Grid) OnMouseButton(e widget.MouseEvent) bool {
	if !g.dg.IsEnabled() {
		return false
	}
	if g.menu.IsOpen() {
		if e.Button == widget.MouseRight && !e.Pressed {
			return true
		}
		pr := g.menu.Bounds()
		if image.Pt(e.X, e.Y).In(pr) {
			return g.menu.OnMouseButton(e)
		}
		g.menu.Close()
	}
	if e.Button != widget.MouseLeft {
		return g.dg.OnMouseButton(e)
	}
	pt := image.Pt(e.X, e.Y)
	if e.Pressed {
		if pt.In(g.headerRect()) {
			g.beginHeaderPress(pt)
			return true
		}
		return g.dg.OnMouseButton(e)
	}
	if g.headerPressActive() {
		return g.finishHeaderPress(pt)
	}
	return g.dg.OnMouseButton(e)
}

func (g *Grid) OnMouseMove(x, y int) {
	if g.menu.IsOpen() {
		g.menu.OnMouseMove(x, y)
		return
	}
	if g.headerPressActive() {
		g.updateHeaderPress(x)
		return
	}
	g.dg.OnMouseMove(x, y)
}

func (g *Grid) OnKeyEvent(e widget.KeyEvent) {
	if g.menu.IsOpen() {
		g.menu.OnKeyEvent(e)
		return
	}
	g.dg.OnKeyEvent(e)
}

func (g *Grid) headerRect() image.Rectangle {
	b := g.dg.Bounds()
	return image.Rect(b.Min.X, b.Min.Y, b.Max.X, b.Min.Y+g.dg.Grid.HeaderHeight)
}

func (g *Grid) headerPressActive() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.press.active
}

func (g *Grid) beginHeaderPress(pt image.Point) {
	col := g.columnIndexForX(pt.X)
	g.mu.Lock()
	g.press = pressState{active: true, startX: pt.X, colIdx: col}
	capture := g.capture
	g.mu.Unlock()
	if capture != nil {
		capture.SetCapture(g)
	}
}

func (g *Grid) updateHeaderPress(x int) {
	g.mu.Lock()
	if abs(x-g.press.startX) > dragThreshold {
		g.press.moved = true
	}
	g.mu.Unlock()
}

func (g *Grid) finishHeaderPress(pt image.Point) bool {
	g.mu.Lock()
	press := g.press
	g.press = pressState{colIdx: -1}
	capture := g.capture
	g.mu.Unlock()
	if capture != nil {
		capture.ReleaseCapture()
	}
	if !press.moved {
		g.openColumnMenu(pt)
		return true
	}
	if press.colIdx >= 0 {
		target := g.targetIndexForX(pt.X)
		g.reorderColumn(press.colIdx, target)
	}
	return true
}

func (g *Grid) columnIndexForX(x int) int {
	cols := g.dg.Grid.Columns()
	left := g.dg.Bounds().Min.X
	for i, c := range cols {
		w := c.ActualWidth()
		if x >= left && x < left+w {
			return i
		}
		left += w
	}
	return -1
}

func (g *Grid) targetIndexForX(x int) int {
	cols := g.dg.Grid.Columns()
	left := g.dg.Bounds().Min.X
	for i, c := range cols {
		w := c.ActualWidth()
		mid := left + w/2
		if x < mid {
			return i
		}
		left += w
	}
	return len(cols)
}

func (g *Grid) visibleOrder() []ColumnID {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := make([]ColumnID, 0, len(g.order))
	for _, id := range g.order {
		if g.visible[id] {
			out = append(out, id)
		}
	}
	return out
}

func (g *Grid) reorderColumn(fromIdx, toIdx int) {
	visibleIDs := g.visibleOrder()
	dragID := visibleIDs[fromIdx]
	newVisible := slices.Delete(slices.Clone(visibleIDs), fromIdx, fromIdx+1)
	insertAt := toIdx
	if insertAt > fromIdx {
		insertAt--
	}
	newVisible = slices.Insert(newVisible, insertAt, dragID)
	if slices.Equal(newVisible, visibleIDs) {
		return
	}

	g.mu.Lock()
	g.order = mergeVisibleOrder(g.order, newVisible, g.visible)
	g.mu.Unlock()
	g.rebuildColumns()
	g.notifyColumnsChanged()
}

func mergeVisibleOrder(oldOrder, newVisibleSeq []ColumnID, visible map[ColumnID]bool) []ColumnID {
	result := make([]ColumnID, 0, len(oldOrder))
	vi := 0
	for _, id := range oldOrder {
		if !visible[id] {
			result = append(result, id)
			continue
		}
		result = append(result, newVisibleSeq[vi])
		vi++
	}
	return result
}

func (g *Grid) openColumnMenu(pt image.Point) {
	g.mu.Lock()
	order := slices.Clone(g.order)
	visible := g.visible
	g.mu.Unlock()

	items := make([]widget.MenuItem, 0, len(order))
	for _, id := range order {
		def, _ := columnByID(id)
		colID := id
		label := i18n.T(def.key)
		if visible[colID] {
			label = checkedPrefix + label
		}
		items = append(items, widget.MenuItem{
			Text:    label,
			OnClick: func() { g.toggleColumn(colID) },
		})
	}
	g.menu.SetItems(items)
	hr := g.headerRect()
	g.menu.Show(pt.X, hr.Max.Y)
}

func (g *Grid) toggleColumn(id ColumnID) {
	g.mu.Lock()
	nowVisible := !g.visible[id]
	visibleCount := 0
	for _, v := range g.visible {
		if v {
			visibleCount++
		}
	}
	if !nowVisible && visibleCount <= 1 {
		g.mu.Unlock()
		return
	}
	g.visible[id] = nowVisible
	g.mu.Unlock()
	g.rebuildColumns()
	g.notifyColumnsChanged()
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
