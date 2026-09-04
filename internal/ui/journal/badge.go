package journal

import (
	"hash/fnv"
	"image"
	"image/color"
	"sync"
)

const badgeCornerRadius = 3

const badgeLuminanceThreshold = 140

var badgeTextOnLight = color.RGBA{R: 0x0D, G: 0x11, B: 0x17, A: 0xFF}

var badgeTextOnDark = color.RGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF}

var badgePalette = []color.RGBA{
	{R: 0x2F, G: 0x81, B: 0xF7, A: 0xFF},
	{R: 0x2E, G: 0xA0, B: 0x43, A: 0xFF},
	{R: 0xE5, G: 0xA1, B: 0x1B, A: 0xFF},
	{R: 0xD1, G: 0x38, B: 0x3D, A: 0xFF},
	{R: 0x8B, G: 0x94, B: 0x9E, A: 0xFF},
	{R: 0xA3, G: 0x71, B: 0xF7, A: 0xFF},
	{R: 0x39, G: 0xB7, B: 0xA8, A: 0xFF},
	{R: 0xF0, G: 0x88, B: 0x3E, A: 0xFF},
	{R: 0x56, G: 0xD4, B: 0xDD, A: 0xFF},
	{R: 0xDB, G: 0x61, B: 0xA2, A: 0xFF},
}

func authorColor(author string) color.RGBA {
	h := fnv.New32a()
	_, _ = h.Write([]byte(author))
	idx := h.Sum32() % uint32(len(badgePalette))
	return badgePalette[idx]
}

func luminance(c color.RGBA) int {
	return (299*int(c.R) + 587*int(c.G) + 114*int(c.B)) / 1000
}

func badgeTextColor(bg color.RGBA) color.RGBA {
	if luminance(bg) >= badgeLuminanceThreshold {
		return badgeTextOnLight
	}
	return badgeTextOnDark
}

type badgeKey struct {
	author string
	size   int
}

var (
	badgeMu    sync.Mutex
	badgeCache = map[badgeKey]image.Image{}
)

func AuthorBadge(author string, size int) image.Image {
	if size <= 0 || author == "" {
		return nil
	}
	key := badgeKey{author: author, size: size}

	badgeMu.Lock()
	if img, ok := badgeCache[key]; ok {
		badgeMu.Unlock()
		return img
	}
	badgeMu.Unlock()

	img := renderBadge(authorColor(author), size)

	badgeMu.Lock()
	badgeCache[key] = img
	badgeMu.Unlock()
	return img
}

func resetBadgeCacheForTest() {
	badgeMu.Lock()
	badgeCache = map[badgeKey]image.Image{}
	badgeMu.Unlock()
}

func renderBadge(c color.RGBA, size int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			if insideRoundedSquare(x, y, size, badgeCornerRadius) {
				img.SetRGBA(x, y, c)
			}
		}
	}
	return img
}

func insideRoundedSquare(x, y, size, radius int) bool {
	if radius <= 0 {
		return true
	}
	left, right := x < radius, x >= size-radius
	top, bottom := y < radius, y >= size-radius
	if !((left || right) && (top || bottom)) {
		return true
	}
	cx := radius - 1
	if right {
		cx = size - radius
	}
	cy := radius - 1
	if bottom {
		cy = size - radius
	}
	dx, dy := x-cx, y-cy
	return dx*dx+dy*dy <= radius*radius
}
