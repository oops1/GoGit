package settings

import (
	"github.com/oops1/headless-gui/v3/widget"
)

type expanderRow struct {
	grid     *widget.Grid
	row      int
	expander *widget.Expander
	expanded float64
}

func (v *View) expanderRows() []expanderRow {
	return []expanderRow{
		{v.sectionGit, gitAdvancedRow, v.gitAdvanced, advancedRowExpanded},
	}
}

func (v *View) applyExpanderRow(e expanderRow) {
	height := float64(expanderRowCollapsed)
	if e.expander.IsExpanded {
		height = e.expanded
	}
	e.grid.RowDefs[e.row].Value = height
	for _, s := range v.searchSections {
		if s.grid == e.grid {
			s.originalRows[e.row].Value = height
		}
	}
	e.grid.SetBounds(e.grid.Bounds())
	v.syncScroll()
}

func (v *View) wireExpanders() {
	for _, e := range v.expanderRows() {
		row := e
		v.applyExpanderRow(row)
		row.expander.OnExpandedChanged = func(bool) { v.applyExpanderRow(row) }
	}
}
