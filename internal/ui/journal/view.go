package journal

import (
	"github.com/oops1/headless-gui/v3/widget"
	"github.com/oops1/headless-gui/v3/widget/datagrid"
)

const nearEndRows = 5

type View struct {
	grid      *widget.DataGridWidget
	items     *datagrid.ObservableCollection
	OnSelect  func(Row)
	OnNearEnd func()
}

func NewView() *View {
	return &View{items: datagrid.NewObservableCollection()}
}

func (v *View) Bind(grid *widget.DataGridWidget) {
	v.grid = grid
	grid.Grid.SetItemsSource(v.items)
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
