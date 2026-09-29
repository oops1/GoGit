package settings

import (
	"image/color"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/config"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/style"
)

const (
	colorAccentRow    = 31
	colorSurfaceRow   = 33
	colorFieldRow     = 35
	colorTextRow      = 37
	colorSecondaryRow = 39
	colorsPreviewRow  = 41

	colorPickerHintGap      = 8
	colorPreviewFieldWidth  = 160
	colorPreviewFieldHeight = 40
	colorPreviewAccentWidth = 56
	colorPreviewPad         = 12
	colorPreviewTextInset   = 14
	colorPreviewCaptionGap  = 6
)

type colorRow struct {
	picker *widget.ColorPicker
	chosen bool
	from   func(style.Palette) color.RGBA
}

func (v *View) colorRows() []*colorRow {
	return []*colorRow{
		{picker: v.colorAccent, chosen: v.colorChosen[0], from: func(p style.Palette) color.RGBA { return p.Accent }},
		{picker: v.colorSurface, chosen: v.colorChosen[1], from: func(p style.Palette) color.RGBA { return p.Surface }},
		{picker: v.colorField, chosen: v.colorChosen[2], from: func(p style.Palette) color.RGBA { return p.Field }},
		{picker: v.colorText, chosen: v.colorChosen[3], from: func(p style.Palette) color.RGBA { return p.Text }},
		{picker: v.colorSecondary, chosen: v.colorChosen[4], from: func(p style.Palette) color.RGBA { return p.Secondary }},
	}
}

func (v *View) buildColorPreview() {
	v.colorsPreview = widget.NewSwatch(color.RGBA{})
	v.colorsPreview.SetGridProps(colorsPreviewRow, 0, 1, 3)
	v.sectionGeneral.AddChild(v.colorsPreview)

	v.colorsPreviewField = widget.NewSwatch(color.RGBA{})
	v.colorsPreviewField.CornerRadius = 4
	v.colorsPreviewField.SetGridProps(colorsPreviewRow, 0, 1, 3)
	v.colorsPreviewField.SetHAlign(widget.HAlignLeft)
	v.colorsPreviewField.SetVAlign(widget.VAlignTop)
	v.colorsPreviewField.SetXAMLSize(colorPreviewFieldWidth, colorPreviewFieldHeight)
	v.colorsPreviewField.SetMargin(widget.Margin{Left: colorPreviewPad, Top: colorPreviewPad})
	v.sectionGeneral.AddChild(v.colorsPreviewField)

	v.colorsPreviewText = widget.NewLabel(i18n.T("Dialog.Settings.Colors.Preview.Field"), color.RGBA{A: 0xFF})
	v.colorsPreviewText.SetGridProps(colorsPreviewRow, 0, 1, 3)
	v.colorsPreviewText.SetHAlign(widget.HAlignLeft)
	v.colorsPreviewText.SetVAlign(widget.VAlignTop)
	v.colorsPreviewText.SetXAMLSize(colorPreviewFieldWidth-2*colorPreviewTextInset, hintLineHeight)
	v.colorsPreviewText.SetMargin(widget.Margin{
		Left: colorPreviewPad + colorPreviewTextInset,
		Top:  colorPreviewPad + (colorPreviewFieldHeight-hintLineHeight)/2,
	})
	v.sectionGeneral.AddChild(v.colorsPreviewText)

	v.colorsPreviewSecond = widget.NewLabel(i18n.T("Dialog.Settings.Colors.Preview.Secondary"), color.RGBA{A: 0xFF})
	v.colorsPreviewSecond.FontSize = hintFontSize
	v.colorsPreviewSecond.SetGridProps(colorsPreviewRow, 0, 1, 3)
	v.colorsPreviewSecond.SetHAlign(widget.HAlignLeft)
	v.colorsPreviewSecond.SetVAlign(widget.VAlignTop)
	v.colorsPreviewSecond.SetXAMLSize(colorPreviewFieldWidth, hintLineHeight)
	v.colorsPreviewSecond.SetMargin(widget.Margin{
		Left: colorPreviewPad,
		Top:  colorPreviewPad + colorPreviewFieldHeight + colorPreviewCaptionGap,
	})
	v.sectionGeneral.AddChild(v.colorsPreviewSecond)

	v.colorsPreviewAccent = widget.NewSwatch(color.RGBA{})
	v.colorsPreviewAccent.CornerRadius = 4
	v.colorsPreviewAccent.SetGridProps(colorsPreviewRow, 0, 1, 3)
	v.colorsPreviewAccent.SetHAlign(widget.HAlignRight)
	v.colorsPreviewAccent.SetVAlign(widget.VAlignTop)
	v.colorsPreviewAccent.SetXAMLSize(colorPreviewAccentWidth, colorPreviewFieldHeight)
	v.colorsPreviewAccent.SetMargin(widget.Margin{Right: colorPreviewPad, Top: colorPreviewPad})
	v.sectionGeneral.AddChild(v.colorsPreviewAccent)
}

func addColorHint(grid *widget.Grid, key string, row, maxLines int) *hintLabel {
	h := addHint(grid, key, row, 2, hintFieldRowSpan, 1)
	h.WrapText = true
	h.SetXAMLSize(0, maxLines*hintLineHeight)
	h.SetVAlign(widget.VAlignCenter)
	h.SetMargin(widget.Margin{Left: colorPickerHintGap})
	return h
}

func (v *View) colorPickers() []*widget.ColorPicker {
	return []*widget.ColorPicker{v.colorAccent, v.colorSurface, v.colorField, v.colorText, v.colorSecondary}
}

func (v *View) wireColorPickers(onAny func()) {
	for at, picker := range v.colorPickers() {
		picker.OnChanged = func(color.RGBA) {
			v.colorChosen[at] = true
			v.refreshColorsPreview()
			onAny()
		}
	}
	v.colorsReset.OnClick = func() {
		for at := range v.colorChosen {
			v.colorChosen[at] = false
		}
		v.showThemeColors()
		onAny()
	}
}

func (v *View) applyColors(colors config.Colors) {
	for at, text := range []string{colors.Accent, colors.Surface, colors.Field, colors.Text, colors.Secondary} {
		chosen, ok := style.ParseColor(text)
		v.colorChosen[at] = ok
		if ok {
			v.colorPickers()[at].SetValueQuiet(chosen)
		}
	}
	v.showThemeColors()
}

func (v *View) chosenColors() config.Colors {
	values := make([]string, len(v.colorChosen))
	for at, picker := range v.colorPickers() {
		if v.colorChosen[at] {
			values[at] = style.HexColor(picker.Value())
		}
	}
	return config.Colors{
		Accent:    values[0],
		Surface:   values[1],
		Field:     values[2],
		Text:      values[3],
		Secondary: values[4],
	}
}

func (v *View) restyleColors(p style.Palette) {
	v.colorsPreview.Border = p.Border
	v.colorsPreviewField.Border = p.Border
	v.colorsPreviewAccent.Border = p.Border
	v.showThemeColors()
}

func (v *View) showThemeColors() {
	palette := style.Of(v.themeOrCurrent())
	for _, row := range v.colorRows() {
		if !row.chosen {
			row.picker.SetValueQuiet(row.from(palette))
		}
	}
	v.refreshColorsPreview()
}

func (v *View) themeOrCurrent() *widget.Theme {
	if v.currentTheme != nil {
		return v.currentTheme
	}
	return widget.CurrentTheme()
}

func (v *View) refreshColorsPreview() {
	v.colorsPreview.Color = v.colorSurface.Value()
	v.colorsPreviewField.Color = v.colorField.Value()
	v.colorsPreviewText.TextColor = v.colorText.Value()
	v.colorsPreviewSecond.TextColor = v.colorSecondary.Value()
	v.colorsPreviewAccent.Color = v.colorAccent.Value()
	v.colorsPreview.Invalidate()
	v.colorsPreviewField.Invalidate()
	v.colorsPreviewText.Invalidate()
	v.colorsPreviewSecond.Invalidate()
	v.colorsPreviewAccent.Invalidate()
}
