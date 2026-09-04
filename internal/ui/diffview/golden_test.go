package diffview

import (
	"flag"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/oops1/headless-gui/v3/engine"
)

var updateGolden = flag.Bool("update", false, "rewrite golden frames in testdata/golden")

func renderView(t *testing.T, width, height int, prepare func(*DiffView)) *image.RGBA {
	t.Helper()
	eng := engine.New(width, height, 30)
	t.Cleanup(eng.Stop)
	dv := New()
	dv.SetFontFamily("")
	prepare(dv)
	eng.SetRoot(dv)
	frame := eng.RenderOnce()
	if frame == nil {
		t.Fatal("engine produced no frame")
	}
	return frame
}

func assertGolden(t *testing.T, name string, got *image.RGBA) {
	t.Helper()
	path := filepath.Join("testdata", "golden", name+".png")
	if *updateGolden {
		writeGolden(t, path, got)
		return
	}
	want := readGolden(t, path)
	if !want.Bounds().Eq(got.Bounds()) {
		t.Fatalf("%s: bounds = %v, want %v", name, got.Bounds(), want.Bounds())
	}
	for y := got.Bounds().Min.Y; y < got.Bounds().Max.Y; y++ {
		for x := got.Bounds().Min.X; x < got.Bounds().Max.X; x++ {
			if got.RGBAAt(x, y) != want.RGBAAt(x, y) {
				t.Fatalf("%s: pixel (%d,%d) = %v, want %v (run go test -update to refresh)",
					name, x, y, got.RGBAAt(x, y), want.RGBAAt(x, y))
			}
		}
	}
}

func writeGolden(t *testing.T, path string, img *image.RGBA) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
	}()
	if err := png.Encode(file, img); err != nil {
		t.Fatal(err)
	}
}

func readGolden(t *testing.T, path string) *image.RGBA {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
	}()
	img, err := png.Decode(file)
	if err != nil {
		t.Fatal(err)
	}
	rgba, ok := img.(*image.RGBA)
	if ok {
		return rgba
	}
	rgba = image.NewRGBA(img.Bounds())
	for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y; y++ {
		for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
			rgba.Set(x, y, img.At(x, y))
		}
	}
	return rgba
}
