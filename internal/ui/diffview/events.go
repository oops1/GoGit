package diffview

import (
	"image"

	"github.com/oops1/headless-gui/v3/widget"
)

func (dv *DiffView) OnMouseButton(e widget.MouseEvent) bool {
	if !dv.IsEnabled() {
		return false
	}
	s := dv.snapshot()
	g := dv.layout(s)
	switch e.Button {
	case widget.MouseWheelUp:
		return dv.wheel(e, g, -1)
	case widget.MouseWheelDown:
		return dv.wheel(e, g, 1)
	case widget.MouseLeft:
		if e.Pressed {
			return dv.press(image.Pt(e.X, e.Y), g, s)
		}
		return dv.release()
	default:
		return false
	}
}

func (dv *DiffView) wheel(e widget.MouseEvent, g geometry, direction int) bool {
	if !e.Pressed {
		return true
	}
	dv.ScrollBy(0, direction*wheelRows*g.rowH)
	return true
}

func (dv *DiffView) press(pt image.Point, g geometry, s snapshot) bool {
	switch {
	case g.hasV && pt.In(g.vThumb()):
		dv.startDrag(false, pt.Y, g.scrollY)
		return true
	case g.hasV && pt.In(g.vBar):
		dv.scrollTo(g.scrollX, scrollForPoint(pt.Y-g.vBar.Min.Y, g.vBar.Dy(), g.viewH, g.contentH, g.maxScrollY))
		return true
	case g.hasH && pt.In(g.hThumb()):
		dv.startDrag(true, pt.X, g.scrollX)
		return true
	case g.hasH && pt.In(g.hBar):
		dv.scrollTo(scrollForPoint(pt.X-g.hBar.Min.X, g.hBar.Dx(), g.viewW, g.contentW, g.maxScrollX), g.scrollY)
		return true
	case pt.In(g.content):
		index := g.rowAt(pt.Y)
		if index < 0 || index >= len(s.rows) {
			return false
		}
		dv.selectRow(index, s.mode == SideBySide && pt.X >= g.split)
		return true
	default:
		return false
	}
}

func (dv *DiffView) startDrag(horizontal bool, from, start int) {
	dv.mu.Lock()
	dv.dragging = true
	dv.dragHoriz = horizontal
	dv.dragFrom = from
	dv.dragStart = start
	capture := dv.capture
	dv.mu.Unlock()
	if capture != nil {
		capture.SetCapture(dv)
	}
}

func (dv *DiffView) release() bool {
	dv.mu.Lock()
	dragging := dv.dragging
	dv.dragging = false
	capture := dv.capture
	dv.mu.Unlock()
	if !dragging {
		return false
	}
	if capture != nil {
		capture.ReleaseCapture()
	}
	return true
}

func (dv *DiffView) OnMouseMove(x, y int) {
	dv.mu.Lock()
	dragging := dv.dragging
	horizontal := dv.dragHoriz
	from := dv.dragFrom
	start := dv.dragStart
	dv.mu.Unlock()
	if !dragging {
		return
	}
	s := dv.snapshot()
	g := dv.layout(s)
	if horizontal {
		dv.scrollTo(start+dragDelta(x-from, g.hBar.Dx(), g.viewW, g.contentW, g.maxScrollX), g.scrollY)
		return
	}
	dv.scrollTo(g.scrollX, start+dragDelta(y-from, g.vBar.Dy(), g.viewH, g.contentH, g.maxScrollY))
}

func (dv *DiffView) WantsCapture(e widget.MouseEvent) bool {
	if e.Button != widget.MouseLeft || !e.Pressed {
		return false
	}
	g := dv.layout(dv.snapshot())
	pt := image.Pt(e.X, e.Y)
	return (g.hasV && pt.In(g.vThumb())) || (g.hasH && pt.In(g.hThumb()))
}

func (dv *DiffView) OnKeyEvent(e widget.KeyEvent) {
	if !e.Pressed || !dv.IsEnabled() {
		return
	}
	s := dv.snapshot()
	g := dv.layout(s)
	switch e.Code {
	case widget.KeyUp:
		dv.ScrollBy(0, -g.rowH)
	case widget.KeyDown:
		dv.ScrollBy(0, g.rowH)
	case widget.KeyPageUp:
		dv.ScrollBy(0, -g.content.Dy())
	case widget.KeyPageDown:
		dv.ScrollBy(0, g.content.Dy())
	case widget.KeyHome:
		dv.scrollTo(0, 0)
	case widget.KeyEnd:
		dv.scrollTo(g.scrollX, g.maxScrollY)
	case widget.KeyLeft:
		dv.ScrollBy(-horizontalStep, 0)
	case widget.KeyRight:
		dv.ScrollBy(horizontalStep, 0)
	}
}

func scrollForPoint(offset, track, view, content, maxScroll int) int {
	length := thumbLength(track, view, content)
	free := track - length
	if free <= 0 {
		return 0
	}
	return clampInt((offset-length/2)*maxScroll/free, 0, maxScroll)
}

func dragDelta(delta, track, view, content, maxScroll int) int {
	free := track - thumbLength(track, view, content)
	if free <= 0 {
		return 0
	}
	return delta * maxScroll / free
}
