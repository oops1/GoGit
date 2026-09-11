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

func TestSurfacesTakeATraceOfTheAccent(t *testing.T) {
	base := widget.Win11LightTheme()
	accent := color.RGBA{R: 0xA2, G: 0x7F, B: 0x2B, A: 0xFF}

	t2 := Tinted(base, accent)

	if t2.WindowBG == base.WindowBG || t2.PanelBG == base.PanelBG {
		t.Fatal("the window must take the tint of the system colour")
	}
	if distance(t2.WindowBG, base.WindowBG) > distance(t2.WindowBG, accent) {
		t.Fatalf("window = %v, want it near the colour of the preset, not near the accent", t2.WindowBG)
	}
	if distance(t2.InputBG, base.InputBG) >= distance(t2.WindowBG, base.WindowBG) {
		t.Fatal("fields must be tinted less than the surfaces behind them")
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

func distance(a, b color.RGBA) int {
	return abs(int(a.R)-int(b.R)) + abs(int(a.G)-int(b.G)) + abs(int(a.B)-int(b.B))
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
