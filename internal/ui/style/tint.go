package style

import (
	"image/color"

	"github.com/oops1/headless-gui/v3/widget"
)

const (
	surfaceTintPercent    = 5
	controlTintPercent    = 2
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
	for _, surface := range []*color.RGBA{
		&t.WindowBG, &t.PanelBG, &t.TitleBG, &t.DialogBG, &t.DialogTitleBG,
		&t.StatusBarBG, &t.TabBG, &t.TabActiveBG, &t.TabContentBG, &t.MenuBG,
	} {
		*surface = mix(*surface, accent, surfaceTintPercent)
	}
	for _, control := range []*color.RGBA{
		&t.BtnBG, &t.BtnHoverBG, &t.BtnPressedBG, &t.InputBG, &t.DropBG,
		&t.Border, &t.InputBorder, &t.BtnBorder, &t.DropBorder,
	} {
		*control = mix(*control, accent, controlTintPercent)
	}
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
