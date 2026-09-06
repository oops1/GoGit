package app

import (
	"testing"

	"github.com/oops1/headless-gui/v3/engine"
	"github.com/oops1/headless-gui/v3/window"

	"github.com/oops1/gogit/internal/ui/icons"
)

func TestWindowIconImagesRendersEveryConfiguredSize(t *testing.T) {
	imgs := windowIconImages()
	if len(imgs) != len(windowIconSizes) {
		t.Fatalf("images = %d, want %d", len(imgs), len(windowIconSizes))
	}
	for i, size := range windowIconSizes {
		b := imgs[i].Bounds()
		if b.Dx() != size || b.Dy() != size {
			t.Fatalf("image %d bounds = %v, want %dx%d", i, b, size, size)
		}
	}
}

func TestWindowIconImagesReuseTheToolbarRasterization(t *testing.T) {
	imgs := windowIconImages()
	for i, size := range windowIconSizes {
		want := icons.ToolbarPlain(windowIconName, size)
		if imgs[i] != want {
			t.Fatalf("image %d does not reuse the cached rasterization for %q at size %d", i, windowIconName, size)
		}
	}
}

func TestApplyWindowIconLogsDebugWhenTheBackendHasNoNativeWindowYet(t *testing.T) {
	a := newTestApp(t)
	eng := engine.New(800, 600, 30)
	win := window.New(eng, "test")

	a.applyWindowIcon(win)
}

func TestWindowIconImagesSkipsSizesThatFailToRasterize(t *testing.T) {
	prevName := windowIconName
	windowIconName = "does-not-exist"
	t.Cleanup(func() { windowIconName = prevName })

	if imgs := windowIconImages(); len(imgs) != 0 {
		t.Fatalf("images = %d, want 0 for an icon that cannot be rasterized", len(imgs))
	}
}

func TestApplyWindowIconSkipsSetIconWhenNoSizeRenders(t *testing.T) {
	a := newTestApp(t)
	prevSizes := windowIconSizes
	windowIconSizes = nil
	t.Cleanup(func() { windowIconSizes = prevSizes })

	eng := engine.New(800, 600, 30)
	win := window.New(eng, "test")

	a.applyWindowIcon(win)
}
