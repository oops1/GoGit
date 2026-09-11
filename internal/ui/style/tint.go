package style

import (
	"image/color"

	"github.com/oops1/headless-gui/v3/widget"
)

const (
	selectionDarkPercent  = 55
	selectionLightPercent = 70
)

func Tinted(base *widget.Theme, accent color.RGBA) *widget.Theme {
	t := *base
	t.Accent = accent
	t.ProgressFill = accent
	t.SliderFill = accent
	t.ToggleOnBG = accent
	t.InputCaret = accent
	t.InputFocus = accent
	t.SplitterHoverBG = accent
	t.ListItemSelect = selectionFill(accent, base.LabelText, base.ListItemSelect.A)
	t.DropItemBG = selectionFill(accent, base.DropText, base.DropItemBG.A)
	return &t
}

func selectionFill(accent, text color.RGBA, alpha uint8) color.RGBA {
	fill := mix(accent, color.RGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF}, selectionLightPercent)
	if luminance(text) > brightAccent {
		fill = mix(accent, color.RGBA{A: 0xFF}, selectionDarkPercent)
	}
	fill.A = alpha
	return fill
}
