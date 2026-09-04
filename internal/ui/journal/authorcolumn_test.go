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
	col := NewView().newAuthorColumn("Author", true)
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
	col := NewView().newAuthorColumn("Author", false)
	if _, ok := col.(*datagrid.DataGridTemplateColumn); !ok {
		t.Fatalf("column type = %T, want *datagrid.DataGridTemplateColumn", col)
	}
}

func TestDrawAuthorBadgeCellIgnoresNonRowItems(t *testing.T) {
	t.Cleanup(resetBadgeCacheForTest)
	dc := &recordingDrawCtx{}
	NewView().drawAuthorBadgeCell(cellContext("not-a-row", dc))
	if len(dc.images) != 0 || len(dc.texts) != 0 {
		t.Fatal("expected no drawing for a non-Row item")
	}
}

func TestDrawAuthorBadgeCellSkipsRowsWithNoAuthor(t *testing.T) {
	t.Cleanup(resetBadgeCacheForTest)
	dc := &recordingDrawCtx{}
	NewView().drawAuthorBadgeCell(cellContext(Row{Author: ""}, dc))
	if len(dc.images) != 0 || len(dc.texts) != 0 {
		t.Fatal("expected no drawing for a row with an empty author")
	}
}

func TestDrawAuthorBadgeCellDrawsTheBadgeImageAndTheInitials(t *testing.T) {
	t.Cleanup(resetBadgeCacheForTest)
	dc := &recordingDrawCtx{}
	cdc := cellContext(Row{Author: "Чукалин Валерий"}, dc)
	NewView().drawAuthorBadgeCell(cdc)

	if len(dc.images) != 1 {
		t.Fatalf("images drawn = %d, want 1", len(dc.images))
	}
	want := badgeFitSize(cdc.Rect.Dy())
	if dc.images[0].w != want || dc.images[0].h != want {
		t.Fatalf("badge size = %dx%d, want %dx%d", dc.images[0].w, dc.images[0].h, want, want)
	}
	if len(dc.texts) != 1 {
		t.Fatalf("texts drawn = %d, want 1", len(dc.texts))
	}
	if dc.texts[0].text != "ЧВ" {
		t.Fatalf("initials = %q, want %q", dc.texts[0].text, "ЧВ")
	}
	if got := dc.texts[0].color; got != badgeTextColor(authorColor("Чукалин Валерий")) {
		t.Fatalf("text color = %v, want the xor of the badge colour", got)
	}
}

func TestDrawAuthorBadgeCellFillsTheRowHeightWithTheBadge(t *testing.T) {
	t.Cleanup(resetBadgeCacheForTest)
	dc := &recordingDrawCtx{}
	cdc := cellContext(Row{Author: "ann"}, dc)
	cdc.Rect = image.Rect(0, 0, 40, 20)
	NewView().drawAuthorBadgeCell(cdc)

	if len(dc.images) != 1 {
		t.Fatalf("images drawn = %d, want 1", len(dc.images))
	}
	if got := dc.images[0].w; got != 20-2*authorBadgePaddingY {
		t.Fatalf("badge width = %d, want the row height minus its padding", got)
	}
}

func TestDrawAuthorBadgeCellSkipsWhenTheRowIsTooShortForABadge(t *testing.T) {
	t.Cleanup(resetBadgeCacheForTest)
	dc := &recordingDrawCtx{}
	cdc := cellContext(Row{Author: "ann"}, dc)
	cdc.Rect = image.Rect(0, 0, 160, 2*authorBadgePaddingY)
	NewView().drawAuthorBadgeCell(cdc)

	if len(dc.images) != 0 || len(dc.texts) != 0 {
		t.Fatal("expected no drawing when the row is too short to fit a badge")
	}
}

func TestDrawAuthorBadgeCellSkipsARepeatedAuthor(t *testing.T) {
	t.Cleanup(resetBadgeCacheForTest)
	v := NewView()
	v.Append([]Row{{Author: "ann"}, {Author: "ann"}, {Author: "bob"}})

	for _, tc := range []struct {
		name  string
		index int
		drawn bool
	}{
		{"first row of the author", 0, true},
		{"same author again", 1, false},
		{"another author", 2, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dc := &recordingDrawCtx{}
			item, _ := v.items.Get(tc.index).(Row)
			cdc := cellContext(item, dc)
			cdc.RowIndex = tc.index
			v.drawAuthorBadgeCell(cdc)
			if drawn := len(dc.images) == 1; drawn != tc.drawn {
				t.Fatalf("badge drawn = %v, want %v", drawn, tc.drawn)
			}
		})
	}
}

func TestAuthorColumnWidthFollowsTheRowHeight(t *testing.T) {
	if got := authorBadgeColumnWidth(20); got != badgeFitSize(20)+2*authorBadgePaddingX {
		t.Fatalf("authorBadgeColumnWidth(20) = %d", got)
	}
}
