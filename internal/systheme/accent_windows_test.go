package systheme

import (
	"errors"
	"image/color"
	"testing"
)

func TestTheWindowsPaletteGivesTheShadesWindowsItselfUses(t *testing.T) {
	blob := []byte{
		0xF0, 0xC0, 0xF4, 0x00,
		0xDB, 0x9E, 0xE5, 0x00,
		0xB7, 0x63, 0xCB, 0x00,
		0xA9, 0x4D, 0xC1, 0x00,
		0x8E, 0x3A, 0xA7, 0x00,
		0x69, 0x27, 0x82, 0x00,
		0x40, 0x0E, 0x59, 0x00,
		0x88, 0x17, 0x98, 0x00,
	}

	accent, ok := fromAccentPalette(blob, nil)

	if !ok || !accent.Known {
		t.Fatal("a full palette must be read")
	}
	if accent.Base != (color.RGBA{R: 0xC1, G: 0x4D, B: 0xA9, A: 0xFF}) {
		t.Fatalf("base = %v, want the fourth shade read as BGRA", accent.Base)
	}
	if accent.Light != (color.RGBA{R: 0xE5, G: 0x9E, B: 0xDB, A: 0xFF}) {
		t.Fatalf("light = %v, want the second shade", accent.Light)
	}
	if accent.Dark != (color.RGBA{R: 0xA7, G: 0x3A, B: 0x8E, A: 0xFF}) {
		t.Fatalf("dark = %v, want the fifth shade", accent.Dark)
	}
}

func TestAShortOrMissingPaletteIsRefused(t *testing.T) {
	if _, ok := fromAccentPalette([]byte{1, 2, 3}, nil); ok {
		t.Fatal("a truncated palette must not be read")
	}
	if _, ok := fromAccentPalette(nil, errors.New("no value")); ok {
		t.Fatal("a missing palette must not be read")
	}
}

func TestTheSingleAccentValueIsReadAsABGR(t *testing.T) {
	accent := fromAccentColor(0xFF810068, nil, true)

	if accent.Base != (color.RGBA{R: 0x68, G: 0x00, B: 0x81, A: 0xFF}) {
		t.Fatalf("base = %v, want the value read as ABGR", accent.Base)
	}
	if !accent.OnFrame {
		t.Fatal("the frame flag must be kept")
	}
	if got := fromAccentColor(0, errors.New("no value"), true); got.Known {
		t.Fatalf("accent = %+v, want nothing when the registry has no value", got)
	}
}

func TestTheFrameIsColouredOnlyWhenWindowsSaysSo(t *testing.T) {
	if !colorPrevalence(1, nil) {
		t.Fatal("ColorPrevalence 1 means the frame takes the accent")
	}
	if colorPrevalence(0, nil) || colorPrevalence(1, errors.New("x")) {
		t.Fatal("without the value the frame keeps the colour of the theme")
	}
}

func TestThePaletteWinsOverTheSingleValue(t *testing.T) {
	blob := make([]byte, accentPaletteShades*4)
	blob[paletteBaseShade*4+2] = 0x11

	fromPalette := accentFrom(blob, nil, 0xFF810068, nil, false)
	fromValue := accentFrom(nil, errors.New("no palette"), 0xFF810068, nil, true)

	if fromPalette.Base.R != 0x11 {
		t.Fatalf("base = %v, want the shade out of the palette", fromPalette.Base)
	}
	if fromValue.Base != (color.RGBA{R: 0x68, G: 0x00, B: 0x81, A: 0xFF}) || !fromValue.OnFrame {
		t.Fatalf("accent = %+v, want the single value when there is no palette", fromValue)
	}
}

func TestReadBinaryValueErrors(t *testing.T) {
	if _, err := readBinaryValue(`Software\GoGit\DoesNotExist\Key`, "x"); err == nil {
		t.Fatal("missing key must fail")
	}
	if _, err := readBinaryValue(personalizeKey, "GoGitMissingValue"); err == nil {
		t.Fatal("missing value must fail")
	}
}

func TestDetectAccentReadsTheRegistry(t *testing.T) {
	blob, blobErr := readBinaryValue(explorerAccentKey, "AccentPalette")
	value, valueErr := readIntegerValue(dwmKey, "AccentColor")
	want := accentFrom(blob, blobErr, value, valueErr, colorPrevalence(readIntegerValue(dwmKey, "ColorPrevalence")))

	if got := DetectAccent(); got != want {
		t.Fatalf("accent = %+v, registry says %+v", got, want)
	}
}
