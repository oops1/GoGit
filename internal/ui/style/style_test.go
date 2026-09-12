package style

import (
	"image/color"
	"testing"

	"github.com/oops1/headless-gui/v3/widget"
)

func TestThePaletteComesFromTheTheme(t *testing.T) {
	theme := widget.Win11LightTheme()

	p := Of(theme)

	if p.Surface != theme.DialogBG || p.Field != theme.InputBG || p.Border != theme.InputBorder {
		t.Fatalf("palette = %+v, want the colours of the theme", p)
	}
	if p.Text != theme.LabelText || p.Secondary != theme.SecondaryText || p.Accent != theme.Accent {
		t.Fatalf("palette = %+v, want the colours of the theme", p)
	}
}

func TestTextOnTheAccentStaysReadable(t *testing.T) {
	dark := Of(&widget.Theme{Accent: color.RGBA{R: 0x00, G: 0x5F, B: 0xB8, A: 0xFF}})
	light := Of(&widget.Theme{Accent: color.RGBA{R: 0x4C, G: 0xC2, B: 0xFF, A: 0xFF}})

	if dark.OnAccent != (color.RGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF}) {
		t.Fatalf("on a dark accent = %v, want white", dark.OnAccent)
	}
	if light.OnAccent != (color.RGBA{R: onBrightAccent, G: onBrightAccent, B: onBrightAccent, A: 0xFF}) {
		t.Fatalf("on a bright accent = %v, want near black", light.OnAccent)
	}
}

func TestTheAccentAnswersToTheMouse(t *testing.T) {
	for _, accent := range []color.RGBA{
		{R: 0x00, G: 0x5F, B: 0xB8, A: 0xFF},
		{R: 0x4C, G: 0xC2, B: 0xFF, A: 0xFF},
	} {
		p := Of(&widget.Theme{Accent: accent})

		if p.AccentHover == accent || p.AccentPressed == accent || p.AccentHover == p.AccentPressed {
			t.Fatalf("accent %v: hover = %v, pressed = %v, want three distinct fills", accent, p.AccentHover, p.AccentPressed)
		}
		if luminance(p.AccentPressed) == luminance(p.AccentHover) {
			t.Fatalf("accent %v: pressed must go further than hover", accent)
		}
	}
}

func TestEveryControlOfADialogGetsTheSameColours(t *testing.T) {
	p := Of(widget.Win11DarkTheme())
	input := widget.NewTextInput("")
	area := widget.NewTextBox("")
	dropdown := widget.NewDropdown()
	primary := widget.NewButton("")
	quiet := widget.NewButton("")
	body := widget.NewLabel("", color.RGBA{})
	hint := widget.NewLabel("", color.RGBA{})

	p.Fields(input)
	p.Areas(area)
	p.Lists(dropdown)
	p.Primary(primary)
	p.Quiet(quiet)
	p.Body(body)
	p.Hints(hint)

	if input.Background != p.Field || input.PaddingX != FieldPaddingX || input.PlaceColor != p.Secondary {
		t.Fatalf("input = %+v, want the field colours", input)
	}
	if area.Background != p.Field || area.PaddingX != FieldPaddingX || area.TextColor != p.Text {
		t.Fatalf("text area = %+v, want the field colours", area)
	}
	if dropdown.Background != p.Field || dropdown.PaddingX != FieldPaddingX || dropdown.ArrowColor != p.Text {
		t.Fatalf("dropdown = %+v, want the same field colours as the input", dropdown)
	}
	if primary.Background != p.Accent || primary.TextColor != p.OnAccent || primary.HoverBG != p.AccentHover {
		t.Fatal("the primary button must be filled with the accent")
	}
	if quiet.Background != p.Field || quiet.BorderColor != p.Border || quiet.TextColor != p.Text {
		t.Fatal("a quiet button must look like a field")
	}
	if body.TextColor != p.Text || hint.TextColor != p.Secondary {
		t.Fatalf("body = %v, hint = %v, want the text and the secondary colour", body.TextColor, hint.TextColor)
	}
}

func TestABannerIsTheChromeTouchedByTheAccent(t *testing.T) {
	theme := widget.Win11DarkTheme()
	p := Of(theme)
	panel := widget.NewDockPanel()
	panel.UseAlpha = true
	label := widget.NewLabel("", color.RGBA{A: 0xFF})

	p.Banner(panel, label)

	if panel.Background == p.Chrome || panel.Background == p.Accent || panel.UseAlpha || panel.Background.A != p.Chrome.A {
		t.Fatalf("background = %v, chrome %v, accent %v", panel.Background, p.Chrome, p.Accent)
	}
	if label.TextColor != p.Text {
		t.Fatalf("text = %v", label.TextColor)
	}
}

func TestATintLeansFromTheFieldTowardsTheAccent(t *testing.T) {
	p := Of(widget.Win11LightTheme())

	if p.Tint(0) != p.Field || p.Tint(100) != p.Accent {
		t.Fatalf("tint 0 = %v, tint 100 = %v, want the field and the accent", p.Tint(0), p.Tint(100))
	}
}

func TestLineCountsAreBrightOnBothLightAndDarkThemes(t *testing.T) {
	light := Of(widget.Win11LightTheme())
	dark := Of(widget.Win11DarkTheme())

	if light.Added() != addedOnLight || light.Deleted() != deletedOnLight {
		t.Fatalf("light = %v %v", light.Added(), light.Deleted())
	}
	if dark.Added() != addedOnDark || dark.Deleted() != deletedOnDark {
		t.Fatalf("dark = %v %v", dark.Added(), dark.Deleted())
	}
}
