package panetitle

import (
	"image/color"

	"github.com/oops1/headless-gui/v3/widget"
)

const activeTintPercent = 18

func Tint(base, accent color.RGBA) color.RGBA {
	return color.RGBA{
		R: blend(base.R, accent.R),
		G: blend(base.G, accent.G),
		B: blend(base.B, accent.B),
		A: 0xFF,
	}
}

func blend(base, accent uint8) uint8 {
	return uint8((int(base)*(100-activeTintPercent) + int(accent)*activeTintPercent) / 100)
}

func Apply(panes []*widget.DockPane, t *widget.Theme) {
	for _, pane := range panes {
		if pane == nil {
			continue
		}
		pane.TitleBG = t.PanelBG
		pane.TitleActiveBG = Tint(t.PanelBG, t.Accent)
		pane.TitleText = t.SecondaryText
		pane.TitleTextActive = t.LabelText
	}
}
