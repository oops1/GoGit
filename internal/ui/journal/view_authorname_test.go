package journal

import (
	"testing"

	"github.com/oops1/headless-gui/v3/widget/datagrid"
)

func journalColumns() []datagrid.Column {
	return []datagrid.Column{
		datagrid.NewTextColumn("Graph", "Graph"),
		datagrid.NewTextColumn("Message", "Message"),
		newAuthorColumn("Author", false),
		datagrid.NewTextColumn("Date", "Date"),
		datagrid.NewTextColumn("Hash", "ShortHash"),
	}
}

func TestSetFullAuthorNameSwitchesTheAuthorColumnToATextColumn(t *testing.T) {
	v, grid := bound(t)
	grid.Grid.SetColumns(journalColumns())

	v.SetFullAuthorName(true)

	cols := grid.Grid.Columns()
	if _, ok := cols[authorColumnIndex].(*datagrid.DataGridTextColumn); !ok {
		t.Fatalf("author column type = %T, want *datagrid.DataGridTextColumn", cols[authorColumnIndex])
	}
}

func TestSetFullAuthorNameSwitchesTheAuthorColumnBackToTheBadgeTemplate(t *testing.T) {
	v, grid := bound(t)
	grid.Grid.SetColumns(journalColumns())
	v.SetFullAuthorName(true)

	v.SetFullAuthorName(false)

	cols := grid.Grid.Columns()
	if _, ok := cols[authorColumnIndex].(*datagrid.DataGridTemplateColumn); !ok {
		t.Fatalf("author column type = %T, want *datagrid.DataGridTemplateColumn", cols[authorColumnIndex])
	}
}

func TestSetFullAuthorNamePreservesTheColumnHeaderAndWidth(t *testing.T) {
	v, grid := bound(t)
	cols := journalColumns()
	cols[authorColumnIndex].SetWidth(datagrid.PixelWidth(160))
	grid.Grid.SetColumns(cols)

	v.SetFullAuthorName(true)

	got := grid.Grid.Columns()[authorColumnIndex]
	if got.Header() != "Author" {
		t.Fatalf("header = %q, want Author", got.Header())
	}
	if got.Width() != datagrid.PixelWidth(160) {
		t.Fatalf("width = %+v, want 160px", got.Width())
	}
}

func TestSetFullAuthorNameMarksTheGridFullyDirtySoTheJournalRedraws(t *testing.T) {
	v, grid := bound(t)
	grid.Grid.SetColumns(journalColumns())
	grid.Grid.TakeDirty()

	v.SetFullAuthorName(true)

	_, full := grid.Grid.TakeDirty()
	if !full {
		t.Fatal("SetFullAuthorName must mark the grid fully dirty so the journal redraws")
	}
}

func TestSetFullAuthorNameIsANoOpWhenTheViewIsNotBound(t *testing.T) {
	v := NewView()
	v.SetFullAuthorName(true)
}

func TestSetFullAuthorNameIsANoOpWhenTheAuthorColumnIsMissing(t *testing.T) {
	v, grid := bound(t)
	grid.Grid.SetColumns([]datagrid.Column{datagrid.NewTextColumn("Graph", "Graph")})

	v.SetFullAuthorName(true)

	if len(grid.Grid.Columns()) != 1 {
		t.Fatal("column count must stay unchanged when the author column index is out of range")
	}
}
