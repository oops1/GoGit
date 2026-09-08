package journal

import (
	"image"
	"image/color"
	"testing"

	"github.com/oops1/headless-gui/v3/widget"
	"github.com/oops1/headless-gui/v3/widget/datagrid"
)

type drawnBadge struct {
	x, y, w, h int
	color      color.RGBA
}

type recordingBadgeCtx struct {
	recordingDrawCtx
	badges []drawnBadge
}

func (c *recordingBadgeCtx) FillRoundRect(x, y, w, h, r int, col color.RGBA) {
	c.badges = append(c.badges, drawnBadge{x, y, w, h, col})
}

func messageCell(row Row, dc datagrid.DrawContextBridge, width int) datagrid.CellDrawContext {
	return datagrid.CellDrawContext{
		Rect:      image.Rect(0, 0, width, 20),
		Item:      row,
		DrawCtx:   dc,
		TextColor: color.RGBA{A: 0xFF},
		FontSize:  9,
	}
}

func newStyledView(t *testing.T) *View {
	t.Helper()
	v := NewView()
	v.Restyle(widget.Win11LightTheme())
	return v
}

func TestEveryRefIsDrawnAsABadgeBeforeTheMessage(t *testing.T) {
	dc := &recordingBadgeCtx{}
	row := Row{
		Message: "a commit",
		Refs: []Ref{
			{Name: "main", Kind: RefBranch, Head: true},
			{Name: "origin/main", Kind: RefRemote},
			{Name: "v1.0.0", Kind: RefTag},
		},
	}

	newStyledView(t).drawMessageCell(messageCell(row, dc, 400))

	if len(dc.badges) != 3 {
		t.Fatalf("badges = %d, want one per ref", len(dc.badges))
	}
	for i := 1; i < len(dc.badges); i++ {
		if dc.badges[i].x <= dc.badges[i-1].x {
			t.Fatalf("badges = %+v, want them side by side", dc.badges)
		}
	}
	if len(dc.texts) != 4 {
		t.Fatalf("texts = %+v, want a name in every badge and the message", dc.texts)
	}
	if dc.texts[3].text != "a commit" {
		t.Fatalf("last text = %q, want the message", dc.texts[3].text)
	}
	if dc.texts[3].x <= dc.badges[2].x {
		t.Fatal("the message must follow the badges")
	}
}

func TestTheKindOfARefDecidesItsColour(t *testing.T) {
	v := newStyledView(t)
	dc := &recordingBadgeCtx{}
	row := Row{Refs: []Ref{
		{Name: "main", Kind: RefBranch, Head: true},
		{Name: "feature", Kind: RefBranch},
		{Name: "origin/main", Kind: RefRemote},
		{Name: "v1", Kind: RefTag},
	}}

	v.drawMessageCell(messageCell(row, dc, 600))

	if len(dc.badges) != 4 {
		t.Fatalf("badges = %d, want one per ref", len(dc.badges))
	}
	if dc.badges[0].color != v.refs.head {
		t.Fatalf("head colour = %v, want the accent", dc.badges[0].color)
	}
	if dc.badges[1].color == dc.badges[0].color {
		t.Fatal("a branch that is not checked out must look calmer than HEAD")
	}
	if dc.badges[2].color == dc.badges[1].color {
		t.Fatal("a remote branch must differ from a local one")
	}
	if dc.badges[3].color != v.refs.tag {
		t.Fatalf("tag colour = %v, want the tag fill", dc.badges[3].color)
	}
}

func TestBadgesNeverEatTheWholeMessageColumn(t *testing.T) {
	dc := &recordingBadgeCtx{}
	row := Row{
		Message: "a commit",
		Refs: []Ref{
			{Name: "a-long-branch-name", Kind: RefBranch},
			{Name: "another-long-branch-name", Kind: RefBranch},
			{Name: "and-one-more-branch-name", Kind: RefBranch},
		},
	}

	newStyledView(t).drawMessageCell(messageCell(row, dc, 120))

	if len(dc.badges) >= 3 {
		t.Fatalf("badges = %d, want the row to keep room for the message", len(dc.badges))
	}
}

func TestTheMessageCellDrawsNothingForAnythingButARow(t *testing.T) {
	dc := &recordingBadgeCtx{}

	newStyledView(t).drawMessageCell(datagrid.CellDrawContext{
		Rect:    image.Rect(0, 0, 200, 20),
		Item:    "not a row",
		DrawCtx: dc,
	})

	if len(dc.badges) != 0 || len(dc.texts) != 0 {
		t.Fatal("a cell that holds no commit must stay empty")
	}
}

func TestACommitWithoutAMessageDrawsOnlyItsBadges(t *testing.T) {
	dc := &recordingBadgeCtx{}

	newStyledView(t).drawMessageCell(messageCell(Row{Refs: []Ref{{Name: "main"}}}, dc, 200))

	if len(dc.badges) != 1 || len(dc.texts) != 1 {
		t.Fatalf("badges = %d, texts = %d, want the badge alone", len(dc.badges), len(dc.texts))
	}
}

func TestTheMessageColumnKeepsItsHeaderAndWidth(t *testing.T) {
	v, grid := bound(t)
	grid.Grid.SetColumns(journalColumns())
	before := grid.Grid.Columns()[messageColumnIndex]

	v.installMessageColumn()

	after := grid.Grid.Columns()[messageColumnIndex]
	if after.Header() != before.Header() || after.Width() != before.Width() {
		t.Fatalf("column = %q %v, want the header and width of the one it replaced", after.Header(), after.Width())
	}
	if _, ok := after.(*datagrid.DataGridTemplateColumn); !ok {
		t.Fatalf("column type = %T, want a template column", after)
	}
}

func TestTheBadgeColoursFollowTheTheme(t *testing.T) {
	v := NewView()

	v.Restyle(widget.Win11DarkTheme())
	dark := v.refs

	v.Restyle(widget.Win11LightTheme())

	if v.refs.branch == dark.branch {
		t.Fatal("the branch badge must follow the theme")
	}
}
