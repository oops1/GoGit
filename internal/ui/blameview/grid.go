package blameview

import (
	"image/color"
	"sync"

	"github.com/oops1/headless-gui/v3/widget"
)

const gridTag = "BlameGrid"

var registerGrid = sync.OnceFunc(func() {
	widget.RegisterXAMLWidget(gridTag, func(widget.XAMLAttrs) (widget.Widget, error) { return newCodeGrid(), nil })
})

type codeGrid struct {
	*widget.DataGridWidget
}

func newCodeGrid() *codeGrid {
	dg := widget.NewDataGridWidget()
	dg.Grid.IsReadOnly = true
	dg.Grid.CanUserSortColumns = false
	dg.Grid.ZebraStripes = false
	dg.Grid.RowHeight = rowHeight
	dg.Grid.FontSize = fontSize
	return &codeGrid{DataGridWidget: dg}
}

func (g *codeGrid) ApplyTheme(t *widget.Theme) {
	g.DataGridWidget.ApplyTheme(t)
	g.paint(t.InputBG)
}

func (g *codeGrid) paint(bg color.RGBA) {
	g.Grid.Background = bg
	g.Grid.AlternateBG = bg
	g.Grid.GridLineColor = bg
}
