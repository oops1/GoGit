package journal

import (
	"github.com/oops1/headless-gui/v3/widget/datagrid"
)

const (
	authorColumnIndex     = 2
	authorBadgePaddingX   = 3
	authorBadgePaddingY   = 1
	authorBadgeColumnWide = 160
)

func authorBadgeColumnWidth(rowHeight int) int {
	return badgeFitSize(rowHeight) + 2*authorBadgePaddingX
}

func (v *View) newAuthorColumn(header string, fullName bool) datagrid.Column {
	if fullName {
		return datagrid.NewTextColumn(header, "Author")
	}
	return datagrid.NewTemplateColumn(header, v.drawAuthorBadgeCell)
}

func (v *View) drawAuthorBadgeCell(cdc datagrid.CellDrawContext) {
	row, ok := cdc.Item.(Row)
	if !ok {
		return
	}
	initials := Initials(row.Author)
	if initials == "" || v.sameAuthorAbove(cdc.RowIndex, row.Author) {
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
	textHeight := int(cdc.FontSize * fontHeightRatio)
	cdc.DrawCtx.DrawTextSize(initials, x+(size-textWidth)/2, y+(size-textHeight)/2, cdc.FontSize, textColor)
}

func (v *View) sameAuthorAbove(index int, author string) bool {
	if index <= 0 {
		return false
	}
	previous, ok := v.items.Get(index - 1).(Row)
	return ok && previous.Author == author
}

func badgeFitSize(rowHeight int) int {
	return rowHeight - 2*authorBadgePaddingY
}
