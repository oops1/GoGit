package journal

import (
	"image/color"

	"github.com/oops1/headless-gui/v3/widget"
	"github.com/oops1/headless-gui/v3/widget/datagrid"

	"github.com/oops1/gogit/internal/ui/journal/graph"
)

const nearEndRows = 5

type View struct {
	grid           *widget.DataGridWidget
	items          *datagrid.ObservableCollection
	authorHeader   string
	fullAuthorName bool
	lanes          *graph.Layout
	refs           refPalette
	credit         creditPalette
	OnSelect       func(Row)
	OnNearEnd      func()
}

func NewView() *View {
	return &View{
		items:  datagrid.NewObservableCollection(),
		lanes:  graph.New(),
		refs:   paletteOf(widget.CurrentTheme()),
		credit: creditOf(widget.CurrentTheme()),
	}
}

func (v *View) Bind(grid *widget.DataGridWidget) {
	v.grid = grid
	grid.Grid.ZebraStripes = false
	grid.Grid.RowHeight = rowHeight
	grid.Grid.FontSize = fontSize
	grid.Grid.SetItemsSource(v.items)
	v.Restyle(widget.CurrentTheme())
	v.installGraphColumn()
	v.installMessageColumn()
	v.SetFullAuthorName(false)
	grid.Grid.OnScroll = func(int, int) { v.resizeToVisibleRows() }
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

func (v *View) Restyle(t *widget.Theme) {
	v.refs = paletteOf(t)
	v.credit = creditOf(t)
	if v.grid == nil {
		return
	}
	v.grid.Grid.GridLineColor = color.RGBA{}
}

func (v *View) installMessageColumn() {
	cols := v.grid.Grid.Columns()
	if messageColumnIndex >= len(cols) {
		return
	}
	old := cols[messageColumnIndex]
	fresh := v.newMessageColumn(old.Header())
	fresh.SetWidth(old.Width())
	cols[messageColumnIndex] = fresh
	v.grid.Grid.SetColumns(cols)
}

func (v *View) Reset() {
	v.items.Clear()
	v.lanes.Reset()
	v.resizeToVisibleRows()
}

func (v *View) resizeToVisibleRows() {
	v.resizeGraphColumn()
	v.resizeAuthorColumn()
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
	cols[graphColumnIndex].SetWidth(datagrid.PixelWidth(float64(graphColumnWidth(v.visibleLanes()))))
	v.grid.Grid.SetColumns(cols)
}

func (v *View) visibleLanes() int {
	first := v.grid.Grid.FirstVisibleRow()
	last := min(first+v.grid.Grid.VisibleRowCount(), v.items.Count())
	widest := 0
	for at := max(first, 0); at < last; at++ {
		row, ok := v.items.Get(at).(Row)
		if !ok {
			continue
		}
		widest = max(widest, row.Graph.Lanes)
	}
	return widest
}

func (v *View) ClearSelection() {
	if v.grid == nil {
		return
	}
	v.grid.Grid.SetSelectedIndex(-1)
}

func (v *View) Append(rows []Row) {
	for _, row := range rows {
		row.Graph = v.lanes.Add(graph.Commit{ID: row.ID, Parents: row.Parents})
		v.items.Add(row)
	}
	v.resizeToVisibleRows()
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
	v.fullAuthorName = fullName
	col := v.newAuthorColumn(header, fullName)
	col.SetWidth(datagrid.PixelWidth(authorBadgeColumnWide))
	cols[authorColumnIndex] = col
	v.grid.Grid.SetColumns(cols)
	v.resizeAuthorColumn()
}

const (
	rowHeight       = 20
	fontSize        = 9.0
	fontHeightRatio = 1.4
)
