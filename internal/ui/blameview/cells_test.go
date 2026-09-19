package blameview

import (
	"image"
	"image/color"
	"testing"

	"github.com/oops1/headless-gui/v3/widget/datagrid"
)

type drawnText struct {
	text  string
	x, y  int
	size  float64
	color color.RGBA
}

type drawnImage struct {
	x, y, w, h int
}

type recordingDrawCtx struct {
	texts  []drawnText
	images []drawnImage
	fills  []image.Rectangle
}

func (c *recordingDrawCtx) FillRect(x, y, w, h int, _ color.RGBA) {
	c.fills = append(c.fills, image.Rect(x, y, x+w, y+h))
}
func (c *recordingDrawCtx) FillRectAlpha(int, int, int, int, color.RGBA)       {}
func (c *recordingDrawCtx) FillEllipseAA(int, int, int, int, color.RGBA)       {}
func (c *recordingDrawCtx) FillRoundRect(int, int, int, int, int, color.RGBA)  {}
func (c *recordingDrawCtx) DrawLineAA(int, int, int, int, float64, color.RGBA) {}
func (c *recordingDrawCtx) StrokePolylineAA([]image.Point, float64, bool, color.RGBA) {
}
func (c *recordingDrawCtx) StrokeEllipseAA(int, int, int, int, float64, color.RGBA) {}
func (c *recordingDrawCtx) FillPolygonAA([]image.Point, color.RGBA)                 {}
func (c *recordingDrawCtx) DrawBorder(int, int, int, int, color.RGBA)               {}
func (c *recordingDrawCtx) DrawText(text string, x, y int, col color.RGBA) {
	c.DrawTextSize(text, x, y, 12, col)
}

func (c *recordingDrawCtx) DrawTextSize(text string, x, y int, sizePt float64, col color.RGBA) {
	c.texts = append(c.texts, drawnText{text: text, x: x, y: y, size: sizePt, color: col})
}

func (c *recordingDrawCtx) MeasureText(text string, _ float64) int { return len(text) * 7 }
func (c *recordingDrawCtx) SetClip(image.Rectangle)                {}
func (c *recordingDrawCtx) ClearClip()                             {}
func (c *recordingDrawCtx) DrawHLine(int, int, int, color.RGBA)    {}
func (c *recordingDrawCtx) DrawVLine(int, int, int, color.RGBA)    {}
func (c *recordingDrawCtx) DrawImage(image.Image, int, int)        {}
func (c *recordingDrawCtx) DrawImageScaled(_ image.Image, x, y, w, h int) {
	c.images = append(c.images, drawnImage{x: x, y: y, w: w, h: h})
}

func blameCell(item interface{}, dc datagrid.DrawContextBridge) datagrid.CellDrawContext {
	return datagrid.CellDrawContext{
		Rect:      image.Rect(0, 0, 80, rowHeight),
		Item:      item,
		DrawCtx:   dc,
		TextColor: color.RGBA{A: 0xFF},
		FontSize:  fontSize,
	}
}

func TestTheCommitCellIsDrawnAsAnUnderlinedLink(t *testing.T) {
	v := newTestView(t)
	v.linkColor = color.RGBA{B: 0xFF, A: 0xFF}
	dc := &recordingDrawCtx{}

	v.drawCommitCell(blameCell(Row{Commit: "abcdef1"}, dc))

	if len(dc.texts) != 1 || dc.texts[0].text != "abcdef1" {
		t.Fatalf("texts = %+v", dc.texts)
	}
	if dc.texts[0].color != v.linkColor {
		t.Fatalf("colour = %+v, want the link colour", dc.texts[0].color)
	}
	if len(dc.fills) != 1 || dc.fills[0].Dy() != 1 {
		t.Fatalf("underline = %+v", dc.fills)
	}
}

func TestACellOfTheWrongShapeOrAnEmptyCommitDrawsNothing(t *testing.T) {
	v := newTestView(t)
	dc := &recordingDrawCtx{}

	v.drawCommitCell(blameCell("not a row", dc))
	v.drawCommitCell(blameCell(Row{}, dc))
	v.drawAuthorCell(blameCell("not a row", dc))
	v.drawAuthorCell(blameCell(Row{Author: ""}, dc))

	if len(dc.texts) != 0 || len(dc.images) != 0 || len(dc.fills) != 0 {
		t.Fatalf("drew %+v %+v %+v", dc.texts, dc.images, dc.fills)
	}
}

func TestTheAuthorCellIsABadgeWithInitials(t *testing.T) {
	v := newTestView(t)
	dc := &recordingDrawCtx{}

	v.drawAuthorCell(blameCell(Row{Author: "Ann Blake"}, dc))

	if len(dc.images) != 1 || dc.images[0].w != dc.images[0].h {
		t.Fatalf("badge = %+v, want one square", dc.images)
	}
	if len(dc.texts) != 1 || dc.texts[0].text != "AB" {
		t.Fatalf("texts = %+v, want the initials", dc.texts)
	}
}

func TestARowTooShortForTheBadgeDrawsNothing(t *testing.T) {
	v := newTestView(t)
	dc := &recordingDrawCtx{}
	cell := blameCell(Row{Author: "Ann Blake"}, dc)
	cell.Rect = image.Rect(0, 0, 80, 2*badgePadding)

	v.drawAuthorCell(cell)

	if len(dc.images) != 0 {
		t.Fatalf("badge = %+v, want none in a row with no room", dc.images)
	}
}
