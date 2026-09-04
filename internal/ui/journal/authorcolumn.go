package journal

import (
	"github.com/oops1/headless-gui/v3/widget/datagrid"
)

const (
	authorColumnIndex   = 2
	authorBadgeSize     = 16
	authorBadgePaddingX = 4
)

func newAuthorColumn(header string, fullName bool) datagrid.Column {
	if fullName {
		return datagrid.NewTextColumn(header, "Author")
	}
	return datagrid.NewTemplateColumn(header, drawAuthorBadgeCell)
}

func drawAuthorBadgeCell(cdc datagrid.CellDrawContext) {
	row, ok := cdc.Item.(Row)
	if !ok {
		return
	}
	initials := Initials(row.Author)
	if initials == "" {
		return
	}
	size := badgeFitSize(cdc.Rect.Dy())
	if size <= 0 {
		return
	}
	badge := AuthorBadge(row.Author, size)

	x := cdc.Rect.Min.X + authorBadgePaddingX
	y := cdc.Rect.Min.Y + (cdc.Rect.Dy()-size)/2
	cdc.DrawCtx.DrawImageScaled(badge, x, y, size, size)

	textColor := badgeTextColor(authorColor(row.Author))
	textWidth := cdc.DrawCtx.MeasureText(initials, cdc.FontSize)
	textX := x + (size-textWidth)/2
	textY := cdc.Rect.Min.Y + (cdc.Rect.Dy()-14)/2
	cdc.DrawCtx.DrawTextSize(initials, textX, textY, cdc.FontSize, textColor)
}

func badgeFitSize(rowHeight int) int {
	size := authorBadgeSize
	if avail := rowHeight - 2*authorBadgePaddingX; avail < size {
		size = avail
	}
	return size
}
