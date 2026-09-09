package reposettings

import (
	"image/color"

	"github.com/oops1/headless-gui/v3/widget"
)

type palette struct {
	surface        color.RGBA
	chrome         color.RGBA
	input          color.RGBA
	border         color.RGBA
	text           color.RGBA
	secondary      color.RGBA
	primary        color.RGBA
	primaryHover   color.RGBA
	primaryPressed color.RGBA
	onPrimary      color.RGBA
}

const darkWindowSum = 384

var (
	primaryFill    = color.RGBA{R: 0x06, G: 0x68, B: 0xF9, A: 0xFF}
	primaryHovered = color.RGBA{R: 0x05, G: 0x5B, B: 0xDF, A: 0xFF}
	primaryPushed  = color.RGBA{R: 0x04, G: 0x4D, B: 0xC2, A: 0xFF}
	primaryText    = color.RGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF}

	lightPalette = palette{
		surface:        color.RGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF},
		chrome:         color.RGBA{R: 0xF5, G: 0xF6, B: 0xF8, A: 0xFF},
		input:          color.RGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF},
		border:         color.RGBA{R: 0xD0, G: 0xD5, B: 0xDF, A: 0xFF},
		text:           color.RGBA{R: 0x17, G: 0x20, B: 0x33, A: 0xFF},
		secondary:      color.RGBA{R: 0x68, G: 0x74, B: 0x8A, A: 0xFF},
		primary:        primaryFill,
		primaryHover:   primaryHovered,
		primaryPressed: primaryPushed,
		onPrimary:      primaryText,
	}

	darkPalette = palette{
		surface:        color.RGBA{R: 0x23, G: 0x26, B: 0x29, A: 0xFF},
		chrome:         color.RGBA{R: 0x2A, G: 0x2E, B: 0x34, A: 0xFF},
		input:          color.RGBA{R: 0x34, G: 0x38, B: 0x3E, A: 0xFF},
		border:         color.RGBA{R: 0x59, G: 0x61, B: 0x6B, A: 0xFF},
		text:           color.RGBA{R: 0xED, G: 0xF0, B: 0xF5, A: 0xFF},
		secondary:      color.RGBA{R: 0xB0, G: 0xB8, B: 0xC4, A: 0xFF},
		primary:        primaryFill,
		primaryHover:   primaryHovered,
		primaryPressed: primaryPushed,
		onPrimary:      primaryText,
	}
)

func paletteFor(t *widget.Theme) palette {
	if int(t.WindowBG.R)+int(t.WindowBG.G)+int(t.WindowBG.B) < darkWindowSum {
		return darkPalette
	}
	return lightPalette
}
