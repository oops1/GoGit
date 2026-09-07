package settings

import (
	"image"
	"image/color"

	"github.com/oops1/headless-gui/v3/widget/svg"
)

const searchIconSVG = `<svg viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg">
  <circle cx="10" cy="10" r="7" fill="none" stroke="#808080" stroke-width="2.2"/>
  <line x1="15.3" y1="15.3" x2="21" y2="21" stroke="#808080" stroke-width="2.4" stroke-linecap="round"/>
</svg>`

var parseSearchIconSVG = svg.Parse

func buildSearchIcon(tint color.RGBA) image.Image {
	doc, err := parseSearchIconSVG([]byte(searchIconSVG))
	if err != nil {
		return nil
	}
	return doc.Rasterize(searchIconSize, searchIconSize, tint, true)
}
