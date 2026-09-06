package settings

import (
	"image"
	"image/color"
	"testing"

	"github.com/oops1/headless-gui/v3/widget"
	"github.com/oops1/headless-gui/v3/widget/datagrid"

	"github.com/oops1/gogit/internal/i18n"
)

type recordingStatusDrawCtx struct {
	ellipses []struct {
		cx, cy, rx, ry int
		col            color.RGBA
	}
	texts []struct {
		text string
		x, y int
		size float64
		col  color.RGBA
	}
}

func (c *recordingStatusDrawCtx) FillRect(x, y, w, h int, col color.RGBA)      {}
func (c *recordingStatusDrawCtx) FillRectAlpha(x, y, w, h int, col color.RGBA) {}
func (c *recordingStatusDrawCtx) FillRoundRect(x, y, w, h, r int, col color.RGBA) {
}

func (c *recordingStatusDrawCtx) FillEllipseAA(cx, cy, rx, ry int, col color.RGBA) {
	c.ellipses = append(c.ellipses, struct {
		cx, cy, rx, ry int
		col            color.RGBA
	}{cx, cy, rx, ry, col})
}

func (c *recordingStatusDrawCtx) DrawBorder(x, y, w, h int, col color.RGBA) {}
func (c *recordingStatusDrawCtx) DrawText(text string, x, y int, col color.RGBA) {
	c.DrawTextSize(text, x, y, 12, col)
}

func (c *recordingStatusDrawCtx) DrawTextSize(text string, x, y int, sizePt float64, col color.RGBA) {
	c.texts = append(c.texts, struct {
		text string
		x, y int
		size float64
		col  color.RGBA
	}{text, x, y, sizePt, col})
}

func (c *recordingStatusDrawCtx) MeasureText(text string, sizePt float64) int { return len(text) * 6 }
func (c *recordingStatusDrawCtx) SetClip(r image.Rectangle)                   {}
func (c *recordingStatusDrawCtx) ClearClip()                                  {}
func (c *recordingStatusDrawCtx) DrawHLine(x, y, length int, col color.RGBA)  {}
func (c *recordingStatusDrawCtx) DrawVLine(x, y, length int, col color.RGBA)  {}
func (c *recordingStatusDrawCtx) DrawImage(src image.Image, x, y int)         {}
func (c *recordingStatusDrawCtx) DrawImageScaled(src image.Image, x, y, w, h int) {
}

func statusCellContext(item interface{}, dc datagrid.DrawContextBridge) datagrid.CellDrawContext {
	return datagrid.CellDrawContext{
		Rect:      image.Rect(0, 0, 160, 22),
		Item:      item,
		DrawCtx:   dc,
		TextColor: color.RGBA{A: 0xFF},
		FontSize:  12,
	}
}

func TestDrawSecretStatusCellSkipsUnknownItemTypes(t *testing.T) {
	dc := &recordingStatusDrawCtx{}
	drawSecretStatusCell(statusCellContext("not a secret", dc))
	if len(dc.ellipses) != 0 || len(dc.texts) != 0 {
		t.Fatal("expected no drawing for an item that carries no status")
	}
}

func TestDrawSecretStatusCellDrawsTheDerivedDotColorAndLabel(t *testing.T) {
	widget.ClearStrings()
	defer widget.ClearStrings()
	if _, err := i18n.Install(""); err != nil {
		t.Fatal(err)
	}
	i18n.Apply("en")
	widget.ApplyGlobalTheme(widget.Win11DarkTheme())

	dc := &recordingStatusDrawCtx{}
	drawSecretStatusCell(statusCellContext(SecretEntry{Resource: "r", Status: StatusError}, dc))

	if len(dc.ellipses) != 1 {
		t.Fatalf("ellipses drawn = %d, want 1", len(dc.ellipses))
	}
	want := StatusError.DotColor(widget.CurrentTheme())
	if dc.ellipses[0].col != want {
		t.Fatalf("dot color = %+v, want %+v", dc.ellipses[0].col, want)
	}
	if len(dc.texts) != 1 || dc.texts[0].text != StatusError.Label() {
		t.Fatalf("texts = %+v, want label %q", dc.texts, StatusError.Label())
	}
}

func TestDrawSecretStatusCellWorksForKeyEntryToo(t *testing.T) {
	widget.ClearStrings()
	defer widget.ClearStrings()
	if _, err := i18n.Install(""); err != nil {
		t.Fatal(err)
	}
	i18n.Apply("en")

	dc := &recordingStatusDrawCtx{}
	drawSecretStatusCell(statusCellContext(KeyEntry{Host: "h", Status: StatusAuthRequired}, dc))
	if len(dc.ellipses) != 1 || len(dc.texts) != 1 {
		t.Fatalf("expected one dot and one label for a KeyEntry, got ellipses=%d texts=%d", len(dc.ellipses), len(dc.texts))
	}
	if dc.texts[0].text != StatusAuthRequired.Label() {
		t.Fatalf("label = %q, want %q", dc.texts[0].text, StatusAuthRequired.Label())
	}
}

func TestStatusOfItemDefaultsToSavedForUnknownTypes(t *testing.T) {
	if status, ok := statusOfItem(42); ok || status != StatusSaved {
		t.Fatalf("statusOfItem(42) = %v,%v, want StatusSaved,false", status, ok)
	}
}
