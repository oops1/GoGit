package style

import (
	"image/color"
	"testing"

	"github.com/oops1/headless-gui/v3/widget"
)

func TestParseColorAcceptsSixDigitHexWithHash(t *testing.T) {
	c, ok := ParseColor("#1A2B3C")
	if !ok {
		t.Fatal("want ok")
	}
	if c != (color.RGBA{R: 0x1A, G: 0x2B, B: 0x3C, A: 0xFF}) {
		t.Fatalf("color = %v", c)
	}
}

func TestParseColorAcceptsSixDigitHexWithoutHash(t *testing.T) {
	c, ok := ParseColor("1A2B3C")
	if !ok {
		t.Fatal("want ok")
	}
	if c != (color.RGBA{R: 0x1A, G: 0x2B, B: 0x3C, A: 0xFF}) {
		t.Fatalf("color = %v", c)
	}
}

func TestParseColorAcceptsThreeDigitShorthand(t *testing.T) {
	c, ok := ParseColor("#0AF")
	if !ok {
		t.Fatal("want ok")
	}
	if c != (color.RGBA{R: 0x00, G: 0xAA, B: 0xFF, A: 0xFF}) {
		t.Fatalf("color = %v", c)
	}
}

func TestParseColorAcceptsThreeDigitShorthandWithoutHash(t *testing.T) {
	c, ok := ParseColor("0af")
	if !ok {
		t.Fatal("want ok")
	}
	if c != (color.RGBA{R: 0x00, G: 0xAA, B: 0xFF, A: 0xFF}) {
		t.Fatalf("color = %v", c)
	}
}

func TestParseColorIsCaseInsensitive(t *testing.T) {
	upper, ok1 := ParseColor("#AABBCC")
	lower, ok2 := ParseColor("#aabbcc")
	if !ok1 || !ok2 || upper != lower {
		t.Fatalf("upper = %v (%v), lower = %v (%v)", upper, ok1, lower, ok2)
	}
}

func TestParseColorTrimsSurroundingSpaces(t *testing.T) {
	c, ok := ParseColor("  #112233  ")
	if !ok {
		t.Fatal("want ok")
	}
	if c != (color.RGBA{R: 0x11, G: 0x22, B: 0x33, A: 0xFF}) {
		t.Fatalf("color = %v", c)
	}
}

func TestParseColorRejectsJunk(t *testing.T) {
	for _, s := range []string{"", "#", "red", "#12345", "#1234567", "#GGGGGG", "  ", "#12G"} {
		if _, ok := ParseColor(s); ok {
			t.Fatalf("ParseColor(%q) = ok, want rejected", s)
		}
	}
}

func TestHexColorFormatsUppercaseWithHash(t *testing.T) {
	s := HexColor(color.RGBA{R: 0x1a, G: 0x2b, B: 0x3c, A: 0xFF})
	if s != "#1A2B3C" {
		t.Fatalf("hex = %q", s)
	}
}

func TestHexColorIsEmptyWhenAlphaIsZero(t *testing.T) {
	s := HexColor(color.RGBA{R: 0x1a, G: 0x2b, B: 0x3c, A: 0})
	if s != "" {
		t.Fatalf("hex = %q, want empty", s)
	}
}

func TestHexColorRoundTripsThroughParseColor(t *testing.T) {
	want := color.RGBA{R: 0x9E, G: 0x00, B: 0x42, A: 0xFF}
	got, ok := ParseColor(HexColor(want))
	if !ok || got != want {
		t.Fatalf("round trip = %v (%v), want %v", got, ok, want)
	}
}

func TestCustomWithEmptyColorsEqualsTheBaseTheme(t *testing.T) {
	for _, base := range []*widget.Theme{widget.Win11LightTheme(), widget.Win11DarkTheme()} {
		got := Custom(base, Colors{})
		if *got != *base {
			t.Fatalf("custom = %+v, want the base theme unchanged", *got)
		}
	}
}

func TestCustomAppliesAccentThroughTinted(t *testing.T) {
	base := widget.Win11DarkTheme()
	accent := color.RGBA{R: 0x11, G: 0x22, B: 0x33, A: 0xFF}

	got := Custom(base, Colors{Accent: accent})
	want := Tinted(base, accent)

	if *got != *want {
		t.Fatalf("custom = %+v, want the tinted theme %+v", *got, *want)
	}
}

func TestCustomAppliesSurfaceToAllSurfaceFields(t *testing.T) {
	base := widget.Win11LightTheme()
	surface := color.RGBA{R: 0x10, G: 0x20, B: 0x30, A: 0xFF}

	got := Custom(base, Colors{Surface: surface})

	fields := []color.RGBA{
		got.WindowBG, got.PanelBG, got.DialogBG, got.TabContentBG,
		got.StatusBarBG, got.SplitterBG, got.MenuBG, got.DropBG,
	}
	for _, f := range fields {
		if f != surface {
			t.Fatalf("surface field = %v, want %v", f, surface)
		}
	}
	if got.InputBG == surface || got.LabelText == surface {
		t.Fatal("surface must not leak into field or text colours")
	}
}

func TestCustomAppliesFieldToAllFieldColours(t *testing.T) {
	base := widget.Win11LightTheme()
	field := color.RGBA{R: 0x40, G: 0x50, B: 0x60, A: 0xFF}

	got := Custom(base, Colors{Field: field})

	fields := []color.RGBA{got.InputBG, got.ProgressBG, got.CheckBG, got.ScrollTrackBG}
	for _, f := range fields {
		if f != field {
			t.Fatalf("field colour = %v, want %v", f, field)
		}
	}
}

func TestCustomAppliesTextToAllTextColours(t *testing.T) {
	base := widget.Win11LightTheme()
	text := color.RGBA{R: 0x01, G: 0x02, B: 0x03, A: 0xFF}

	got := Custom(base, Colors{Text: text})

	fields := []color.RGBA{
		got.LabelText, got.InputText, got.BtnText, got.CheckText, got.TreeText,
		got.DropText, got.TabText, got.TabActiveText, got.HeaderText, got.StatusBarText,
	}
	for _, f := range fields {
		if f != text {
			t.Fatalf("text colour = %v, want %v", f, text)
		}
	}
}

func TestCustomAppliesSecondaryToItsTwoColours(t *testing.T) {
	base := widget.Win11LightTheme()
	secondary := color.RGBA{R: 0x70, G: 0x80, B: 0x90, A: 0xFF}

	got := Custom(base, Colors{Secondary: secondary})

	if got.SecondaryText != secondary || got.InputPlaceholder != secondary {
		t.Fatalf("secondary = %v/%v, want %v", got.SecondaryText, got.InputPlaceholder, secondary)
	}
}

func TestCustomWithoutAccentLeavesTheThemeAccentAlone(t *testing.T) {
	base := widget.Win11DarkTheme()

	got := Custom(base, Colors{Surface: color.RGBA{R: 1, G: 2, B: 3, A: 0xFF}})

	if got.Accent != base.Accent || got.ProgressFill != base.ProgressFill {
		t.Fatal("accent-derived fields must stay as the base theme's when no accent is given")
	}
}

func TestCustomCombinesAllFiveColoursIndependently(t *testing.T) {
	base := widget.Win11LightTheme()
	c := Colors{
		Accent:    color.RGBA{R: 1, G: 1, B: 1, A: 0xFF},
		Surface:   color.RGBA{R: 2, G: 2, B: 2, A: 0xFF},
		Field:     color.RGBA{R: 3, G: 3, B: 3, A: 0xFF},
		Text:      color.RGBA{R: 4, G: 4, B: 4, A: 0xFF},
		Secondary: color.RGBA{R: 5, G: 5, B: 5, A: 0xFF},
	}

	got := Custom(base, c)

	if got.Accent != c.Accent {
		t.Fatal("accent not applied")
	}
	if got.WindowBG != c.Surface {
		t.Fatal("surface not applied")
	}
	if got.InputBG != c.Field {
		t.Fatal("field not applied")
	}
	if got.LabelText != c.Text {
		t.Fatal("text not applied")
	}
	if got.SecondaryText != c.Secondary {
		t.Fatal("secondary not applied")
	}
}
