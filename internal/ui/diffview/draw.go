package diffview

import (
	"image"
	"image/color"
	"strconv"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/i18n"
)

func (dv *DiffView) Draw(ctx widget.DrawContext) {
	b := dv.Bounds()
	if b.Empty() {
		return
	}
	s := dv.snapshot()
	ctx.FillRect(b.Min.X, b.Min.Y, b.Dx(), b.Dy(), s.pal.Background)
	if s.doc.Binary {
		dv.drawBinary(ctx, b, s)
		return
	}
	if len(s.rows) == 0 {
		return
	}
	outer := ctx.Clip()
	g := dv.layout(s)
	g.clip = outer.Intersect(b)
	first, last := g.visibleRows(len(s.rows))
	for i := first; i < last; i++ {
		dv.drawRow(ctx, g, s, s.rows[i], i)
	}
	ctx.SetClip(outer)
	dv.drawSeparators(ctx, g, s)
	dv.drawScrollbars(ctx, g, s)
}

func (dv *DiffView) drawBinary(ctx widget.DrawContext, b image.Rectangle, s snapshot) {
	text := i18n.T("Diff.Binary")
	width := dv.measure(text, s.size)
	x := b.Min.X + max(b.Dx()-width, 0)/2
	y := b.Min.Y + max(b.Dy()-int(s.size), 0)/2
	ctx.DrawTextFont(text, x, y, s.size, s.font, s.pal.Text)
}

func (dv *DiffView) drawRow(ctx widget.DrawContext, g geometry, s snapshot, r row, index int) {
	y := g.rowY(index)
	switch {
	case r.header:
		dv.drawHeaderRow(ctx, g, s, r, y)
	case s.mode == Unified:
		dv.drawUnifiedRow(ctx, g, s, r, y)
	default:
		dv.drawSideRow(ctx, g, s, r, y)
	}
	if index == s.selected {
		ctx.SetClip(g.clipped(g.content))
		ctx.DrawBorder(g.content.Min.X, y, g.content.Dx(), g.rowH, s.pal.Selection)
	}
}

func (dv *DiffView) drawHeaderRow(ctx widget.DrawContext, g geometry, s snapshot, r row, y int) {
	ctx.SetClip(g.clipped(g.content))
	ctx.FillRect(g.content.Min.X, y, g.content.Dx(), g.rowH, s.pal.HunkHeaderBG)
	ctx.DrawTextFont(r.text, g.content.Min.X+textPad-g.scrollX, dv.textBaseline(y, g.rowH, s.size),
		s.size, s.font, s.pal.HunkHeaderText)
}

func (dv *DiffView) drawSideRow(ctx widget.DrawContext, g geometry, s snapshot, r row, y int) {
	dv.drawPane(ctx, g, s, r.left, g.leftNumbers(), g.leftText(), y, r.left.oldNo)
	dv.drawPane(ctx, g, s, r.right, g.rightNumbers(), g.rightText(), y, r.right.newNo)
}

func (dv *DiffView) drawUnifiedRow(ctx widget.DrawContext, g geometry, s snapshot, r row, y int) {
	c := r.left
	dv.drawNumbers(ctx, g, s, c, g.oldNumbers(), y, c.oldNo)
	dv.drawNumbers(ctx, g, s, c, g.newNumbers(), y, c.newNo)
	area := g.unifiedText()
	ctx.SetClip(g.clipped(image.Rect(area.Min.X, y, area.Max.X, y+g.rowH)))
	ctx.FillRect(area.Min.X, y, area.Dx(), g.rowH, cellBackground(s.pal, c))
	x := area.Min.X + textPad - g.scrollX
	baseline := dv.textBaseline(y, g.rowH, s.size)
	sign := unifiedSign(c.kind)
	ctx.DrawTextFont(sign, x, baseline, s.size, s.font, cellText(s.pal, c))
	textX := x + dv.measure(sign, s.size)
	dv.drawSpans(ctx, g, s, c, textX, y)
	ctx.DrawTextFont(c.text, textX, baseline, s.size, s.font, cellText(s.pal, c))
}

func (dv *DiffView) drawPane(ctx widget.DrawContext, g geometry, s snapshot, c cell,
	numbers, area image.Rectangle, y, number int) {
	dv.drawNumbers(ctx, g, s, c, numbers, y, number)
	ctx.SetClip(g.clipped(image.Rect(area.Min.X, y, area.Max.X, y+g.rowH)))
	ctx.FillRect(area.Min.X, y, area.Dx(), g.rowH, cellBackground(s.pal, c))
	if !c.filled {
		return
	}
	x := area.Min.X + textPad - g.scrollX
	dv.drawSpans(ctx, g, s, c, x, y)
	ctx.DrawTextFont(c.text, x, dv.textBaseline(y, g.rowH, s.size), s.size, s.font, cellText(s.pal, c))
}

func (dv *DiffView) drawNumbers(ctx widget.DrawContext, g geometry, s snapshot, c cell,
	numbers image.Rectangle, y, number int) {
	ctx.SetClip(g.clipped(image.Rect(numbers.Min.X, y, numbers.Max.X, y+g.rowH)))
	ctx.FillRect(numbers.Min.X, y, numbers.Dx(), g.rowH, gutterBackground(s.pal, c))
	if number <= 0 {
		return
	}
	text := strconv.Itoa(number)
	x := numbers.Max.X - numberPad - dv.measure(text, s.size)
	ctx.DrawTextFont(text, x, dv.textBaseline(y, g.rowH, s.size), s.size, s.font, s.pal.GutterText)
}

func (dv *DiffView) drawSpans(ctx widget.DrawContext, g geometry, s snapshot, c cell, x, y int) {
	tint := s.pal.AddedSpan
	if c.kind == Removed {
		tint = s.pal.RemovedSpan
	}
	for _, span := range c.spans {
		start := dv.runePrefixWidth(c.text, span.Start, s.size)
		end := dv.runePrefixWidth(c.text, span.End, s.size)
		if end <= start {
			continue
		}
		ctx.FillRectAlpha(x+start, y, end-start, g.rowH, tint)
	}
}

func (dv *DiffView) drawSeparators(ctx widget.DrawContext, g geometry, s snapshot) {
	height := g.content.Dy()
	if s.mode == Unified {
		ctx.DrawVLine(g.oldNumbers().Max.X, g.content.Min.Y, height, s.pal.Border)
		ctx.DrawVLine(g.newNumbers().Max.X, g.content.Min.Y, height, s.pal.Border)
		return
	}
	ctx.DrawVLine(g.leftNumbers().Max.X, g.content.Min.Y, height, s.pal.Border)
	ctx.DrawVLine(g.split, g.content.Min.Y, height, s.pal.Border)
	ctx.DrawVLine(g.rightNumbers().Max.X, g.content.Min.Y, height, s.pal.Border)
}

func (dv *DiffView) drawScrollbars(ctx widget.DrawContext, g geometry, s snapshot) {
	if g.hasV {
		ctx.FillRect(g.vBar.Min.X, g.vBar.Min.Y, g.vBar.Dx(), g.vBar.Dy(), s.pal.ScrollTrack)
		thumb := g.vThumb()
		ctx.FillRoundRect(thumb.Min.X+2, thumb.Min.Y, thumb.Dx()-4, thumb.Dy(), 3, s.pal.ScrollThumb)
	}
	if g.hasH {
		ctx.FillRect(g.hBar.Min.X, g.hBar.Min.Y, g.hBar.Dx(), g.hBar.Dy(), s.pal.ScrollTrack)
		thumb := g.hThumb()
		ctx.FillRoundRect(thumb.Min.X, thumb.Min.Y+2, thumb.Dx(), thumb.Dy()-4, 3, s.pal.ScrollThumb)
	}
}

const pointToPixel = 96.0 / 72.0

func (dv *DiffView) textBaseline(y, rowH int, size float64) int {
	return y + max(rowH-int(size*pointToPixel), 0)/2
}

func unifiedSign(kind Kind) string {
	switch kind {
	case Added:
		return "+"
	case Removed:
		return "-"
	case NoNewline:
		return `\`
	default:
		return " "
	}
}

func cellBackground(p Palette, c cell) color.RGBA {
	switch {
	case !c.filled:
		return p.PlaceholderBG
	case c.kind == Added:
		return p.AddedBG
	case c.kind == Removed:
		return p.RemovedBG
	default:
		return p.Background
	}
}

func gutterBackground(p Palette, c cell) color.RGBA {
	switch {
	case !c.filled:
		return p.PlaceholderBG
	case c.kind == Added:
		return p.AddedGutterBG
	case c.kind == Removed:
		return p.RemovedGutterBG
	default:
		return p.GutterBG
	}
}

func cellText(p Palette, c cell) color.RGBA {
	if c.kind == NoNewline {
		return p.NoNewlineText
	}
	return p.Text
}
