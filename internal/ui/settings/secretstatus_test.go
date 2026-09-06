package settings

import (
	"image/color"
	"math"
	"testing"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/i18n"
)

func TestSecretStatusLabelUsesLocalizedText(t *testing.T) {
	widget.ClearStrings()
	defer widget.ClearStrings()
	if _, err := i18n.Install(""); err != nil {
		t.Fatal(err)
	}
	i18n.Apply("en")

	cases := []struct {
		status SecretStatus
		want   string
	}{
		{StatusSaved, i18n.T("Dialog.Settings.Secrets.Status.Saved")},
		{StatusAuthRequired, i18n.T("Dialog.Settings.Secrets.Status.AuthRequired")},
		{StatusError, i18n.T("Dialog.Settings.Secrets.Status.Error")},
		{SecretStatus(99), i18n.T("Dialog.Settings.Secrets.Status.Saved")},
	}
	for _, c := range cases {
		if got := c.status.Label(); got != c.want {
			t.Fatalf("Label(%d) = %q, want %q", c.status, got, c.want)
		}
	}
}

func TestSecretStatusDotColorDerivesDistinctHuesFromTheAccent(t *testing.T) {
	accent := color.RGBA{R: 0, G: 120, B: 215, A: 255}
	theme := &widget.Theme{Accent: accent}

	saved := StatusSaved.DotColor(theme)
	authRequired := StatusAuthRequired.DotColor(theme)
	errStatus := StatusError.DotColor(theme)

	if saved == authRequired || saved == errStatus || authRequired == errStatus {
		t.Fatalf("expected three distinct colors, got saved=%+v authRequired=%+v error=%+v",
			saved, authRequired, errStatus)
	}
	for name, got := range map[string]color.RGBA{"saved": saved, "authRequired": authRequired, "error": errStatus} {
		if got.A != accent.A {
			t.Fatalf("%s: alpha = %d, want %d (inherited from accent)", name, got.A, accent.A)
		}
	}
}

func TestSecretStatusDotColorBoostsSaturationForAGrayAccent(t *testing.T) {
	gray := color.RGBA{R: 128, G: 128, B: 128, A: 255}
	theme := &widget.Theme{Accent: gray}

	got := StatusError.DotColor(theme)
	if got.R == got.G && got.G == got.B {
		t.Fatalf("expected a saturated red-ish color, got %+v (still gray)", got)
	}
}

func TestRGBToHSLAndBackRoundTripsWithinRoundingTolerance(t *testing.T) {
	samples := []color.RGBA{
		{R: 0, G: 120, B: 215, A: 255},
		{R: 255, G: 0, B: 0, A: 255},
		{R: 0, G: 255, B: 0, A: 255},
		{R: 0, G: 0, B: 255, A: 255},
		{R: 128, G: 128, B: 128, A: 255},
		{R: 255, G: 255, B: 255, A: 255},
		{R: 0, G: 0, B: 0, A: 255},
		{R: 200, G: 140, B: 140, A: 255},
		{R: 255, G: 0, B: 100, A: 255},
	}
	for _, c := range samples {
		h, s, l := rgbToHSL(c)
		back := hslToRGB(h, s, l, c.A)
		const tolerance = 1
		if diff(back.R, c.R) > tolerance || diff(back.G, c.G) > tolerance || diff(back.B, c.B) > tolerance {
			t.Fatalf("round trip of %+v = %+v (h=%.2f s=%.2f l=%.2f)", c, back, h, s, l)
		}
	}
}

func diff(a, b uint8) int {
	if a > b {
		return int(a - b)
	}
	return int(b - a)
}

func TestHSLToRGBCoversEveryHueSextant(t *testing.T) {
	hues := []float64{0, 30, 90, 150, 210, 270, 330, 359}
	for _, h := range hues {
		got := hslToRGB(h, 1, 0.5, 255)
		if got.R == 0 && got.G == 0 && got.B == 0 {
			t.Fatalf("hue %.0f produced black", h)
		}
	}
}

func TestRGBToHSLReturnsZeroSaturationForAchromaticColors(t *testing.T) {
	_, s, l := rgbToHSL(color.RGBA{R: 100, G: 100, B: 100, A: 255})
	if s != 0 {
		t.Fatalf("saturation = %v, want 0 for a gray color", s)
	}
	if math.Abs(l-100.0/255) > 0.01 {
		t.Fatalf("lightness = %v, want ~%v", l, 100.0/255)
	}
}
