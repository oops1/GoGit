package settings

import (
	"image"

	"github.com/oops1/headless-gui/v3/widget"
)

type sectionScroller struct {
	*widget.ScrollView

	host   *widget.Grid
	height func() int
}

func newSectionScroller(host *widget.Grid, height func() int) *sectionScroller {
	s := &sectionScroller{ScrollView: widget.NewScrollView(), host: host, height: height}
	s.AddChild(host)
	return s
}

func (s *sectionScroller) SetBounds(r image.Rectangle) {
	s.ScrollView.SetBounds(r)
	s.syncContent()
}

func (s *sectionScroller) syncContent() {
	b := s.Bounds()
	content := s.height()
	s.ContentHeight = content
	width := b.Dx()
	if content > b.Dy() {
		width -= scrollbarWidth
	}
	if content < b.Dy() {
		content = b.Dy()
	}
	s.host.SetBounds(image.Rect(b.Min.X, b.Min.Y, b.Min.X+width, b.Min.Y+content))
	s.SetScrollY(s.ScrollY())
}

func gridPixelHeight(g *widget.Grid) int {
	total := 0.0
	for _, def := range g.RowDefs {
		if def.Mode == widget.GridSizePixel {
			total += def.Value
		}
	}
	return int(total)
}

func (v *View) attachScroll() {
	v.sectionArea.RemoveChild(v.sectionHost)
	v.scroll = newSectionScroller(v.sectionHost, v.sectionContentHeight)
	v.sectionArea.AddChild(v.scroll)
	v.sectionArea.SetBounds(v.sectionArea.Bounds())
}

func (v *View) sectionContentHeight() int {
	grid, ok := v.sectionWidgets()[v.section]
	if !ok {
		return 0
	}
	return gridPixelHeight(grid) + sectionBottomPadding
}

func (v *View) syncScroll() {
	v.scroll.syncContent()
}
