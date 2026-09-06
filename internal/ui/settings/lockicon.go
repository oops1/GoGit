package settings

import (
	"image"
	"image/color"

	"github.com/oops1/headless-gui/v3/widget/svg"
)

const lockIconSize = 14

const lockIconSVG = `<svg viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg">
  <rect x="5" y="11" width="14" height="10" rx="2" fill="none" stroke="#808080" stroke-width="2.2"/>
  <path d="M8 11V8a4 4 0 0 1 8 0v3" fill="none" stroke="#808080" stroke-width="2.2"/>
</svg>`

var parseLockIconSVG = svg.Parse

func buildLockIcon(tint color.RGBA) image.Image {
	doc, err := parseLockIconSVG([]byte(lockIconSVG))
	if err != nil {
		return nil
	}
	return doc.Rasterize(lockIconSize, lockIconSize, tint, true)
}
