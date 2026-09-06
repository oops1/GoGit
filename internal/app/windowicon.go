package app

import (
	"errors"
	"image"

	"github.com/oops1/headless-gui/v3/window"

	"github.com/oops1/gogit/internal/ui/icons"
)

var windowIconName = "app"

var windowIconSizes = []int{16, 32, 48, 256}

func windowIconImages() []image.Image {
	imgs := make([]image.Image, 0, len(windowIconSizes))
	for _, size := range windowIconSizes {
		img := icons.ToolbarPlain(windowIconName, size)
		if img == nil {
			continue
		}
		imgs = append(imgs, img)
	}
	return imgs
}

func (a *App) applyWindowIcon(win *window.Window) {
	imgs := windowIconImages()
	if len(imgs) == 0 {
		return
	}
	if err := win.SetIcon(imgs...); err != nil {
		if errors.Is(err, window.ErrIconUnsupported) {
			a.log.Debug("window icon unsupported by the backend", "error", err)
			return
		}
		a.log.Warn("set window icon failed", "error", err)
	}
}
