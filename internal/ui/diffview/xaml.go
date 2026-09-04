package diffview

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/oops1/headless-gui/v3/widget"
)

const XAMLTag = "DiffView"

var (
	ErrUnknownMode = errors.New("diffview: unknown mode")
	ErrBadAttr     = errors.New("diffview: invalid attribute")
)

func Register() {
	widget.RegisterXAMLWidget(XAMLTag, buildFromXAML)
}

func ParseMode(text string) (Mode, error) {
	switch strings.ToLower(strings.ReplaceAll(text, "-", "")) {
	case "sidebyside", "side by side", "split":
		return SideBySide, nil
	case "unified", "inline":
		return Unified, nil
	default:
		return SideBySide, fmt.Errorf("%w: %s", ErrUnknownMode, text)
	}
}

func buildFromXAML(attrs widget.XAMLAttrs) (widget.Widget, error) {
	dv := New()
	if value := attrs.Attr("Mode"); value != "" {
		mode, err := ParseMode(value)
		if err != nil {
			return nil, err
		}
		dv.SetMode(mode)
	}
	if value := attrs.Attr("FontFamily", "FontName"); value != "" {
		dv.SetFontFamily(value)
	}
	if value := attrs.Attr("FontSize"); value != "" {
		size, err := strconv.ParseFloat(value, 64)
		if err != nil || size <= 0 {
			return nil, fmt.Errorf("%w: FontSize=%s", ErrBadAttr, value)
		}
		dv.SetFontSize(size)
	}
	if value := attrs.Attr("RowHeight"); value != "" {
		height, err := strconv.Atoi(value)
		if err != nil || height <= 0 {
			return nil, fmt.Errorf("%w: RowHeight=%s", ErrBadAttr, value)
		}
		dv.SetRowHeight(height)
	}
	return dv, nil
}
