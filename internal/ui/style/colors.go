package style

import (
	"fmt"
	"image/color"
	"strconv"
	"strings"

	"github.com/oops1/headless-gui/v3/widget"
)

type Colors struct {
	Accent    color.RGBA
	Surface   color.RGBA
	Field     color.RGBA
	Text      color.RGBA
	Secondary color.RGBA
}

func ParseColor(s string) (color.RGBA, bool) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "#")
	switch len(s) {
	case 3:
		r, ok1 := parseHexDigit(s[0])
		g, ok2 := parseHexDigit(s[1])
		b, ok3 := parseHexDigit(s[2])
		if !ok1 || !ok2 || !ok3 {
			return color.RGBA{}, false
		}
		return color.RGBA{R: r * 17, G: g * 17, B: b * 17, A: 0xFF}, true
	case 6:
		v, err := strconv.ParseUint(s, 16, 32)
		if err != nil {
			return color.RGBA{}, false
		}
		return color.RGBA{
			R: uint8(v >> 16),
			G: uint8(v >> 8),
			B: uint8(v),
			A: 0xFF,
		}, true
	default:
		return color.RGBA{}, false
	}
}

func parseHexDigit(c byte) (uint8, bool) {
	v, err := strconv.ParseUint(string(c), 16, 8)
	if err != nil {
		return 0, false
	}
	return uint8(v), true
}

func HexColor(c color.RGBA) string {
	if c.A == 0 {
		return ""
	}
	return fmt.Sprintf("#%02X%02X%02X", c.R, c.G, c.B)
}

func Custom(base *widget.Theme, c Colors) *widget.Theme {
	var t *widget.Theme
	if c.Accent.A != 0 {
		t = Tinted(base, c.Accent)
	} else {
		clone := *base
		t = &clone
	}
	if c.Surface.A != 0 {
		t.WindowBG = c.Surface
		t.PanelBG = c.Surface
		t.DialogBG = c.Surface
		t.TabContentBG = c.Surface
		t.StatusBarBG = c.Surface
		t.SplitterBG = c.Surface
		t.MenuBG = c.Surface
		t.DropBG = c.Surface
	}
	if c.Field.A != 0 {
		t.InputBG = c.Field
		t.ProgressBG = c.Field
		t.CheckBG = c.Field
		t.ScrollTrackBG = c.Field
	}
	if c.Text.A != 0 {
		t.LabelText = c.Text
		t.InputText = c.Text
		t.BtnText = c.Text
		t.CheckText = c.Text
		t.TreeText = c.Text
		t.DropText = c.Text
		t.TabText = c.Text
		t.TabActiveText = c.Text
		t.HeaderText = c.Text
		t.StatusBarText = c.Text
	}
	if c.Secondary.A != 0 {
		t.SecondaryText = c.Secondary
		t.InputPlaceholder = c.Secondary
	}
	return t
}
