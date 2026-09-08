package panetitle

import (
	"image/color"
	"testing"

	"github.com/oops1/headless-gui/v3/widget"
)

func TestTintKeepsTheBackgroundAndOnlyHintsAtTheAccent(t *testing.T) {
	base := color.RGBA{R: 0xF3, G: 0xF3, B: 0xF3, A: 0xFF}
	accent := color.RGBA{R: 0x00, G: 0x78, B: 0xD7, A: 0xFF}

	got := Tint(base, accent)

	if got.A != 0xFF {
		t.Fatalf("alpha = %d, want an opaque colour", got.A)
	}
	if distance(got, base) >= distance(got, accent) {
		t.Fatalf("tint %v leans towards the accent, want it to stay close to the background", got)
	}
	if got == base {
		t.Fatal("the active title must differ from the inactive one")
	}
}

func TestTintOfEqualColoursChangesNothing(t *testing.T) {
	grey := color.RGBA{R: 0x2D, G: 0x2D, B: 0x30, A: 0xFF}

	if got := Tint(grey, grey); got != grey {
		t.Fatalf("Tint(%v, %v) = %v, want it unchanged", grey, grey, got)
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

func TestApplyPaintsTitlesFromTheTheme(t *testing.T) {
	theme := widget.Win11LightTheme()
	first := widget.NewDockPane("a", "A", nil)
	second := widget.NewDockPane("b", "B", nil)

	Apply([]*widget.DockPane{first, nil, second}, theme)

	for _, pane := range []*widget.DockPane{first, second} {
		if pane.TitleBG != theme.PanelBG {
			t.Fatalf("pane %q title background = %v, want the panel colour", pane.ID, pane.TitleBG)
		}
		if pane.TitleActiveBG != Tint(theme.PanelBG, theme.Accent) {
			t.Fatalf("pane %q active title = %v, want the tinted panel colour", pane.ID, pane.TitleActiveBG)
		}
		if pane.TitleText != theme.SecondaryText || pane.TitleTextActive != theme.LabelText {
			t.Fatalf("pane %q title text = %v/%v, want the theme text colours", pane.ID, pane.TitleText, pane.TitleTextActive)
		}
	}
}

func TestApplyAcceptsNoPanes(t *testing.T) {
	Apply(nil, widget.Win11DarkTheme())
}
