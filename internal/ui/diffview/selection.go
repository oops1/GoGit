package diffview

import (
	"image"

	"github.com/oops1/headless-gui/v3/widget"
)

type LineRef struct {
	Hunk int
	Line int
}

func (s snapshot) selectedRange() (int, int, bool) {
	if s.selected < 0 || s.selected >= len(s.rows) {
		return 0, 0, false
	}
	return min(s.anchor, s.selected), max(s.anchor, s.selected), true
}

func (s snapshot) inRange(index int) bool {
	lo, hi, ok := s.selectedRange()
	return ok && index >= lo && index <= hi
}

func (dv *DiffView) extendRow(index int) {
	dv.mu.Lock()
	if dv.anchor == noSelection {
		dv.anchor = index
	}
	dv.selected = index
	dv.mu.Unlock()
	dv.Invalidate()
}

func (dv *DiffView) SelectedLines() []LineRef {
	s := dv.snapshot()
	lo, hi, ok := s.selectedRange()
	if !ok {
		return nil
	}
	seen := map[LineRef]bool{}
	var out []LineRef
	add := func(hunk int, c cell) {
		ref := LineRef{Hunk: hunk, Line: c.line}
		if !c.filled || c.kind == NoNewline || seen[ref] {
			return
		}
		seen[ref] = true
		out = append(out, ref)
	}
	for _, r := range s.rows[lo : hi+1] {
		if r.header {
			continue
		}
		add(r.hunk, r.left)
		add(r.hunk, r.right)
	}
	return out
}

func (dv *DiffView) SelectedHunks() []int {
	s := dv.snapshot()
	lo, hi, ok := s.selectedRange()
	if !ok {
		return nil
	}
	var out []int
	for _, r := range s.rows[lo : hi+1] {
		if len(out) == 0 || out[len(out)-1] != r.hunk {
			out = append(out, r.hunk)
		}
	}
	return out
}

func (dv *DiffView) openMenu(pt image.Point, g geometry, s snapshot) bool {
	if dv.OnMenu == nil {
		return false
	}
	if index := g.rowAt(pt.Y); pt.In(g.content) && index >= 0 && index < len(s.rows) && !s.inRange(index) {
		dv.selectRow(index, s.mode == SideBySide && pt.X >= g.split)
	}
	items := dv.OnMenu()
	if len(items) == 0 {
		return false
	}
	dv.menu.SetItems(items)
	dv.menu.Show(pt.X, pt.Y)
	dv.Invalidate()
	return true
}

func (dv *DiffView) HasOverlay() bool { return dv.menu.IsOpen() }

func (dv *DiffView) DrawOverlay(ctx widget.DrawContext) { dv.menu.DrawOverlay(ctx) }

func (dv *DiffView) OverlayBounds() image.Rectangle { return dv.menu.OverlayBounds() }
