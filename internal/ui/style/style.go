package style

import (
	"image/color"

	"github.com/oops1/headless-gui/v3/widget"
)

const (
	FieldPaddingX = 12

	accentHoverShift   = 12
	accentPressedShift = 24
	brightAccent       = 150
	onBrightAccent     = 0x1A
	bannerTint         = 18
	darkField          = 128
)

var (
	addedOnLight   = color.RGBA{R: 0x1A, G: 0x7F, B: 0x37, A: 0xFF}
	deletedOnLight = color.RGBA{R: 0xCF, G: 0x22, B: 0x2E, A: 0xFF}
	addedOnDark    = color.RGBA{R: 0x3F, G: 0xB9, B: 0x50, A: 0xFF}
	deletedOnDark  = color.RGBA{R: 0xF8, G: 0x51, B: 0x49, A: 0xFF}
)

type Palette struct {
	Surface       color.RGBA
	Chrome        color.RGBA
	Field         color.RGBA
	Border        color.RGBA
	Text          color.RGBA
	Secondary     color.RGBA
	Accent        color.RGBA
	AccentHover   color.RGBA
	AccentPressed color.RGBA
	OnAccent      color.RGBA
}

func Of(t *widget.Theme) Palette {
	return Palette{
		Surface:       t.DialogBG,
		Chrome:        t.PanelBG,
		Field:         t.InputBG,
		Border:        t.InputBorder,
		Text:          t.LabelText,
		Secondary:     t.SecondaryText,
		Accent:        t.Accent,
		AccentHover:   shift(t.Accent, accentHoverShift),
		AccentPressed: shift(t.Accent, accentPressedShift),
		OnAccent:      onAccent(t.Accent),
	}
}

func (p Palette) Fields(inputs ...*widget.TextInput) {
	for _, input := range inputs {
		input.Background = p.Field
		input.BorderColor = p.Border
		input.TextColor = p.Text
		input.PlaceColor = p.Secondary
		input.PaddingX = FieldPaddingX
	}
}

func (p Palette) Areas(boxes ...*widget.TextBox) {
	for _, box := range boxes {
		box.Background = p.Field
		box.BorderColor = p.Border
		box.TextColor = p.Text
		box.PlaceColor = p.Secondary
		box.PaddingX = FieldPaddingX
	}
}

func (p Palette) Lists(dropdowns ...*widget.Dropdown) {
	for _, dropdown := range dropdowns {
		dropdown.Background = p.Field
		dropdown.BorderColor = p.Border
		dropdown.TextColor = p.Text
		dropdown.ArrowColor = p.Text
		dropdown.PaddingX = FieldPaddingX
	}
}

func (p Palette) Primary(buttons ...*widget.Button) {
	for _, button := range buttons {
		button.Background = p.Accent
		button.BorderColor = p.Accent
		button.HoverBG = p.AccentHover
		button.PressedBG = p.AccentPressed
		button.TextColor = p.OnAccent
	}
}

func (p Palette) Quiet(buttons ...*widget.Button) {
	for _, button := range buttons {
		button.Background = p.Field
		button.BorderColor = p.Border
		button.TextColor = p.Text
	}
}

func (p Palette) Body(labels ...*widget.Label) {
	for _, label := range labels {
		label.TextColor = p.Text
	}
}

func (p Palette) Tint(percent int) color.RGBA {
	return mix(p.Field, p.Accent, percent)
}

func (p Palette) Added() color.RGBA {
	if luminance(p.Field) < darkField {
		return addedOnDark
	}
	return addedOnLight
}

func (p Palette) Deleted() color.RGBA {
	if luminance(p.Field) < darkField {
		return deletedOnDark
	}
	return deletedOnLight
}

func (p Palette) Hints(labels ...*widget.Label) {
	for _, label := range labels {
		label.TextColor = p.Secondary
	}
}

func (p Palette) Banner(panel *widget.DockPanel, labels ...*widget.Label) {
	panel.Background = mix(p.Chrome, p.Accent, bannerTint)
	panel.UseAlpha = false
	p.Body(labels...)
}

func onAccent(accent color.RGBA) color.RGBA {
	if luminance(accent) > brightAccent {
		return color.RGBA{R: onBrightAccent, G: onBrightAccent, B: onBrightAccent, A: 0xFF}
	}
	return color.RGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF}
}

func shift(c color.RGBA, percent int) color.RGBA {
	if luminance(c) > brightAccent {
		return mix(c, color.RGBA{A: 0xFF}, percent)
	}
	return mix(c, color.RGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF}, percent)
}

func mix(from, to color.RGBA, percent int) color.RGBA {
	channel := func(a, b uint8) uint8 {
		return uint8((int(a)*(100-percent) + int(b)*percent) / 100)
	}
	return color.RGBA{
		R: channel(from.R, to.R),
		G: channel(from.G, to.G),
		B: channel(from.B, to.B),
		A: from.A,
	}
}

func luminance(c color.RGBA) int {
	return (299*int(c.R) + 587*int(c.G) + 114*int(c.B)) / 1000
}
