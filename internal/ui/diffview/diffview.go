package diffview

import (
	"sync"

	"github.com/oops1/headless-gui/v3/widget"
)

const (
	defaultRowHeight  = 18
	defaultFontSize   = 11.0
	defaultFontFamily = "Consolas"
	measureCacheMax   = 2048
	noSelection       = -1
)

type DiffView struct {
	widget.Base

	mu       sync.Mutex
	doc      Document
	mode     Mode
	set      rowSet
	scrollX  int
	scrollY  int
	selected int
	font     string
	size     float64
	rowH     int
	pal      Palette
	focused  bool

	dragging  bool
	dragHoriz bool
	dragFrom  int
	dragStart int
	capture   widget.CaptureManager

	measureMu   sync.Mutex
	measureRev  uint64
	measureSize float64
	measured    map[string]int

	OnLineClick func(hunk, line int)
}

type snapshot struct {
	doc      Document
	rows     []row
	mode     Mode
	maxRunes int
	digits   int
	scrollX  int
	scrollY  int
	selected int
	pal      Palette
	font     string
	size     float64
	rowH     int
}

func New() *DiffView {
	return &DiffView{
		mode:     SideBySide,
		selected: noSelection,
		font:     defaultFontFamily,
		size:     defaultFontSize,
		rowH:     defaultRowHeight,
		pal:      DarkPalette(),
		set:      rowSet{digits: 1},
	}
}

func (dv *DiffView) snapshot() snapshot {
	dv.mu.Lock()
	defer dv.mu.Unlock()
	return snapshot{
		doc:      dv.doc,
		rows:     dv.set.rows,
		mode:     dv.mode,
		maxRunes: dv.set.maxRunes,
		digits:   dv.set.digits,
		scrollX:  dv.scrollX,
		scrollY:  dv.scrollY,
		selected: dv.selected,
		pal:      dv.pal,
		font:     dv.font,
		size:     dv.size,
		rowH:     dv.rowH,
	}
}

func (dv *DiffView) SetDocument(doc Document) {
	dv.mu.Lock()
	dv.doc = doc
	dv.set = buildRows(doc, dv.mode)
	dv.scrollX, dv.scrollY = 0, 0
	dv.selected = noSelection
	dv.mu.Unlock()
	dv.Invalidate()
}

func (dv *DiffView) Document() Document {
	dv.mu.Lock()
	defer dv.mu.Unlock()
	return dv.doc
}

func (dv *DiffView) Clear() {
	dv.SetDocument(Document{})
}

func (dv *DiffView) SetMode(mode Mode) {
	dv.mu.Lock()
	if dv.mode == mode {
		dv.mu.Unlock()
		return
	}
	dv.mode = mode
	dv.set = buildRows(dv.doc, mode)
	dv.scrollX, dv.scrollY = 0, 0
	dv.selected = noSelection
	dv.mu.Unlock()
	dv.Invalidate()
}

func (dv *DiffView) Mode() Mode {
	dv.mu.Lock()
	defer dv.mu.Unlock()
	return dv.mode
}

func (dv *DiffView) SetPalette(p Palette) {
	dv.mu.Lock()
	dv.pal = p
	dv.mu.Unlock()
	dv.Invalidate()
}

func (dv *DiffView) Palette() Palette {
	dv.mu.Lock()
	defer dv.mu.Unlock()
	return dv.pal
}

func (dv *DiffView) SetFontFamily(name string) {
	dv.mu.Lock()
	dv.font = name
	dv.mu.Unlock()
	dv.Invalidate()
}

func (dv *DiffView) FontFamily() string {
	dv.mu.Lock()
	defer dv.mu.Unlock()
	return dv.font
}

func (dv *DiffView) SetFontSize(size float64) {
	dv.mu.Lock()
	dv.size = size
	dv.mu.Unlock()
	dv.Invalidate()
}

func (dv *DiffView) FontSize() float64 {
	dv.mu.Lock()
	defer dv.mu.Unlock()
	return dv.size
}

func (dv *DiffView) SetRowHeight(height int) {
	dv.mu.Lock()
	dv.rowH = height
	dv.mu.Unlock()
	dv.Invalidate()
}

func (dv *DiffView) RowHeight() int {
	dv.mu.Lock()
	defer dv.mu.Unlock()
	return dv.rowH
}

func (dv *DiffView) RowCount() int {
	dv.mu.Lock()
	defer dv.mu.Unlock()
	return len(dv.set.rows)
}

func (dv *DiffView) ScrollOffset() (int, int) {
	g := dv.layout(dv.snapshot())
	return g.scrollX, g.scrollY
}

func (dv *DiffView) ScrollBy(dx, dy int) {
	g := dv.layout(dv.snapshot())
	dv.scrollTo(g.scrollX+dx, g.scrollY+dy)
}

func (dv *DiffView) scrollTo(x, y int) {
	g := dv.layout(dv.snapshot())
	x = clampInt(x, 0, g.maxScrollX)
	y = clampInt(y, 0, g.maxScrollY)
	dv.mu.Lock()
	changed := dv.scrollX != x || dv.scrollY != y
	dv.scrollX, dv.scrollY = x, y
	dv.mu.Unlock()
	if changed {
		dv.Invalidate()
	}
}

func (dv *DiffView) Selected() (int, int, bool) {
	s := dv.snapshot()
	if s.selected < 0 || s.selected >= len(s.rows) {
		return 0, 0, false
	}
	r := s.rows[s.selected]
	return r.hunk, r.lineFor(false), true
}

func (dv *DiffView) selectRow(index int, rightSide bool) {
	s := dv.snapshot()
	if index < 0 || index >= len(s.rows) {
		return
	}
	dv.mu.Lock()
	changed := dv.selected != index
	dv.selected = index
	dv.mu.Unlock()
	if changed {
		dv.Invalidate()
	}
	if dv.OnLineClick != nil {
		r := s.rows[index]
		dv.OnLineClick(r.hunk, r.lineFor(rightSide))
	}
}

func (dv *DiffView) ApplyTheme(t *widget.Theme) {
	dv.SetPalette(PaletteFor(t))
}

func (dv *DiffView) SetFocused(focused bool) {
	dv.mu.Lock()
	dv.focused = focused
	dv.mu.Unlock()
}

func (dv *DiffView) IsFocused() bool {
	dv.mu.Lock()
	defer dv.mu.Unlock()
	return dv.focused
}

func (dv *DiffView) SetCaptureManager(cm widget.CaptureManager) {
	dv.mu.Lock()
	dv.capture = cm
	dv.mu.Unlock()
}

func (dv *DiffView) measure(text string, size float64) int {
	rev := widget.TextMetricsRev()
	dv.measureMu.Lock()
	if dv.measureRev != rev || dv.measureSize != size {
		dv.measured = nil
		dv.measureRev = rev
		dv.measureSize = size
	}
	if width, ok := dv.measured[text]; ok {
		dv.measureMu.Unlock()
		return width
	}
	dv.measureMu.Unlock()

	width := widget.MeasureUIText(text, size)

	dv.measureMu.Lock()
	if dv.measureRev == rev && dv.measureSize == size {
		if dv.measured == nil || len(dv.measured) >= measureCacheMax {
			dv.measured = make(map[string]int, 64)
		}
		dv.measured[text] = width
	}
	dv.measureMu.Unlock()
	return width
}

func (dv *DiffView) runePrefixWidth(text string, runes int, size float64) int {
	if runes <= 0 {
		return 0
	}
	count := 0
	for i := range text {
		if count == runes {
			return dv.measure(text[:i], size)
		}
		count++
	}
	return dv.measure(text, size)
}
