package journal

import (
	"image/color"
	"strconv"

	"github.com/oops1/headless-gui/v3/widget/datagrid"
)

const (
	authorColumnIndex     = 2
	authorBadgePaddingX   = 3
	authorBadgePaddingY   = 1
	authorBadgeColumnWide = 160
	creditedBadgeGap      = 2
	creditedBadgesShown   = 4
	creditedMorePrefix    = "+"
)

type creditPalette struct {
	fill color.RGBA
	text color.RGBA
}

func authorBadgeColumnWidth(rowHeight int) int {
	return badgeFitSize(rowHeight) + 2*authorBadgePaddingX
}

func authorColumnWidth(rowHeight, credited int) int {
	size := badgeFitSize(rowHeight)
	return authorBadgeColumnWidth(rowHeight) + min(credited, creditedBadgesShown)*(size+creditedBadgeGap)
}

func (v *View) newAuthorColumn(header string, fullName bool) datagrid.Column {
	if fullName {
		return datagrid.NewTextColumn(header, "AuthorLine")
	}
	return datagrid.NewTemplateColumn(header, v.drawAuthorBadgeCell)
}

func (v *View) drawAuthorBadgeCell(cdc datagrid.CellDrawContext) {
	row, ok := cdc.Item.(Row)
	if !ok {
		return
	}
	size := badgeFitSize(cdc.Rect.Dy())
	if size <= 0 {
		return
	}
	x := cdc.Rect.Min.X + authorBadgePaddingX
	y := cdc.Rect.Min.Y + (cdc.Rect.Dy()-size)/2
	if initials := Initials(row.Author); initials != "" && !v.sameAuthorAbove(cdc.RowIndex, row.Author) {
		cdc.DrawCtx.DrawImageScaled(AuthorBadge(row.Author, size), x, y, size, size)
		drawBadgeText(cdc, initials, x, y, size, badgeTextColor(authorColor(row.Author)))
	}
	for i, label := range creditedLabels(row.Credited) {
		left := x + (i+1)*(size+creditedBadgeGap)
		if left+size > cdc.Rect.Max.X {
			return
		}
		cdc.DrawCtx.FillRoundRect(left, y, size, size, badgeCornerRadius, v.credit.fill)
		drawBadgeText(cdc, label, left, y, size, v.credit.text)
	}
}

func creditedLabels(people []string) []string {
	labels := make([]string, 0, min(len(people), creditedBadgesShown))
	for i, person := range people {
		if i == creditedBadgesShown-1 && len(people) > creditedBadgesShown {
			return append(labels, creditedMorePrefix+strconv.Itoa(len(people)-i))
		}
		labels = append(labels, Initials(person))
	}
	return labels
}

func drawBadgeText(cdc datagrid.CellDrawContext, text string, x, y, size int, col color.RGBA) {
	textWidth := cdc.DrawCtx.MeasureText(text, cdc.FontSize)
	textHeight := int(cdc.FontSize * fontHeightRatio)
	cdc.DrawCtx.DrawTextSize(text, x+(size-textWidth)/2, y+(size-textHeight)/2, cdc.FontSize, col)
}

func (v *View) sameAuthorAbove(index int, author string) bool {
	if index <= 0 {
		return false
	}
	previous, ok := v.items.Get(index - 1).(Row)
	return ok && previous.Author == author
}

func (v *View) resizeAuthorColumnLocked() {
	if v.grid == nil || v.fullAuthorName {
		return
	}
	cols := v.grid.Grid.Columns()
	if authorColumnIndex >= len(cols) {
		return
	}
	width := authorColumnWidth(v.grid.Grid.RowHeight, v.mostCreditedVisible())
	cols[authorColumnIndex].SetWidth(datagrid.PixelWidth(float64(width)))
	v.grid.Grid.SetColumns(cols)
}

func (v *View) mostCreditedVisible() int {
	first := v.grid.Grid.FirstVisibleRow()
	last := min(first+v.grid.Grid.VisibleRowCount(), v.items.Count())
	most := 0
	for at := max(first, 0); at < last; at++ {
		if row, ok := v.items.Get(at).(Row); ok {
			most = max(most, len(row.Credited))
		}
	}
	return most
}

func badgeFitSize(rowHeight int) int {
	return rowHeight - 2*authorBadgePaddingY
}
