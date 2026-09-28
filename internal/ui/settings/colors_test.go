package settings

import (
	"image/color"
	"testing"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/config"
	"github.com/oops1/gogit/internal/ui/style"
)

func TestPickingAColourPaintsThePreviewAndIsKept(t *testing.T) {
	v := newTestView(t, []string{"en"}, Model{})
	picked := color.RGBA{R: 0x33, G: 0x77, B: 0xCC, A: 0xFF}

	v.colorField.SetValue(picked)
	v.colorField.OnChanged(picked)

	if v.colorsPreviewField.Color != picked {
		t.Fatalf("preview field = %+v, want %+v", v.colorsPreviewField.Color, picked)
	}
	if got := v.request().Colors.Field; got != "#3377CC" {
		t.Fatalf("field colour = %q, want the picked one", got)
	}
}

func TestAColourNobodyTouchedStaysEmptyAndShowsTheTheme(t *testing.T) {
	v := newTestView(t, []string{"en"}, Model{})
	palette := style.Of(widget.CurrentTheme())

	if got := v.colorAccent.Value(); got != palette.Accent {
		t.Fatalf("accent picker = %+v, want the theme accent %+v", got, palette.Accent)
	}
	if got := v.request().Colors; got != (config.Colors{}) {
		t.Fatalf("colors = %+v, want all of them empty", got)
	}
}

func TestResetGivesEveryColourBackToTheThemeAndMarksTheDialog(t *testing.T) {
	v := newTestView(t, []string{"en"}, Model{Colors: config.Colors{Accent: "#112233", Surface: "#445566"}})
	if got := v.request().Colors.Accent; got != "#112233" {
		t.Fatalf("accent = %q before the reset", got)
	}

	v.colorsReset.OnClick()

	if got := v.request().Colors; got != (config.Colors{}) {
		t.Fatalf("colors = %+v after the reset, want all of them empty", got)
	}
	palette := style.Of(widget.CurrentTheme())
	if v.colorAccent.Value() != palette.Accent || v.colorSurface.Value() != palette.Surface {
		t.Fatal("the pickers kept the colours instead of taking them from the theme")
	}
	if !v.Modified() {
		t.Fatal("the reset must mark the dialog modified")
	}
}

func TestApplyAndRequestRoundTripColors(t *testing.T) {
	m := Model{Colors: config.Colors{
		Accent:    "#111111",
		Surface:   "#222222",
		Field:     "#333333",
		Text:      "#444444",
		Secondary: "#555555",
	}}
	v := newTestView(t, []string{"en"}, m)

	got := v.request()

	if got.Colors != m.Colors {
		t.Fatalf("colors = %+v, want %+v", got.Colors, m.Colors)
	}
}

func TestRestyleKeepsTheChosenColoursAndRefreshesTheRest(t *testing.T) {
	v := newTestView(t, []string{"en"}, Model{Colors: config.Colors{Accent: "#112233"}})
	chosen := color.RGBA{R: 0x11, G: 0x22, B: 0x33, A: 0xFF}

	for _, theme := range []*widget.Theme{widget.Win11LightTheme(), widget.Win11DarkTheme()} {
		v.Restyle(theme)
		palette := style.Of(theme)
		if v.colorAccent.Value() != chosen {
			t.Fatalf("accent = %+v, want the chosen colour kept", v.colorAccent.Value())
		}
		if v.colorSurface.Value() != palette.Surface {
			t.Fatalf("surface = %+v, want the colour of the new theme %+v", v.colorSurface.Value(), palette.Surface)
		}
		if v.colorsPreview.Color != palette.Surface {
			t.Fatalf("preview = %+v, want it repainted with the theme", v.colorsPreview.Color)
		}
	}
}

func TestSearchingAColorLabelKeepsItsRowVisible(t *testing.T) {
	v := newTestView(t, []string{"en"}, Model{})

	v.applySearch("accent")

	if v.sectionGeneral.RowDefs[colorAccentRow].Value == 0 {
		t.Fatal("colorAccent row must stay visible: its label matches")
	}
	if v.sectionGeneral.RowDefs[colorSurfaceRow].Value != 0 {
		t.Fatal("colorSurface row must collapse: nothing about it matches")
	}
	if v.sectionGeneral.RowDefs[3].Value != 0 {
		t.Fatal("language row must collapse: nothing about it matches")
	}
}

func TestSearchingASecondaryColorHintKeepsItsRowVisible(t *testing.T) {
	v := newTestView(t, []string{"en"}, Model{})

	v.applySearch("placeholder")

	if v.sectionGeneral.RowDefs[colorSecondaryRow].Value == 0 {
		t.Fatal("colorSecondary row must stay visible: its hint matches")
	}
}
