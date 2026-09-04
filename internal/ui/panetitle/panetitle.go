package panetitle

import (
	"image/color"

	"github.com/oops1/headless-gui/v3/widget"
)

func XOR(bg color.RGBA) color.RGBA {
	return color.RGBA{R: bg.R ^ 0xFF, G: bg.G ^ 0xFF, B: bg.B ^ 0xFF, A: 0xFF}
}

func Apply(panes []*widget.DockPane) {
	for _, pane := range panes {
		if pane == nil {
			continue
		}
		pane.TitleBG = pane.TitleActiveBG
		pane.TitleText = XOR(pane.TitleActiveBG)
	}
}
