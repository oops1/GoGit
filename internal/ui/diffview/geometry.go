package diffview

import "image"

const (
	scrollbarSize  = 10
	minThumbSize   = 24
	numberPad      = 5
	textPad        = 6
	horizontalStep = 32
	wheelRows      = 3
)

type geometry struct {
	bounds  image.Rectangle
	clip    image.Rectangle
	content image.Rectangle
	vBar    image.Rectangle
	hBar    image.Rectangle
	hasV    bool
	hasH    bool

	mode  Mode
	rowH  int
	numW  int
	split int

	contentH   int
	contentW   int
	viewH      int
	viewW      int
	maxScrollX int
	maxScrollY int
	scrollX    int
	scrollY    int
}

func (dv *DiffView) layout(s snapshot) geometry {
	b := dv.Bounds()
	charW := max(dv.measure("0", s.size), 1)
	g := geometry{
		bounds:   b,
		clip:     b,
		mode:     s.mode,
		rowH:     s.rowH,
		numW:     s.digits*charW + 2*numberPad,
		contentH: len(s.rows) * s.rowH,
		contentW: s.maxRunes*charW + 2*textPad,
	}
	for range 2 {
		w := b.Dx()
		h := b.Dy()
		if g.hasV {
			w -= scrollbarSize
		}
		if g.hasH {
			h -= scrollbarSize
		}
		hasV := g.contentH > max(h, 0)
		hasH := g.contentW > textColumnWidth(s.mode, max(w, 0), g.numW)
		if hasV == g.hasV && hasH == g.hasH {
			break
		}
		g.hasV, g.hasH = hasV, hasH
	}
	right := b.Max.X
	if g.hasV {
		right -= scrollbarSize
	}
	bottom := b.Max.Y
	if g.hasH {
		bottom -= scrollbarSize
	}
	g.content = image.Rect(b.Min.X, b.Min.Y, max(right, b.Min.X), max(bottom, b.Min.Y))
	g.vBar = image.Rect(max(right, b.Min.X), b.Min.Y, b.Max.X, max(bottom, b.Min.Y))
	g.hBar = image.Rect(b.Min.X, max(bottom, b.Min.Y), max(right, b.Min.X), b.Max.Y)
	g.split = g.content.Min.X + g.content.Dx()/2
	g.viewH = g.content.Dy()
	g.viewW = textColumnWidth(s.mode, g.content.Dx(), g.numW)
	g.maxScrollY = max(g.contentH-g.viewH, 0)
	g.maxScrollX = max(g.contentW-g.viewW, 0)
	g.scrollX = clampInt(s.scrollX, 0, g.maxScrollX)
	g.scrollY = clampInt(s.scrollY, 0, g.maxScrollY)
	return g
}

func textColumnWidth(mode Mode, width, numW int) int {
	if mode == Unified {
		return max(width-2*numW, 0)
	}
	return max(width/2-numW, 0)
}

func (g geometry) leftNumbers() image.Rectangle {
	return image.Rect(g.content.Min.X, g.content.Min.Y, min(g.content.Min.X+g.numW, g.content.Max.X), g.content.Max.Y)
}

func (g geometry) leftText() image.Rectangle {
	return image.Rect(g.leftNumbers().Max.X, g.content.Min.Y, max(g.split, g.leftNumbers().Max.X), g.content.Max.Y)
}

func (g geometry) rightNumbers() image.Rectangle {
	return image.Rect(g.split, g.content.Min.Y, min(g.split+g.numW, g.content.Max.X), g.content.Max.Y)
}

func (g geometry) rightText() image.Rectangle {
	return image.Rect(g.rightNumbers().Max.X, g.content.Min.Y, max(g.content.Max.X, g.rightNumbers().Max.X), g.content.Max.Y)
}

func (g geometry) oldNumbers() image.Rectangle {
	return image.Rect(g.content.Min.X, g.content.Min.Y, min(g.content.Min.X+g.numW, g.content.Max.X), g.content.Max.Y)
}

func (g geometry) newNumbers() image.Rectangle {
	start := g.oldNumbers().Max.X
	return image.Rect(start, g.content.Min.Y, min(start+g.numW, g.content.Max.X), g.content.Max.Y)
}

func (g geometry) unifiedText() image.Rectangle {
	start := g.newNumbers().Max.X
	return image.Rect(start, g.content.Min.Y, max(g.content.Max.X, start), g.content.Max.Y)
}

func (g geometry) rowY(index int) int {
	return g.content.Min.Y + index*g.rowH - g.scrollY
}

func (g geometry) visibleRows(total int) (int, int) {
	if g.rowH <= 0 || total == 0 || g.content.Dy() <= 0 {
		return 0, 0
	}
	first := clampInt(g.scrollY/g.rowH, 0, total)
	last := clampInt((g.scrollY+g.content.Dy())/g.rowH+1, 0, total)
	return first, last
}

func (g geometry) rowAt(y int) int {
	if g.rowH <= 0 {
		return -1
	}
	index := (y - g.content.Min.Y + g.scrollY) / g.rowH
	if index < 0 {
		return -1
	}
	return index
}

func (g geometry) vThumb() image.Rectangle {
	if !g.hasV {
		return image.Rectangle{}
	}
	offset := thumbOffset(g.vBar.Dy(), g.viewH, g.contentH, g.scrollY, g.maxScrollY)
	return image.Rect(g.vBar.Min.X, g.vBar.Min.Y+offset,
		g.vBar.Max.X, g.vBar.Min.Y+offset+thumbLength(g.vBar.Dy(), g.viewH, g.contentH))
}

func (g geometry) hThumb() image.Rectangle {
	if !g.hasH {
		return image.Rectangle{}
	}
	offset := thumbOffset(g.hBar.Dx(), g.viewW, g.contentW, g.scrollX, g.maxScrollX)
	return image.Rect(g.hBar.Min.X+offset, g.hBar.Min.Y,
		g.hBar.Min.X+offset+thumbLength(g.hBar.Dx(), g.viewW, g.contentW), g.hBar.Max.Y)
}

func thumbLength(track, view, content int) int {
	if content <= 0 {
		return track
	}
	return clampInt(track*view/content, min(minThumbSize, track), track)
}

func thumbOffset(track, view, content, scroll, maxScroll int) int {
	if maxScroll <= 0 {
		return 0
	}
	free := track - thumbLength(track, view, content)
	return clampInt(scroll*free/maxScroll, 0, max(free, 0))
}

func (g geometry) clipped(r image.Rectangle) image.Rectangle {
	return r.Intersect(g.clip)
}

func clampInt(v, lo, hi int) int {
	return min(max(v, lo), hi)
}
