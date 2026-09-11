package systheme

import (
	"image/color"
	"os"
	"path/filepath"
	"testing"
)

func TestTheShadeFollowsTheColourScheme(t *testing.T) {
	accent := Accent{
		Light: color.RGBA{R: 0xE5, G: 0x9E, B: 0xDB, A: 0xFF},
		Dark:  color.RGBA{R: 0x82, G: 0x27, B: 0x69, A: 0xFF},
	}

	if got := accent.For(Dark); got != accent.Light {
		t.Fatalf("on a dark desktop = %v, want the light shade", got)
	}
	if got := accent.For(Light); got != accent.Dark {
		t.Fatalf("on a light desktop = %v, want the dark shade", got)
	}
	if got := accent.For(Unknown); got != accent.Light {
		t.Fatalf("without a known scheme = %v, want the light shade", got)
	}
}

func TestShadesAreDerivedWhenTheSystemGivesOneColour(t *testing.T) {
	base := color.RGBA{R: 0x68, G: 0x00, B: 0x81, A: 0xFF}

	accent := withShades(base, true)

	if !accent.Known || !accent.OnFrame || accent.Base != base {
		t.Fatalf("accent = %+v, want the colour as reported", accent)
	}
	if accent.Light == base || accent.Dark == base || accent.Light == accent.Dark {
		t.Fatalf("accent = %+v, want a lighter and a darker shade", accent)
	}
	if accent.Light.R <= base.R || accent.Dark.R >= base.R {
		t.Fatalf("accent = %+v, want the light shade above and the dark one below the base", accent)
	}
}

func TestChannelsAreReadTheWayDesktopFilesWriteThem(t *testing.T) {
	got, ok := parseChannels(" 40, 120,255 ")
	if !ok || got != (color.RGBA{R: 40, G: 120, B: 255, A: 0xFF}) {
		t.Fatalf("colour = %v, ok = %v", got, ok)
	}
	for _, bad := range []string{"", "1,2", "1,2,x", "1,2,300", "-1,2,3"} {
		if _, ok := parseChannels(bad); ok {
			t.Fatalf("%q must not parse as a colour", bad)
		}
	}
}

func TestTheAccentOfAKDEDesktopIsRead(t *testing.T) {
	dir := t.TempDir()
	getenv := func(name string) string {
		if name == "XDG_CONFIG_HOME" {
			return dir
		}
		return ""
	}

	if got := accentFromDesktopFiles(getenv, dir); got.Known {
		t.Fatalf("accent = %+v, want nothing without a kdeglobals", got)
	}

	write := func(body string) {
		if err := os.WriteFile(filepath.Join(dir, "kdeglobals"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	write("[Colors:Selection]\nBackgroundNormal=61,174,233\n")
	if got := accentFromDesktopFiles(getenv, dir); !got.Known || got.Base != (color.RGBA{R: 61, G: 174, B: 233, A: 0xFF}) {
		t.Fatalf("accent = %+v, want the selection colour", got)
	}

	write("[General]\nAccentColor=146,54,180\n[Colors:Selection]\nBackgroundNormal=61,174,233\n")
	if got := accentFromDesktopFiles(getenv, dir); got.Base != (color.RGBA{R: 146, G: 54, B: 180, A: 0xFF}) {
		t.Fatalf("accent = %+v, want the accent to win over the selection colour", got)
	}

	write("[General]\nAccentColor=not a colour\n")
	if got := accentFromDesktopFiles(getenv, dir); got.Known {
		t.Fatalf("accent = %+v, want nothing from a broken line", got)
	}
}

func TestDetectStateCarriesBothHalvesOfTheSystemLook(t *testing.T) {
	state := DetectState()

	if state.Scheme != Detect() || state.Accent != DetectAccent() {
		t.Fatalf("state = %+v, want the scheme and the accent the system reports", state)
	}
}
