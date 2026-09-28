package settings

import (
	"image/color"

	"github.com/oops1/headless-gui/v3/widget"

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

	colorSampleSize         = 18
	colorSampleHintGap      = 4
	colorPreviewFieldWidth  = 160
	colorPreviewFieldHeight = 40
	colorPreviewAccentWidth = 56
	colorPreviewPad         = 12
	colorPreviewTextInset   = 14
	colorPreviewCaptionGap  = 6
)

func (v *View) buildColorSamples() {
	v.colorAccentSample = v.addColorSample(colorAccentRow)
	v.colorSurfaceSample = v.addColorSample(colorSurfaceRow)
	v.colorFieldSample = v.addColorSample(colorFieldRow)
	v.colorTextSample = v.addColorSample(colorTextRow)
	v.colorSecondarySample = v.addColorSample(colorSecondaryRow)

	v.colorsPreview = newSwatchPanel()
	v.colorsPreview.SetGridProps(colorsPreviewRow, 0, 1, 3)
	v.sectionGeneral.AddChild(v.colorsPreview)

	v.colorsPreviewField = newSwatchPanel()
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

	v.colorsPreviewAccent = newSwatchPanel()
	v.colorsPreviewAccent.CornerRadius = 4
	v.colorsPreviewAccent.SetGridProps(colorsPreviewRow, 0, 1, 3)
	v.colorsPreviewAccent.SetHAlign(widget.HAlignRight)
	v.colorsPreviewAccent.SetVAlign(widget.VAlignTop)
	v.colorsPreviewAccent.SetXAMLSize(colorPreviewAccentWidth, colorPreviewFieldHeight)
	v.colorsPreviewAccent.SetMargin(widget.Margin{Right: colorPreviewPad, Top: colorPreviewPad})
	v.sectionGeneral.AddChild(v.colorsPreviewAccent)

	v.sectionGeneral.SetBounds(v.sectionGeneral.Bounds())
}

func newSwatchPanel() *widget.Panel {
	p := widget.NewPanel(color.RGBA{})
	p.ShowHeader = false
	p.ShowBorder = true
	p.CornerRadius = 6
	return p
}

func (v *View) addColorSample(row int) *widget.Panel {
	p := widget.NewPanel(color.RGBA{})
	p.ShowHeader = false
	p.ShowBorder = true
	p.CornerRadius = 3
	p.SetGridProps(row, 2, 1, 1)
	p.SetHAlign(widget.HAlignLeft)
	p.SetVAlign(widget.VAlignCenter)
	p.SetXAMLSize(colorSampleSize, colorSampleSize)
	v.sectionGeneral.AddChild(p)
	return p
}

func addColorHint(grid *widget.Grid, key string, row, maxLines int) *hintLabel {
	h := addHint(grid, key, row, 2, hintFieldRowSpan, 1)
	h.WrapText = true
	h.SetXAMLSize(0, maxLines*hintLineHeight)
	h.SetVAlign(widget.VAlignCenter)
	h.SetMargin(widget.Margin{Left: colorSampleSize + hintLeftGapFromControl + colorSampleHintGap})
	return h
}

func (v *View) colorInputs() []*widget.TextInput {
	return []*widget.TextInput{v.colorAccent, v.colorSurface, v.colorField, v.colorText, v.colorSecondary}
}

func (v *View) colorSamples() []*widget.Panel {
	return []*widget.Panel{v.colorAccentSample, v.colorSurfaceSample, v.colorFieldSample, v.colorTextSample, v.colorSecondarySample}
}

func (v *View) wireColorInputs(onAny func()) {
	for _, input := range v.colorInputs() {
		input.OnChange = func(string) {
			onAny()
			v.refreshColorSamples()
		}
	}
	v.colorsReset.OnClick = func() {
		for _, input := range v.colorInputs() {
			input.SetText("")
		}
		v.refreshColorSamples()
		onAny()
	}
}

func (v *View) restyleColors(p style.Palette) {
	for _, input := range v.colorInputs() {
		input.ErrorBorder = p.DeletedText
	}
	for _, sample := range v.colorSamples() {
		sample.BorderColor = p.Border
	}
	v.colorsPreview.BorderColor = p.Border
	v.colorsPreviewField.BorderColor = p.Border
	v.colorsPreviewAccent.BorderColor = p.Border
	v.refreshColorSamples()
}

func (v *View) refreshColorSamples() {
	theme := v.currentTheme
	if theme == nil {
		theme = widget.CurrentTheme()
	}
	pal := style.Of(theme)
	syncColorSample(v.colorAccent, v.colorAccentSample, pal.Accent)
	syncColorSample(v.colorSurface, v.colorSurfaceSample, pal.Surface)
	syncColorSample(v.colorField, v.colorFieldSample, pal.Field)
	syncColorSample(v.colorText, v.colorTextSample, pal.Text)
	syncColorSample(v.colorSecondary, v.colorSecondarySample, pal.Secondary)
	v.refreshColorsPreview()
}

func syncColorSample(input *widget.TextInput, sample *widget.Panel, fallback color.RGBA) {
	text := input.GetText()
	if text == "" {
		input.SetValidationError("")
		sample.Background = fallback
		sample.Invalidate()
		return
	}
	if c, ok := style.ParseColor(text); ok {
		input.SetValidationError("")
		sample.Background = c
		sample.Invalidate()
		return
	}
	input.SetValidationError(i18n.T("Dialog.Settings.Colors.Invalid"))
}

func (v *View) refreshColorsPreview() {
	v.colorsPreview.Background = v.colorSurfaceSample.Background
	v.colorsPreviewField.Background = v.colorFieldSample.Background
	v.colorsPreviewText.TextColor = v.colorTextSample.Background
	v.colorsPreviewSecond.TextColor = v.colorSecondarySample.Background
	v.colorsPreviewAccent.Background = v.colorAccentSample.Background
	v.colorsPreview.Invalidate()
	v.colorsPreviewField.Invalidate()
	v.colorsPreviewText.Invalidate()
	v.colorsPreviewSecond.Invalidate()
	v.colorsPreviewAccent.Invalidate()
}
