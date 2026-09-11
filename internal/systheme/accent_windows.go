package systheme

import (
	"image/color"

	"golang.org/x/sys/windows/registry"
)

const (
	dwmKey            = `Software\Microsoft\Windows\DWM`
	explorerAccentKey = `Software\Microsoft\Windows\CurrentVersion\Explorer\Accent`

	accentPaletteShades = 8
	paletteLightShade   = 1
	paletteBaseShade    = 3
	paletteDarkShade    = 4
)

func detectAccent() Accent {
	blob, blobErr := readBinaryValue(explorerAccentKey, "AccentPalette")
	value, valueErr := readIntegerValue(dwmKey, "AccentColor")
	return accentFrom(blob, blobErr, value, valueErr, colorPrevalence(readIntegerValue(dwmKey, "ColorPrevalence")))
}

func accentFrom(blob []byte, blobErr error, value uint64, valueErr error, onFrame bool) Accent {
	if accent, ok := fromAccentPalette(blob, blobErr); ok {
		accent.OnFrame = onFrame
		return accent
	}
	return fromAccentColor(value, valueErr, onFrame)
}

func readBinaryValue(path, name string) ([]byte, error) {
	key, err := registry.OpenKey(registry.CURRENT_USER, path, registry.QUERY_VALUE)
	if err != nil {
		return nil, err
	}
	defer key.Close()
	value, _, err := key.GetBinaryValue(name)
	return value, err
}

func colorPrevalence(value uint64, err error) bool {
	return err == nil && value == 1
}

func fromAccentPalette(blob []byte, err error) (Accent, bool) {
	if err != nil || len(blob) < accentPaletteShades*4 {
		return Accent{}, false
	}
	shade := func(index int) color.RGBA {
		at := index * 4
		return color.RGBA{R: blob[at+2], G: blob[at+1], B: blob[at], A: 0xFF}
	}
	return Accent{
		Base:  shade(paletteBaseShade),
		Light: shade(paletteLightShade),
		Dark:  shade(paletteDarkShade),
		Known: true,
	}, true
}

func fromAccentColor(value uint64, err error, onFrame bool) Accent {
	if err != nil {
		return Accent{}
	}
	return withShades(color.RGBA{R: uint8(value), G: uint8(value >> 8), B: uint8(value >> 16), A: 0xFF}, onFrame)
}
