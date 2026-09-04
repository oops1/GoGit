package filesgrid

import (
	"image"

	"github.com/oops1/headless-gui/v3/widget/datagrid"

	"github.com/oops1/gogit/internal/ui/changes"
	"github.com/oops1/gogit/internal/ui/icons"
)

const (
	nameIconSize      = 16
	nameIconPaddingX  = 4
	nameTextGap       = 4
	namePlainPaddingX = 6
)

type pendingIcon struct {
	rect image.Rectangle
	img  image.Image
}

func (g *Grid) drawNameCell(cdc datagrid.CellDrawContext) {
	row, ok := cdc.Item.(changes.Row)
	if !ok {
		g.drawPlainNameText(cdc, plainCellText(cdc.Item))
		return
	}

	textX := cdc.Rect.Min.X + namePlainPaddingX
	img := icons.Status(string(row.Status), nameIconSize)
	if img != nil {
		iconY := cdc.Rect.Min.Y + (cdc.Rect.Dy()-nameIconSize)/2
		rect := image.Rect(
			cdc.Rect.Min.X+nameIconPaddingX, iconY,
			cdc.Rect.Min.X+nameIconPaddingX+nameIconSize, iconY+nameIconSize,
		)
		g.mu.Lock()
		g.pendingIcons = append(g.pendingIcons, pendingIcon{rect: rect, img: img})
		g.mu.Unlock()
		textX = cdc.Rect.Min.X + nameIconPaddingX + nameIconSize + nameTextGap
	}

	drawCellText(cdc, row.Name, textX)
}

func (g *Grid) drawPlainNameText(cdc datagrid.CellDrawContext, text string) {
	drawCellText(cdc, text, cdc.Rect.Min.X+namePlainPaddingX)
}

func drawCellText(cdc datagrid.CellDrawContext, text string, textX int) {
	if text == "" {
		return
	}
	textY := cdc.Rect.Min.Y + (cdc.Rect.Dy()-14)/2
	cdc.DrawCtx.DrawTextSize(text, textX, textY, cdc.FontSize, cdc.TextColor)
}

func plainCellText(item interface{}) string {
	value, ok := datagrid.GetPropertyValue(item, "Name")
	if !ok {
		return ""
	}
	text, _ := value.(string)
	return text
}
