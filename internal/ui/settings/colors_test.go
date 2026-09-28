package settings

import (
	"image/color"
	"testing"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/config"
	"github.com/oops1/gogit/internal/ui/style"
)

func TestValidHexInputPaintsTheSampleAndThePreview(t *testing.T) {
	v := newTestView(t, []string{"en"}, Model{})

	v.colorField.SetText("#334455")
	v.colorField.OnChange("#334455")

	want := color.RGBA{R: 0x33, G: 0x44, B: 0x55, A: 0xFF}
	if v.colorFieldSample.Background != want {
		t.Fatalf("sample = %v, want %v", v.colorFieldSample.Background, want)
	}
	if v.colorsPreviewField.Background != want {
		t.Fatalf("preview field = %v, want %v", v.colorsPreviewField.Background, want)
	}
	if v.colorField.ValidationError() != "" {
		t.Fatalf("validation error = %q, want none", v.colorField.ValidationError())
	}
}

func TestInvalidInputLeavesTheSampleUnchangedAndMarksTheField(t *testing.T) {
	v := newTestView(t, []string{"en"}, Model{})
	v.colorAccent.SetText("#334455")
	v.colorAccent.OnChange("#334455")
	before := v.colorAccentSample.Background

	v.colorAccent.SetText("not-a-colour")
	v.colorAccent.OnChange("not-a-colour")

	if v.colorAccentSample.Background != before {
		t.Fatalf("sample = %v, want it unchanged at %v", v.colorAccentSample.Background, before)
	}
	if v.colorAccent.ValidationError() == "" {
		t.Fatal("want a validation error for junk input")
	}
}

func TestEmptyInputIsNotAnErrorAndShowsTheThemeColour(t *testing.T) {
	v := newTestView(t, []string{"en"}, Model{})
	v.Restyle(widget.Win11DarkTheme())
	v.colorSecondary.SetText("#334455")
	v.colorSecondary.OnChange("#334455")

	v.colorSecondary.SetText("")
	v.colorSecondary.OnChange("")

	if v.colorSecondary.ValidationError() != "" {
		t.Fatalf("validation error = %q, want none for an empty field", v.colorSecondary.ValidationError())
	}
	want := style.Of(widget.Win11DarkTheme()).Secondary
	if v.colorSecondarySample.Background != want {
		t.Fatalf("sample = %v, want the theme's secondary colour %v", v.colorSecondarySample.Background, want)
	}
}

func TestColorsResetClearsAllFiveFieldsAndSamples(t *testing.T) {
	v := newTestView(t, []string{"en"}, Model{})
	for _, input := range v.colorInputs() {
		input.SetText("#112233")
		input.OnChange("#112233")
	}

	v.colorsReset.OnClick()

	for _, input := range v.colorInputs() {
		if input.GetText() != "" {
			t.Fatalf("input left at %q, want empty after reset", input.GetText())
		}
	}
	pal := style.Of(widget.CurrentTheme())
	if v.colorAccentSample.Background != pal.Accent {
		t.Fatalf("accent sample = %v, want the theme accent %v", v.colorAccentSample.Background, pal.Accent)
	}
}

func TestColorsResetMarksTheDialogModified(t *testing.T) {
	v := newTestView(t, []string{"en"}, Model{})
	v.colorAccent.SetText("#112233")
	v.colorAccent.OnChange("#112233")
	v.initial = v.request()
	if v.Modified() {
		t.Fatal("must start unmodified once the initial snapshot is taken")
	}

	v.colorsReset.OnClick()

	if !v.Modified() {
		t.Fatal("clearing a configured colour must mark the dialog modified")
	}
}

func TestRestyleGivesColorInputsTheDeletedTextErrorBorder(t *testing.T) {
	v := newTestView(t, []string{"en"}, Model{})
	theme := widget.Win11DarkTheme()

	v.Restyle(theme)

	want := style.Of(theme).DeletedText
	for _, input := range v.colorInputs() {
		if input.ErrorBorder != want {
			t.Fatalf("error border = %v, want %v", input.ErrorBorder, want)
		}
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
