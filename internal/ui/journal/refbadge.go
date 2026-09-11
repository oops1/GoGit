package journal

import (
	"image/color"

	"github.com/oops1/headless-gui/v3/widget"
	"github.com/oops1/headless-gui/v3/widget/datagrid"
)

const (
	messageColumnIndex = 1
	refBadgeHeight     = 14
	refBadgeRadius     = 4
	refBadgePaddingX   = 5
	refBadgeGap        = 5
	refBadgeShareNum   = 3
	refBadgeShareDen   = 4
	refTextInset       = 6
	refTextHeight      = 14
)

type refPalette struct {
	head       color.RGBA
	branch     color.RGBA
	remote     color.RGBA
	tag        color.RGBA
	onFill     color.RGBA
	onSoftFill color.RGBA
}

func paletteOf(t *widget.Theme) refPalette {
	return refPalette{
		head:       t.Accent,
		branch:     tintOf(t.PanelBG, t.Accent),
		remote:     tintOf(t.PanelBG, t.SecondaryText),
		tag:        badgePalette[2],
		onFill:     color.RGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF},
		onSoftFill: t.LabelText,
	}
}

func creditOf(t *widget.Theme) creditPalette {
	return creditPalette{fill: tintOf(t.PanelBG, t.SecondaryText), text: t.LabelText}
}

func tintOf(base, accent color.RGBA) color.RGBA {
	const percent = 30
	mix := func(b, a uint8) uint8 {
		return uint8((int(b)*(100-percent) + int(a)*percent) / 100)
	}
	return color.RGBA{R: mix(base.R, accent.R), G: mix(base.G, accent.G), B: mix(base.B, accent.B), A: 0xFF}
}

func (p refPalette) fillOf(ref Ref) (color.RGBA, color.RGBA) {
	switch {
	case ref.Head:
		return p.head, p.onFill
	case ref.Kind == RefTag:
		return p.tag, badgeTextColor(p.tag)
	case ref.Kind == RefRemote:
		return p.remote, p.onSoftFill
	default:
		return p.branch, p.onSoftFill
	}
}

func (v *View) newMessageColumn(header string) datagrid.Column {
	return datagrid.NewTemplateColumn(header, v.drawMessageCell)
}

func (v *View) drawMessageCell(cdc datagrid.CellDrawContext) {
	row, ok := cdc.Item.(Row)
	if !ok {
		return
	}
	left := cdc.Rect.Min.X + refTextInset
	limit := cdc.Rect.Min.X + cdc.Rect.Dx()*refBadgeShareNum/refBadgeShareDen
	for _, ref := range row.Refs {
		width := badgeWidth(cdc, ref)
		if left+width > limit {
			break
		}
		v.drawRefBadge(cdc, ref, left, width)
		left += width + refBadgeGap
	}
	if row.Message == "" {
		return
	}
	text := datagrid.EllipsizeText(cdc.DrawCtx, row.Message, cdc.Rect.Max.X-refTextInset-left, cdc.FontSize)
	cdc.DrawCtx.DrawTextSize(text, left, textTop(cdc.Rect.Min.Y, cdc.Rect.Dy()), cdc.FontSize, cdc.TextColor)
}

func badgeWidth(cdc datagrid.CellDrawContext, ref Ref) int {
	return cdc.DrawCtx.MeasureText(ref.Name, cdc.FontSize) + 2*refBadgePaddingX
}

func (v *View) drawRefBadge(cdc datagrid.CellDrawContext, ref Ref, left, width int) {
	fill, text := v.refs.fillOf(ref)
	top := cdc.Rect.Min.Y + (cdc.Rect.Dy()-refBadgeHeight)/2
	cdc.DrawCtx.FillRoundRect(left, top, width, refBadgeHeight, refBadgeRadius, fill)
	cdc.DrawCtx.DrawTextSize(ref.Name, left+refBadgePaddingX, textTop(cdc.Rect.Min.Y, cdc.Rect.Dy()), cdc.FontSize, text)
}

func textTop(top, height int) int {
	return top + (height-refTextHeight)/2
}
