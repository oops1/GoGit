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
	if t2.ListItemSelect.R != accent.R || t2.DropItemBG.R != accent.R {
		t.Fatal("selection fills must take the accent")
	}
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
