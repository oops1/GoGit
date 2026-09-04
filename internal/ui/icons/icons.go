package icons

import (
	"image"
	"image/color"
	"sync"

	"github.com/oops1/headless-gui/v3/widget/svg"

	"github.com/oops1/gogit/internal/assets"
)

type source int

const (
	sourceStatus source = iota
	sourceTree
	sourceToolbar
)

type docKey struct {
	src  source
	name string
}

type rasterKey struct {
	src  source
	name string
	size int
}

var (
	mu       sync.Mutex
	docs     = map[docKey]*svg.Document{}
	docFail  = map[docKey]bool{}
	rendered = map[rasterKey]image.Image{}
)

var parseSVG = svg.Parse

var loadStatusIcon = assets.StatusIcon

var loadTreeIcon = assets.TreeIcon

var loadToolbarIcon = assets.Icon

func Status(name string, size int) image.Image {
	return render(sourceStatus, name, size)
}

func StatusTinted(name string, size int, tint color.RGBA) image.Image {
	return renderTinted(sourceStatus, name, size, tint)
}

func StatusMuted(name string, size int) image.Image {
	return muted(render(sourceStatus, name, size))
}

func Tree(name string, size int) image.Image {
	return render(sourceTree, name, size)
}

func TreeTinted(name string, size int, tint color.RGBA) image.Image {
	return renderTinted(sourceTree, name, size, tint)
}

func ToolbarPlain(name string, size int) image.Image {
	return render(sourceToolbar, name, size)
}

func ToolbarMuted(name string, size int) image.Image {
	return muted(render(sourceToolbar, name, size))
}

func Toolbar(name string, size int, tint color.RGBA) image.Image {
	return renderTinted(sourceToolbar, name, size, tint)
}

func render(src source, name string, size int) image.Image {
	if size <= 0 {
		return nil
	}
	rk := rasterKey{src: src, name: name, size: size}

	mu.Lock()
	if img, ok := rendered[rk]; ok {
		mu.Unlock()
		return img
	}
	mu.Unlock()

	doc := document(src, name)
	if doc == nil {
		return nil
	}
	var img image.Image = doc.Rasterize(size, size, color.RGBA{}, false)

	mu.Lock()
	rendered[rk] = img
	mu.Unlock()
	return img
}

func renderTinted(src source, name string, size int, tint color.RGBA) image.Image {
	if size <= 0 {
		return nil
	}
	doc := document(src, name)
	if doc == nil {
		return nil
	}
	return doc.RasterizeCached(size, size, tint, true)
}

func document(src source, name string) *svg.Document {
	dk := docKey{src: src, name: name}

	mu.Lock()
	if doc, ok := docs[dk]; ok {
		mu.Unlock()
		return doc
	}
	if docFail[dk] {
		mu.Unlock()
		return nil
	}
	mu.Unlock()

	data, err := load(src, name)
	if err != nil {
		markFailed(dk)
		return nil
	}
	doc, err := parseSVG(data)
	if err != nil {
		markFailed(dk)
		return nil
	}

	mu.Lock()
	docs[dk] = doc
	mu.Unlock()
	return doc
}

func markFailed(dk docKey) {
	mu.Lock()
	docFail[dk] = true
	mu.Unlock()
}

func load(src source, name string) ([]byte, error) {
	switch src {
	case sourceTree:
		return loadTreeIcon(name)
	case sourceToolbar:
		return loadToolbarIcon(name)
	default:
		return loadStatusIcon(name)
	}
}

func resetForTest() {
	mu.Lock()
	docs = map[docKey]*svg.Document{}
	docFail = map[docKey]bool{}
	rendered = map[rasterKey]image.Image{}
	mu.Unlock()
}

const mutedAlpha = 0.5

func muted(src image.Image) image.Image {
	if src == nil {
		return nil
	}
	b := src.Bounds()
	out := image.NewRGBA(b)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, a := src.At(x, y).RGBA()
			if a == 0 {
				continue
			}
			gray := (299*r + 587*g + 114*bl) / 1000
			out.Set(x, y, color.RGBA{
				R: uint8(float64(gray>>8) * mutedAlpha),
				G: uint8(float64(gray>>8) * mutedAlpha),
				B: uint8(float64(gray>>8) * mutedAlpha),
				A: uint8(float64(a>>8) * mutedAlpha),
			})
		}
	}
	return out
}
