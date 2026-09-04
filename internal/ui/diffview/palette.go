package diffview

import (
	"image/color"

	"github.com/oops1/headless-gui/v3/widget"
)

type Palette struct {
	Background      color.RGBA
	Text            color.RGBA
	Border          color.RGBA
	GutterBG        color.RGBA
	GutterText      color.RGBA
	AddedBG         color.RGBA
	AddedGutterBG   color.RGBA
	RemovedBG       color.RGBA
	RemovedGutterBG color.RGBA
	PlaceholderBG   color.RGBA
	HunkHeaderBG    color.RGBA
	HunkHeaderText  color.RGBA
	AddedSpan       color.RGBA
	RemovedSpan     color.RGBA
	NoNewlineText   color.RGBA
	Selection       color.RGBA
	ScrollTrack     color.RGBA
	ScrollThumb     color.RGBA
}

func DarkPalette() Palette {
	return Palette{
		Background:      color.RGBA{R: 30, G: 30, B: 30, A: 255},
		Text:            color.RGBA{R: 212, G: 212, B: 212, A: 255},
		Border:          color.RGBA{R: 62, G: 62, B: 66, A: 255},
		GutterBG:        color.RGBA{R: 37, G: 37, B: 38, A: 255},
		GutterText:      color.RGBA{R: 133, G: 133, B: 133, A: 255},
		AddedBG:         color.RGBA{R: 31, G: 56, B: 38, A: 255},
		AddedGutterBG:   color.RGBA{R: 38, G: 68, B: 46, A: 255},
		RemovedBG:       color.RGBA{R: 66, G: 32, B: 32, A: 255},
		RemovedGutterBG: color.RGBA{R: 82, G: 38, B: 38, A: 255},
		PlaceholderBG:   color.RGBA{R: 38, G: 38, B: 40, A: 255},
		HunkHeaderBG:    color.RGBA{R: 45, G: 45, B: 48, A: 255},
		HunkHeaderText:  color.RGBA{R: 118, G: 168, B: 220, A: 255},
		AddedSpan:       color.RGBA{R: 40, G: 86, B: 55, A: 120},
		RemovedSpan:     color.RGBA{R: 98, G: 45, B: 45, A: 120},
		NoNewlineText:   color.RGBA{R: 150, G: 150, B: 150, A: 255},
		Selection:       color.RGBA{R: 0, G: 120, B: 215, A: 255},
		ScrollTrack:     color.RGBA{R: 37, G: 37, B: 38, A: 255},
		ScrollThumb:     color.RGBA{R: 94, G: 94, B: 98, A: 255},
	}
}

func LightPalette() Palette {
	return Palette{
		Background:      color.RGBA{R: 255, G: 255, B: 255, A: 255},
		Text:            color.RGBA{R: 30, G: 30, B: 30, A: 255},
		Border:          color.RGBA{R: 214, G: 214, B: 214, A: 255},
		GutterBG:        color.RGBA{R: 245, G: 245, B: 245, A: 255},
		GutterText:      color.RGBA{R: 110, G: 110, B: 110, A: 255},
		AddedBG:         color.RGBA{R: 226, G: 245, B: 229, A: 255},
		AddedGutterBG:   color.RGBA{R: 204, G: 238, B: 211, A: 255},
		RemovedBG:       color.RGBA{R: 253, G: 231, B: 231, A: 255},
		RemovedGutterBG: color.RGBA{R: 248, G: 209, B: 209, A: 255},
		PlaceholderBG:   color.RGBA{R: 247, G: 247, B: 247, A: 255},
		HunkHeaderBG:    color.RGBA{R: 238, G: 242, B: 247, A: 255},
		HunkHeaderText:  color.RGBA{R: 38, G: 90, B: 150, A: 255},
		AddedSpan:       color.RGBA{R: 52, G: 95, B: 65, A: 110},
		RemovedSpan:     color.RGBA{R: 110, G: 65, B: 65, A: 110},
		NoNewlineText:   color.RGBA{R: 130, G: 130, B: 130, A: 255},
		Selection:       color.RGBA{R: 0, G: 120, B: 215, A: 255},
		ScrollTrack:     color.RGBA{R: 240, G: 240, B: 240, A: 255},
		ScrollThumb:     color.RGBA{R: 190, G: 190, B: 190, A: 255},
	}
}

func PaletteFor(t *widget.Theme) Palette {
	if t == nil {
		return DarkPalette()
	}
	p := LightPalette()
	if isDarkColor(t.WindowBG) {
		p = DarkPalette()
	}
	p.Background = t.WindowBG
	p.Text = t.LabelText
	p.Border = t.Border
	p.ScrollTrack = t.ScrollTrackBG
	p.ScrollThumb = t.ScrollThumbBG
	p.Selection = t.Accent
	return p
}

func isDarkColor(c color.RGBA) bool {
	return int(c.R)*299+int(c.G)*587+int(c.B)*114 < 128*1000
}
