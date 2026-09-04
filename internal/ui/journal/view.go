package journal

import (
	"github.com/oops1/headless-gui/v3/widget"
	"github.com/oops1/headless-gui/v3/widget/datagrid"
)

const nearEndRows = 5

type View struct {
	grid         *widget.DataGridWidget
	items        *datagrid.ObservableCollection
	authorHeader string
	OnSelect     func(Row)
	OnNearEnd    func()
}

func NewView() *View {
	return &View{items: datagrid.NewObservableCollection()}
}

func (v *View) Bind(grid *widget.DataGridWidget) {
	v.grid = grid
	grid.Grid.ZebraStripes = false
	grid.Grid.RowHeight = rowHeight
	grid.Grid.FontSize = fontSize
	grid.Grid.SetItemsSource(v.items)
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
}

func (v *View) ClearSelection() {
	if v.grid == nil {
		return
	}
	v.grid.Grid.SetSelectedIndex(-1)
}

func (v *View) Append(rows []Row) {
	for _, row := range rows {
		v.items.Add(row)
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
