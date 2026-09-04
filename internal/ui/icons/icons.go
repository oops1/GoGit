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

func Status(name string, size int) image.Image {
	return render(sourceStatus, name, size)
}

func Tree(name string, size int) image.Image {
	return render(sourceTree, name, size)
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
	if src == sourceTree {
		return loadTreeIcon(name)
	}
	return loadStatusIcon(name)
}

func resetForTest() {
	mu.Lock()
	docs = map[docKey]*svg.Document{}
	docFail = map[docKey]bool{}
	rendered = map[rasterKey]image.Image{}
	mu.Unlock()
}
