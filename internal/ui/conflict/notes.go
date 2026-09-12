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
	mergeShade        = 0.08
)

type paneNotes struct {
	widget.Base

	merge *widget.MergeView
	mu    sync.Mutex
	texts [3]string
	color color.RGBA
	bg    color.RGBA
}

func newPaneNotes(merge *widget.MergeView, ours, base, theirs string) *paneNotes {
	n := &paneNotes{merge: merge, texts: [3]string{ours, base, theirs}}
	n.Restyle(widget.CurrentTheme())
	return n
}

func mergeBackground(t *widget.Theme) color.RGBA {
	card := orColor(t.InputBG, orColor(t.PanelBG, color.RGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF}))
	card.A = 0xFF
	shaded := blend(card, color.RGBA{R: 0x80, G: 0x80, B: 0x80, A: 0xFF}, mergeShade)
	if bg := orColor(t.WindowBG, shaded); bg != card {
		return bg
	}
	return shaded
}

func orColor(c, fallback color.RGBA) color.RGBA {
	if c.A == 0 {
		return fallback
	}
	return c
}

func blend(from, to color.RGBA, share float64) color.RGBA {
	lerp := func(a, b uint8) uint8 { return uint8(float64(a) + (float64(b)-float64(a))*share) }
	return color.RGBA{R: lerp(from.R, to.R), G: lerp(from.G, to.G), B: lerp(from.B, to.B), A: from.A}
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

func (n *paneNotes) Restyle(t *widget.Theme) {
	n.mu.Lock()
	n.color, n.bg = t.SecondaryText, mergeBackground(t)
	n.mu.Unlock()
	n.Invalidate()
}

func (n *paneNotes) Draw(ctx widget.DrawContext) {
	strip := n.Bounds()
	if strip.Empty() {
		return
	}
	n.mu.Lock()
	texts, col, bg := n.texts, n.color, n.bg
	n.mu.Unlock()
	prev := ctx.Clip()
	ctx.SetClip(strip.Intersect(prev))
	ctx.FillRect(strip.Min.X, strip.Min.Y, strip.Dx(), strip.Dy(), bg)
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
