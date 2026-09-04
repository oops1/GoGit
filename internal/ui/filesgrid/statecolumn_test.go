package filesgrid

import (
	"image"
	"image/color"
	"testing"

	"github.com/oops1/headless-gui/v3/widget/datagrid"

	"github.com/oops1/gogit/internal/ui/changes"
)

type recordingDrawCtx struct {
	texts []struct {
		text string
		size float64
	}
}

func (c *recordingDrawCtx) FillRect(x, y, w, h int, col color.RGBA)      {}
func (c *recordingDrawCtx) FillRectAlpha(x, y, w, h int, col color.RGBA) {}
func (c *recordingDrawCtx) DrawBorder(x, y, w, h int, col color.RGBA)    {}
func (c *recordingDrawCtx) DrawText(text string, x, y int, col color.RGBA) {
	c.DrawTextSize(text, x, y, 12, col)
}

func (c *recordingDrawCtx) DrawTextSize(text string, x, y int, sizePt float64, col color.RGBA) {
	c.texts = append(c.texts, struct {
		text string
		size float64
	}{text, sizePt})
}

func (c *recordingDrawCtx) MeasureText(text string, sizePt float64) int     { return len(text) * 7 }
func (c *recordingDrawCtx) SetClip(r image.Rectangle)                       {}
func (c *recordingDrawCtx) ClearClip()                                      {}
func (c *recordingDrawCtx) DrawHLine(x, y, length int, col color.RGBA)      {}
func (c *recordingDrawCtx) DrawVLine(x, y, length int, col color.RGBA)      {}
func (c *recordingDrawCtx) DrawImage(src image.Image, x, y int)             {}
func (c *recordingDrawCtx) DrawImageScaled(src image.Image, x, y, w, h int) {}

func stateCellContext(item interface{}, dc datagrid.DrawContextBridge) datagrid.CellDrawContext {
	return datagrid.CellDrawContext{
		Rect:      image.Rect(0, 0, 110, DefaultRowHeight),
		Item:      item,
		DrawCtx:   dc,
		TextColor: color.RGBA{A: 0xFF},
		FontSize:  10,
	}
}

func TestDrawStateCellUsesASmallerFontThanTheGrid(t *testing.T) {
	dc := &recordingDrawCtx{}
	drawStateCell(stateCellContext(changes.Row{State: "Изменён"}, dc))

	if len(dc.texts) != 1 {
		t.Fatalf("texts drawn = %d, want 1", len(dc.texts))
	}
	if dc.texts[0].text != "Изменён" {
		t.Fatalf("text = %q", dc.texts[0].text)
	}
	if dc.texts[0].size >= 10 {
		t.Fatalf("state font = %v, want smaller than the grid font", dc.texts[0].size)
	}
}

func TestDrawStateCellIgnoresNonRowItems(t *testing.T) {
	dc := &recordingDrawCtx{}
	drawStateCell(stateCellContext("not-a-row", dc))
	if len(dc.texts) != 0 {
		t.Fatal("expected no drawing for a non-Row item")
	}
}

func TestStateColumnIsDrawnByTheTemplate(t *testing.T) {
	g := New()
	visible := DefaultVisible()
	index := -1
	for i, id := range visible {
		if id == ColState {
			index = i
		}
	}
	if index < 0 {
		t.Fatal("the state column is not visible by default")
	}
	cols := g.Data().Grid.Columns()
	if index >= len(cols) {
		t.Fatalf("columns = %d, state column index = %d", len(cols), index)
	}
	if _, ok := cols[index].(*datagrid.DataGridTemplateColumn); !ok {
		t.Fatalf("state column type = %T, want a template column", cols[index])
	}
}
