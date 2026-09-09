package systheme

import (
	"image/color"
	"strconv"
	"strings"
)

const (
	lightShadeMix = 45
	darkShadeMix  = 25
)

type Accent struct {
	Base    color.RGBA
	Light   color.RGBA
	Dark    color.RGBA
	OnFrame bool
	Known   bool
}

type State struct {
	Scheme Scheme
	Accent Accent
}

func DetectAccent() Accent {
	return detectAccent()
}

func DetectState() State {
	return State{Scheme: Detect(), Accent: DetectAccent()}
}

func (a Accent) For(s Scheme) color.RGBA {
	if s == Light {
		return a.Dark
	}
	return a.Light
}

func withShades(base color.RGBA, onFrame bool) Accent {
	return Accent{
		Base:    base,
		Light:   mix(base, color.RGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF}, lightShadeMix),
		Dark:    mix(base, color.RGBA{A: 0xFF}, darkShadeMix),
		OnFrame: onFrame,
		Known:   true,
	}
}

func mix(from, to color.RGBA, percent int) color.RGBA {
	channel := func(a, b uint8) uint8 {
		return uint8((int(a)*(100-percent) + int(b)*percent) / 100)
	}
	return color.RGBA{
		R: channel(from.R, to.R),
		G: channel(from.G, to.G),
		B: channel(from.B, to.B),
		A: 0xFF,
	}
}

func parseChannels(value string) (color.RGBA, bool) {
	parts := strings.Split(value, ",")
	if len(parts) < 3 {
		return color.RGBA{}, false
	}
	channels := make([]uint8, 3)
	for i := range channels {
		number, err := strconv.Atoi(strings.TrimSpace(parts[i]))
		if err != nil || number < 0 || number > 255 {
			return color.RGBA{}, false
		}
		channels[i] = uint8(number)
	}
	return color.RGBA{R: channels[0], G: channels[1], B: channels[2], A: 0xFF}, true
}
