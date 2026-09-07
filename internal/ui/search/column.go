package search

import (
	"github.com/oops1/headless-gui/v3/widget"
	"github.com/oops1/headless-gui/v3/widget/datagrid"

	"github.com/oops1/gogit/internal/i18n"
)

const (
	pathColumnMinWidth = 200
	kindColumnMinWidth = 120
	cellPadding        = 6
	cellTextHeight     = 14
)

func (v *View) buildColumns() {
	pathCol := datagrid.NewTextColumn(i18n.T("Dialog.Search.Column.Path"), "Path")
	pathCol.SetWidth(datagrid.StarWidth(2))
	pathCol.SetMinWidth(pathColumnMinWidth)

	kindCol := datagrid.NewTemplateColumn(i18n.T("Dialog.Search.Column.Kind"), drawKindCell)
	kindCol.SetWidth(datagrid.StarWidth(1))
	kindCol.SetMinWidth(kindColumnMinWidth)

	v.resultsTable.Grid.SetColumns([]datagrid.Column{pathCol, kindCol})
	v.resultsTable.Grid.EmptyStateText = i18n.T("Dialog.Search.Empty")
	v.resultsTable.Grid.EmptyStateColor = widget.CurrentTheme().SecondaryText
}

func kindLabel(f Found) string {
	switch {
	case f.Worktree:
		return i18n.T("Dialog.Search.Kind.Worktree")
	case f.Bare:
		return i18n.T("Dialog.Search.Kind.Bare")
	default:
		return i18n.T("Dialog.Search.Kind.Repository")
	}
}

func drawKindCell(cdc datagrid.CellDrawContext) {
	f, ok := cdc.Item.(Found)
	if !ok {
		return
	}
	label := kindLabel(f)
	r := cdc.Rect
	textX := r.Min.X + cellPadding
	textY := r.Min.Y + (r.Dy()-cellTextHeight)/2
	maxWidth := r.Max.X - textX - cellPadding
	shown := datagrid.EllipsizeText(cdc.DrawCtx, label, maxWidth, cdc.FontSize)
	cdc.DrawCtx.DrawTextSize(shown, textX, textY, cdc.FontSize, cdc.TextColor)
}
