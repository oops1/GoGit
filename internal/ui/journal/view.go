package journal

import (
	"github.com/oops1/headless-gui/v3/widget"
	"github.com/oops1/headless-gui/v3/widget/datagrid"

	"github.com/oops1/gogit/internal/ui/journal/graph"
)

const nearEndRows = 5

type View struct {
	grid         *widget.DataGridWidget
	items        *datagrid.ObservableCollection
	authorHeader string
	lanes        *graph.Layout
	widest       int
	OnSelect     func(Row)
	OnNearEnd    func()
}

func NewView() *View {
	return &View{items: datagrid.NewObservableCollection(), lanes: graph.New()}
}

func (v *View) Bind(grid *widget.DataGridWidget) {
	v.grid = grid
	grid.Grid.ZebraStripes = false
	grid.Grid.RowHeight = rowHeight
	grid.Grid.FontSize = fontSize
	grid.Grid.SetItemsSource(v.items)
	v.installGraphColumn()
	v.SetFullAuthorName(false)
	grid.Grid.OnSelectionChanged = func(e datagrid.SelectionChangedEvent) {
		row, ok := e.SelectedItem.(Row)
		if !ok {
			return
		}
		if v.OnSelect != nil {
			v.OnSelect(row)
		}
		if v.OnNearEnd != nil && v.items.Count()-e.SelectedIndex <= nearEndRows {
			v.OnNearEnd()
		}
	}
}

func (v *View) Reset() {
	v.items.Clear()
	v.lanes.Reset()
	v.widest = 0
	v.resizeGraphColumn()
}

func (v *View) installGraphColumn() {
	cols := v.grid.Grid.Columns()
	if graphColumnIndex >= len(cols) {
		return
	}
	cols[graphColumnIndex] = v.newGraphColumn()
	v.grid.Grid.SetColumns(cols)
	v.resizeGraphColumn()
}

func (v *View) resizeGraphColumn() {
	if v.grid == nil {
		return
	}
	cols := v.grid.Grid.Columns()
	if graphColumnIndex >= len(cols) {
		return
	}
	cols[graphColumnIndex].SetWidth(datagrid.PixelWidth(float64(graphColumnWidth(v.widest))))
	v.grid.Grid.SetColumns(cols)
}

func (v *View) ClearSelection() {
	if v.grid == nil {
		return
	}
	v.grid.Grid.SetSelectedIndex(-1)
}

func (v *View) Append(rows []Row) {
	grew := false
	for _, row := range rows {
		row.Graph = v.lanes.Add(graph.Commit{ID: row.ID, Parents: row.Parents})
		if row.Graph.Lanes > v.widest {
			v.widest = row.Graph.Lanes
			grew = true
		}
		v.items.Add(row)
	}
	if grew {
		v.resizeGraphColumn()
	}
}

func (v *View) Count() int {
	return v.items.Count()
}

func (v *View) EnsureLoaded(count int) {
	if v.OnNearEnd != nil && v.items.Count() < count {
		v.OnNearEnd()
	}
}

func (v *View) SetFullAuthorName(fullName bool) {
	if v.grid == nil {
		return
	}
	cols := v.grid.Grid.Columns()
	if authorColumnIndex >= len(cols) {
		return
	}
	old := cols[authorColumnIndex]
	if old.Header() != "" {
		v.authorHeader = old.Header()
	}
	header := ""
	if fullName {
		header = v.authorHeader
	}
	col := v.newAuthorColumn(header, fullName)
	if fullName {
		col.SetWidth(datagrid.PixelWidth(authorBadgeColumnWide))
	} else {
		col.SetWidth(datagrid.PixelWidth(float64(authorBadgeColumnWidth(v.grid.Grid.RowHeight))))
	}
	cols[authorColumnIndex] = col
	v.grid.Grid.SetColumns(cols)
}

const (
	rowHeight       = 20
	fontSize        = 9.0
	fontHeightRatio = 1.4
)
