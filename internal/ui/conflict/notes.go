package conflict

import (
	"image"
	"image/color"
	"sync"

	"github.com/oops1/headless-gui/v3/widget"
)

const (
	mergeOuterPad     = 14
	mergeRulerWidth   = 12
	mergeRulerGap     = 6
	mergeGutter       = 10
	mergeHeaderTop    = 4
	mergeHeaderHeight = 34
	mergeCardGap      = 12
	basePane          = 1
	notePadX          = 8
	notePadY          = 1
	noteFontSize      = 8.0
)

type paneNotes struct {
	widget.Base

	merge *widget.MergeView
	mu    sync.Mutex
	texts [3]string
	color color.RGBA
}

func newPaneNotes(merge *widget.MergeView, ours, base, theirs string) *paneNotes {
	return &paneNotes{
		merge: merge,
		texts: [3]string{ours, base, theirs},
		color: widget.CurrentTheme().SecondaryText,
	}
}

func (n *paneNotes) Bounds() image.Rectangle {
	b := n.merge.Bounds()
	if b.Empty() {
		return image.Rectangle{}
	}
	top := b.Min.Y + mergeOuterPad + mergeHeaderTop + mergeHeaderHeight
	return image.Rect(b.Min.X+mergeOuterPad, top, b.Max.X-mergeOuterPad-mergeRulerWidth-mergeRulerGap, top+mergeCardGap)
}

func (n *paneNotes) Texts() [3]string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.texts
}

func (n *paneNotes) Restyle(col color.RGBA) {
	n.mu.Lock()
	n.color = col
	n.mu.Unlock()
	n.Invalidate()
}

func (n *paneNotes) Draw(ctx widget.DrawContext) {
	strip := n.Bounds()
	if strip.Empty() {
		return
	}
	n.mu.Lock()
	texts, col := n.texts, n.color
	n.mu.Unlock()
	prev := ctx.Clip()
	for i, span := range paneSpans(strip, n.merge.ShowBase()) {
		if span[1] <= span[0] || texts[i] == "" {
			continue
		}
		ctx.SetClip(image.Rect(span[0], strip.Min.Y, span[1], strip.Max.Y).Intersect(prev))
		ctx.DrawTextSize(texts[i], span[0]+notePadX, strip.Min.Y+notePadY, noteFontSize, col)
	}
	ctx.SetClip(prev)
}

func paneSpans(strip image.Rectangle, showBase bool) [3][2]int {
	panes := 3
	if !showBase {
		panes = 2
	}
	width := (strip.Dx() - mergeGutter*(panes-1)) / panes
	var spans [3][2]int
	x := strip.Min.X
	for i := range spans {
		if i == basePane && !showBase {
			continue
		}
		spans[i] = [2]int{x, x + width}
		x += width + mergeGutter
	}
	return spans
}
