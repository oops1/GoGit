package search

import (
	"image"
	"image/color"
	"testing"

	"github.com/oops1/headless-gui/v3/widget"
	"github.com/oops1/headless-gui/v3/widget/datagrid"

	"github.com/oops1/gogit/internal/i18n"
)

type recordingKindDrawCtx struct {
	texts []struct {
		text string
		x, y int
		size float64
		col  color.RGBA
	}
}

func (c *recordingKindDrawCtx) FillRect(x, y, w, h int, col color.RGBA)         {}
func (c *recordingKindDrawCtx) FillRectAlpha(x, y, w, h int, col color.RGBA)    {}
func (c *recordingKindDrawCtx) FillRoundRect(x, y, w, h, r int, col color.RGBA) {}
func (c *recordingKindDrawCtx) FillEllipseAA(cx, cy, rx, ry int, col color.RGBA) {
}
func (c *recordingKindDrawCtx) DrawBorder(x, y, w, h int, col color.RGBA) {}
func (c *recordingKindDrawCtx) DrawText(text string, x, y int, col color.RGBA) {
	c.DrawTextSize(text, x, y, 12, col)
}

func (c *recordingKindDrawCtx) DrawTextSize(text string, x, y int, sizePt float64, col color.RGBA) {
	c.texts = append(c.texts, struct {
		text string
		x, y int
		size float64
		col  color.RGBA
	}{text, x, y, sizePt, col})
}

func (c *recordingKindDrawCtx) MeasureText(text string, sizePt float64) int { return len(text) * 6 }
func (c *recordingKindDrawCtx) SetClip(r image.Rectangle)                   {}
func (c *recordingKindDrawCtx) ClearClip()                                  {}
func (c *recordingKindDrawCtx) DrawHLine(x, y, length int, col color.RGBA)  {}
func (c *recordingKindDrawCtx) DrawVLine(x, y, length int, col color.RGBA)  {}
func (c *recordingKindDrawCtx) DrawImage(src image.Image, x, y int)         {}
func (c *recordingKindDrawCtx) DrawImageScaled(src image.Image, x, y, w, h int) {
}

func kindCellContext(item interface{}, dc datagrid.DrawContextBridge) datagrid.CellDrawContext {
	return datagrid.CellDrawContext{
		Rect:      image.Rect(0, 0, 160, 22),
		Item:      item,
		DrawCtx:   dc,
		TextColor: color.RGBA{A: 0xFF},
		FontSize:  12,
	}
}

func setupI18N(t *testing.T) {
	t.Helper()
	widget.ClearStrings()
	t.Cleanup(widget.ClearStrings)
	if _, err := i18n.Install(""); err != nil {
		t.Fatal(err)
	}
	i18n.Apply("en")
}

func TestBuildColumnsCreatesPathAndKindColumns(t *testing.T) {
	setupI18N(t)
	v := newTestView(t)
	cols := v.resultsTable.Grid.Columns()
	if len(cols) != 2 {
		t.Fatalf("columns = %d, want 2", len(cols))
	}
	if got := v.resultsTable.Grid.EmptyStateText; got != i18n.T("Dialog.Search.Empty") {
		t.Fatalf("EmptyStateText = %q", got)
	}
}

func TestKindLabelForEachKind(t *testing.T) {
	setupI18N(t)
	tests := []struct {
		name string
		f    Found
		want string
	}{
		{"repository", Found{}, i18n.T("Dialog.Search.Kind.Repository")},
		{"bare", Found{Bare: true}, i18n.T("Dialog.Search.Kind.Bare")},
		{"worktree", Found{Worktree: true}, i18n.T("Dialog.Search.Kind.Worktree")},
		{"worktree wins over bare", Found{Bare: true, Worktree: true}, i18n.T("Dialog.Search.Kind.Worktree")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := kindLabel(tt.f); got != tt.want {
				t.Fatalf("kindLabel() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDrawKindCellSkipsUnknownItemTypes(t *testing.T) {
	setupI18N(t)
	dc := &recordingKindDrawCtx{}
	drawKindCell(kindCellContext("not a found", dc))
	if len(dc.texts) != 0 {
		t.Fatal("expected no drawing for an item that is not a Found")
	}
}

func TestDrawKindCellDrawsTheLabel(t *testing.T) {
	setupI18N(t)
	dc := &recordingKindDrawCtx{}
	drawKindCell(kindCellContext(Found{Bare: true}, dc))
	if len(dc.texts) != 1 || dc.texts[0].text != i18n.T("Dialog.Search.Kind.Bare") {
		t.Fatalf("texts = %+v", dc.texts)
	}
}
