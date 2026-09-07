package settings

import (
	"image/color"
	"math"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/i18n"
)

type SecretStatus int

const (
	StatusSaved SecretStatus = iota
	StatusAuthRequired
	StatusError
)

const (
	secretStatusHueSaved        = 142.0
	secretStatusHueAuthRequired = 40.0
	secretStatusHueError        = 4.0
	secretStatusMinSaturation   = 0.55
)

func (s SecretStatus) labelKey() string {
	switch s {
	case StatusAuthRequired:
		return "Dialog.Settings.Secrets.Status.AuthRequired"
	case StatusError:
		return "Dialog.Settings.Secrets.Status.Error"
	default:
		return "Dialog.Settings.Secrets.Status.Saved"
	}
}

func (s SecretStatus) Label() string {
	return i18n.T(s.labelKey())
}

func (s SecretStatus) hue() float64 {
	switch s {
	case StatusAuthRequired:
		return secretStatusHueAuthRequired
	case StatusError:
		return secretStatusHueError
	default:
		return secretStatusHueSaved
	}
}

func (s SecretStatus) DotColor(theme *widget.Theme) color.RGBA {
	return deriveAccentHue(theme.Accent, s.hue())
}

func deriveAccentHue(accent color.RGBA, hue float64) color.RGBA {
	_, sat, lum := rgbToHSL(accent)
	if sat < secretStatusMinSaturation {
		sat = secretStatusMinSaturation
	}
	return hslToRGB(hue, sat, lum, accent.A)
}

func rgbToHSL(c color.RGBA) (h, s, l float64) {
	r := float64(c.R) / 255
	g := float64(c.G) / 255
	b := float64(c.B) / 255
	maxV := math.Max(r, math.Max(g, b))
	minV := math.Min(r, math.Min(g, b))
	l = (maxV + minV) / 2
	d := maxV - minV
	if d == 0 {
		return 0, 0, l
	}
	if l > 0.5 {
		s = d / (2 - maxV - minV)
	} else {
		s = d / (maxV + minV)
	}
	switch maxV {
	case r:
		h = math.Mod((g-b)/d, 6)
	case g:
		h = (b-r)/d + 2
	default:
		h = (r-g)/d + 4
	}
	h *= 60
	if h < 0 {
		h += 360
	}
	return h, s, l
}

func hslToRGB(h, s, l float64, a uint8) color.RGBA {
	c := (1 - math.Abs(2*l-1)) * s
	x := c * (1 - math.Abs(math.Mod(h/60, 2)-1))
	m := l - c/2
	var r, g, b float64
	switch {
	case h < 60:
		r, g, b = c, x, 0
	case h < 120:
		r, g, b = x, c, 0
	case h < 180:
		r, g, b = 0, c, x
	case h < 240:
		r, g, b = 0, x, c
	case h < 300:
		r, g, b = x, 0, c
	default:
		r, g, b = c, 0, x
	}
	return color.RGBA{
		R: uint8(math.Round((r + m) * 255)),
		G: uint8(math.Round((g + m) * 255)),
		B: uint8(math.Round((b + m) * 255)),
		A: a,
	}
}
