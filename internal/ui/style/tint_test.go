package style

import (
	"image/color"
	"testing"

	"github.com/oops1/headless-gui/v3/widget"
)

func TestTheAccentOfTheSystemReplacesTheOneOfThePreset(t *testing.T) {
	base := widget.Win11DarkTheme()
	accent := color.RGBA{R: 0xE5, G: 0x9E, B: 0xDB, A: 0xFF}

	t2 := Tinted(base, accent)

	for name, got := range map[string]color.RGBA{
		"Accent":          t2.Accent,
		"ProgressFill":    t2.ProgressFill,
		"SliderFill":      t2.SliderFill,
		"ToggleOnBG":      t2.ToggleOnBG,
		"InputCaret":      t2.InputCaret,
		"InputFocus":      t2.InputFocus,
		"SplitterHoverBG": t2.SplitterHoverBG,
	} {
		if got != accent {
			t.Fatalf("%s = %v, want the accent of the system", name, got)
		}
	}
	if t2.ListItemSelect.A != base.ListItemSelect.A || t2.DropItemBG.A != base.DropItemBG.A {
		t.Fatal("selection fills must keep the transparency of the preset")
	}
	if t2.ListItemSelect == base.ListItemSelect || t2.DropItemBG == base.DropItemBG {
		t.Fatal("selection fills must follow the accent of the system")
	}
}

func TestTextStaysReadableOnASelectionOfAnyAccent(t *testing.T) {
	accents := []color.RGBA{
		{R: 0xDC, G: 0xCC, B: 0x7D, A: 0xFF},
		{R: 0x87, G: 0x66, B: 0x22, A: 0xFF},
		{R: 0x4C, G: 0xC2, B: 0xFF, A: 0xFF},
		{R: 0x00, G: 0x5F, B: 0xB8, A: 0xFF},
		{R: 0xE5, G: 0x9E, B: 0xDB, A: 0xFF},
		{R: 0x68, G: 0x00, B: 0x81, A: 0xFF},
	}
	for _, base := range []*widget.Theme{widget.Win11DarkTheme(), widget.Win11LightTheme()} {
		for _, accent := range accents {
			t2 := Tinted(base, accent)
			shown := over(t2.ListItemSelect, t2.PanelBG)

			if gap := abs(luminance(shown) - luminance(t2.LabelText)); gap < readableGap {
				t.Fatalf("accent %v on %s: selection %v leaves text %v with a gap of %d, want at least %d",
					accent, base.Style.Name, shown, t2.LabelText, gap, readableGap)
			}
		}
	}
}

const readableGap = 100

func over(top, bottom color.RGBA) color.RGBA {
	blend := func(a, b uint8) uint8 {
		return uint8((int(a)*int(top.A) + int(b)*(255-int(top.A))) / 255)
	}
	return color.RGBA{R: blend(top.R, bottom.R), G: blend(top.G, bottom.G), B: blend(top.B, bottom.B), A: 0xFF}
}

func TestTheAccentLeavesTheSurfacesOfTheWindowAlone(t *testing.T) {
	base := widget.Win11LightTheme()

	t2 := Tinted(base, color.RGBA{R: 0xA2, G: 0x7F, B: 0x2B, A: 0xFF})

	for name, pair := range map[string][2]color.RGBA{
		"WindowBG": {t2.WindowBG, base.WindowBG},
		"PanelBG":  {t2.PanelBG, base.PanelBG},
		"TitleBG":  {t2.TitleBG, base.TitleBG},
		"DialogBG": {t2.DialogBG, base.DialogBG},
		"InputBG":  {t2.InputBG, base.InputBG},
		"BtnBG":    {t2.BtnBG, base.BtnBG},
		"Border":   {t2.Border, base.Border},
	} {
		if pair[0] != pair[1] {
			t.Fatalf("%s = %v, want %v: Windows paints accents with the accent, not the window", name, pair[0], pair[1])
		}
	}
}

func TestTintingLeavesThePresetAlone(t *testing.T) {
	base := widget.Win11LightTheme()
	was := *base

	Tinted(base, color.RGBA{R: 0xFF, A: 0xFF})

	if *base != was {
		t.Fatal("the theme it was given must not change")
	}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
