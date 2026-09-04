package panetitle

import (
	"image/color"
	"testing"

	"github.com/oops1/headless-gui/v3/widget"
)

func TestXORInvertsEveryChannelAndKeepsOpaqueAlpha(t *testing.T) {
	cases := []struct {
		name string
		bg   color.RGBA
		want color.RGBA
	}{
		{"black", color.RGBA{A: 255}, color.RGBA{R: 255, G: 255, B: 255, A: 255}},
		{"white", color.RGBA{R: 255, G: 255, B: 255, A: 255}, color.RGBA{A: 255}},
		{"accent", color.RGBA{R: 0x00, G: 0x78, B: 0xD7, A: 255}, color.RGBA{R: 0xFF, G: 0x87, B: 0x28, A: 255}},
		{"transparent stays opaque", color.RGBA{R: 0x2D, G: 0x2D, B: 0x30}, color.RGBA{R: 0xD2, G: 0xD2, B: 0xCF, A: 255}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := XOR(tc.bg); got != tc.want {
				t.Fatalf("XOR(%v) = %v, want %v", tc.bg, got, tc.want)
			}
		})
	}
}

func TestApplySetsTitleTextToInvertedBackground(t *testing.T) {
	first := widget.NewDockPane("a", "A", nil)
	first.TitleBG = color.RGBA{R: 0x20, G: 0x20, B: 0x20, A: 255}
	first.TitleActiveBG = color.RGBA{R: 0x4C, G: 0xC2, B: 0xFF, A: 255}
	second := widget.NewDockPane("b", "B", nil)
	second.TitleBG = color.RGBA{R: 0xF3, G: 0xF3, B: 0xF3, A: 255}
	second.TitleActiveBG = color.RGBA{R: 0x00, G: 0x5F, B: 0xB8, A: 255}

	Apply([]*widget.DockPane{first, nil, second})

	if got := first.TitleText; got != (color.RGBA{R: 0xDF, G: 0xDF, B: 0xDF, A: 255}) {
		t.Fatalf("dark pane title = %v", got)
	}
	if got := first.TitleTextActive; got != (color.RGBA{R: 0xB3, G: 0x3D, B: 0x00, A: 255}) {
		t.Fatalf("dark pane active title = %v", got)
	}
	if got := second.TitleText; got != (color.RGBA{R: 0x0C, G: 0x0C, B: 0x0C, A: 255}) {
		t.Fatalf("light pane title = %v", got)
	}
	if got := second.TitleTextActive; got != (color.RGBA{R: 0xFF, G: 0xA0, B: 0x47, A: 255}) {
		t.Fatalf("light pane active title = %v", got)
	}
	for _, pane := range []*widget.DockPane{first, second} {
		if pane.TitleText != XOR(pane.TitleBG) || pane.TitleTextActive != XOR(pane.TitleActiveBG) {
			t.Fatalf("pane %q title is not the xor of the background it is drawn on", pane.ID)
		}
	}
}

func TestApplyAcceptsNoPanes(t *testing.T) {
	Apply(nil)
}
