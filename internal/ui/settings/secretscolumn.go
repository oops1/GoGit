package settings

import (
	"github.com/oops1/headless-gui/v3/widget"
	"github.com/oops1/headless-gui/v3/widget/datagrid"
)

func statusOfItem(item interface{}) (SecretStatus, bool) {
	switch e := item.(type) {
	case SecretEntry:
		return e.Status, true
	case KeyEntry:
		return e.Status, true
	}
	return StatusSaved, false
}

func drawSecretStatusCell(cdc datagrid.CellDrawContext) {
	status, ok := statusOfItem(cdc.Item)
	if !ok {
		return
	}
	dotColor := status.DotColor(widget.CurrentTheme())

	r := cdc.Rect
	cy := r.Min.Y + r.Dy()/2
	cx := r.Min.X + statusCellPadding + statusDotDiameter/2
	cdc.DrawCtx.FillEllipseAA(cx, cy, statusDotDiameter/2, statusDotDiameter/2, dotColor)

	textX := cx + statusDotDiameter/2 + statusCellTextGap
	textY := r.Min.Y + (r.Dy()-statusTextHeight)/2
	maxWidth := r.Max.X - textX - statusCellPadding
	label := datagrid.EllipsizeText(cdc.DrawCtx, status.Label(), maxWidth, cdc.FontSize)
	cdc.DrawCtx.DrawTextSize(label, textX, textY, cdc.FontSize, cdc.TextColor)
}
