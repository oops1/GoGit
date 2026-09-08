package icons

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/oops1/gogit/internal/assets"
)

func TestPreviewMenuIcons(t *testing.T) {
	dir := os.Getenv("GOGIT_PREVIEW_DIR")
	if dir == "" {
		t.Skip("GOGIT_PREVIEW_DIR not set")
	}
	names := append(assets.MenuIconNames(), "commit", "pull", "push", "sync")
	sort.Strings(names)
	const size, cell, cols = 32, 56, 7
	rows := (len(names) + cols - 1) / cols
	out := image.NewRGBA(image.Rect(0, 0, cols*cell, rows*cell))
	bg := color.RGBA{0x1E, 0x1E, 0x1E, 0xFF}
	for y := out.Rect.Min.Y; y < out.Rect.Max.Y; y++ {
		for x := out.Rect.Min.X; x < out.Rect.Max.X; x++ {
			out.Set(x, y, bg)
		}
	}
	tint := color.RGBA{0xE6, 0xED, 0xF3, 0xFF}
	for i, name := range names {
		img := Menu(name, size, tint)
		if img == nil {
			img = Toolbar(name, size, tint)
		}
		if img == nil {
			continue
		}
		ox := (i%cols)*cell + (cell-size)/2
		oy := (i/cols)*cell + (cell-size)/2
		b := img.Bounds()
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				r, g, bl, a := img.At(x, y).RGBA()
				if a == 0 {
					continue
				}
				al := float64(a) / 65535
				dst := out.RGBAAt(ox+x-b.Min.X, oy+y-b.Min.Y)
				out.SetRGBA(ox+x-b.Min.X, oy+y-b.Min.Y, color.RGBA{
					R: uint8(float64(r>>8)*al + float64(dst.R)*(1-al)),
					G: uint8(float64(g>>8)*al + float64(dst.G)*(1-al)),
					B: uint8(float64(bl>>8)*al + float64(dst.B)*(1-al)),
					A: 0xFF,
				})
			}
		}
	}
	f, err := os.Create(filepath.Join(dir, "menu-icons.png"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if err := png.Encode(f, out); err != nil {
		t.Fatal(err)
	}
	t.Log(names)
}
