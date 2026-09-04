package journal

import (
	"image"
	"image/color"
	"testing"

	"github.com/oops1/headless-gui/v3/widget/datagrid"
)

type recordingDrawCtx struct {
	images []struct {
		img  image.Image
		x, y int
		w, h int
	}
	texts []struct {
		text  string
		x, y  int
		size  float64
		color color.RGBA
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
		text  string
		x, y  int
		size  float64
		color color.RGBA
	}{text, x, y, sizePt, col})
}

func (c *recordingDrawCtx) MeasureText(text string, sizePt float64) int { return len(text) * 7 }
func (c *recordingDrawCtx) SetClip(r image.Rectangle)                   {}
func (c *recordingDrawCtx) ClearClip()                                  {}
func (c *recordingDrawCtx) DrawHLine(x, y, length int, col color.RGBA)  {}
func (c *recordingDrawCtx) DrawVLine(x, y, length int, col color.RGBA)  {}
func (c *recordingDrawCtx) DrawImage(src image.Image, x, y int)         {}
func (c *recordingDrawCtx) DrawImageScaled(src image.Image, x, y, w, h int) {
	c.images = append(c.images, struct {
		img  image.Image
		x, y int
		w, h int
	}{src, x, y, w, h})
}

func cellContext(item interface{}, dc datagrid.DrawContextBridge) datagrid.CellDrawContext {
	return datagrid.CellDrawContext{
		Rect:      image.Rect(0, 0, 160, 28),
		Item:      item,
		DrawCtx:   dc,
		TextColor: color.RGBA{A: 0xFF},
		FontSize:  12,
	}
}

func TestNewAuthorColumnReturnsATextColumnBoundToAuthorWhenFullNameIsEnabled(t *testing.T) {
	col := newAuthorColumn("Author", true)
	textCol, ok := col.(*datagrid.DataGridTextColumn)
	if !ok {
		t.Fatalf("column type = %T, want *datagrid.DataGridTextColumn", col)
	}
	if textCol.GetBinding() == nil || textCol.GetBinding().Path != "Author" {
		t.Fatalf("binding = %+v, want path Author", textCol.GetBinding())
	}
	if col.Header() != "Author" {
		t.Fatalf("header = %q, want Author", col.Header())
	}
}

func TestNewAuthorColumnReturnsATemplateColumnWhenFullNameIsDisabled(t *testing.T) {
	col := newAuthorColumn("Author", false)
	if _, ok := col.(*datagrid.DataGridTemplateColumn); !ok {
		t.Fatalf("column type = %T, want *datagrid.DataGridTemplateColumn", col)
	}
}

func TestDrawAuthorBadgeCellIgnoresNonRowItems(t *testing.T) {
	t.Cleanup(resetBadgeCacheForTest)
	dc := &recordingDrawCtx{}
	drawAuthorBadgeCell(cellContext("not-a-row", dc))
	if len(dc.images) != 0 || len(dc.texts) != 0 {
		t.Fatal("expected no drawing for a non-Row item")
	}
}

func TestDrawAuthorBadgeCellSkipsRowsWithNoAuthor(t *testing.T) {
	t.Cleanup(resetBadgeCacheForTest)
	dc := &recordingDrawCtx{}
	drawAuthorBadgeCell(cellContext(Row{Author: ""}, dc))
	if len(dc.images) != 0 || len(dc.texts) != 0 {
		t.Fatal("expected no drawing for a row with an empty author")
	}
}

func TestDrawAuthorBadgeCellDrawsTheBadgeImageAndTheInitials(t *testing.T) {
	t.Cleanup(resetBadgeCacheForTest)
	dc := &recordingDrawCtx{}
	drawAuthorBadgeCell(cellContext(Row{Author: "Чукалин Валерий"}, dc))

	if len(dc.images) != 1 {
		t.Fatalf("images drawn = %d, want 1", len(dc.images))
	}
	if dc.images[0].w != authorBadgeSize || dc.images[0].h != authorBadgeSize {
		t.Fatalf("badge size = %dx%d, want %dx%d", dc.images[0].w, dc.images[0].h, authorBadgeSize, authorBadgeSize)
	}
	if len(dc.texts) != 1 {
		t.Fatalf("texts drawn = %d, want 1", len(dc.texts))
	}
	if dc.texts[0].text != "ЧВ" {
		t.Fatalf("initials = %q, want %q", dc.texts[0].text, "ЧВ")
	}
	want := badgeTextColor(authorColor("Чукалин Валерий"))
	if dc.texts[0].color != want {
		t.Fatalf("text color = %v, want %v", dc.texts[0].color, want)
	}
}

func TestDrawAuthorBadgeCellShrinksTheBadgeToFitAShortRow(t *testing.T) {
	t.Cleanup(resetBadgeCacheForTest)
	dc := &recordingDrawCtx{}
	cdc := cellContext(Row{Author: "ann"}, dc)
	cdc.Rect = image.Rect(0, 0, 160, 10)
	drawAuthorBadgeCell(cdc)

	if len(dc.images) != 1 {
		t.Fatalf("images drawn = %d, want 1", len(dc.images))
	}
	if dc.images[0].w >= authorBadgeSize {
		t.Fatalf("badge width = %d, want less than %d for a short row", dc.images[0].w, authorBadgeSize)
	}
}

func TestDrawAuthorBadgeCellSkipsWhenTheRowIsTooShortForABadge(t *testing.T) {
	t.Cleanup(resetBadgeCacheForTest)
	dc := &recordingDrawCtx{}
	cdc := cellContext(Row{Author: "ann"}, dc)
	cdc.Rect = image.Rect(0, 0, 160, 2*authorBadgePaddingX)
	drawAuthorBadgeCell(cdc)

	if len(dc.images) != 0 || len(dc.texts) != 0 {
		t.Fatal("expected no drawing when the row is too short to fit a badge")
	}
}

func TestBadgeFitSizeCapsAtTheDefaultBadgeSize(t *testing.T) {
	if got := badgeFitSize(200); got != authorBadgeSize {
		t.Fatalf("badgeFitSize(200) = %d, want %d", got, authorBadgeSize)
	}
}

func TestBadgeFitSizeShrinksForATightRow(t *testing.T) {
	if got := badgeFitSize(10); got != 10-2*authorBadgePaddingX {
		t.Fatalf("badgeFitSize(10) = %d, want %d", got, 10-2*authorBadgePaddingX)
	}
}
