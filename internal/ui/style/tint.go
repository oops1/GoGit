package style

import (
	"image/color"

	"github.com/oops1/headless-gui/v3/widget"
)

const (
	surfaceTintPercent = 5
	controlTintPercent = 2
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
	t.ListItemSelect = keepAlpha(accent, base.ListItemSelect)
	t.DropItemBG = keepAlpha(accent, base.DropItemBG)
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

func keepAlpha(accent, was color.RGBA) color.RGBA {
	accent.A = was.A
	return accent
}
