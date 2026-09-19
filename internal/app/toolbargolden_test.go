package app

import (
	"flag"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/oops1/headless-gui/v3/engine"
	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/config"
)

const (
	goldenFrameOS     = "windows"
	goldenToolbarSize = 64
)

var updateGolden = flag.Bool("update", false, "rewrite golden frames in testdata/golden")

func renderToolbarFrame(t *testing.T, theme string, captions bool) *image.RGBA {
	t.Helper()
	cfg := config.Default()
	cfg.Theme = theme
	cfg.UI.ToolbarCaptions = captions
	a := newTestAppWithConfig(t, cfg)
	a.SetActiveRepository("demo", false)
	a.setHasRemotes(true)
	a.setHasStagedChanges(true)
	a.setFilesSelected(true)

	panel, ok := a.toolbarPanel()
	if !ok {
		t.Fatal("the toolbar panel is missing")
	}
	height := goldenToolbarSize
	if !captions {
		height = toolbarCompactHeight + 8
	}
	panel.SetBounds(rectOfSize(0, 0, config.MinWindowWidth, height))

	canvas := engine.New(config.MinWindowWidth, height, 30)
	t.Cleanup(canvas.Stop)
	canvas.SetTheme(a.theme())
	widget.ApplyThemeTree(panel, a.theme())
	a.applyToolbarIcons(a.theme())
	panel.SetBounds(rectOfSize(0, 0, config.MinWindowWidth, height))
	canvas.SetRoot(panel)
	frame := canvas.RenderOnce()
	if frame == nil {
		t.Fatal("engine produced no frame")
	}
	return frame
}

func TestToolbarGolden(t *testing.T) {
	if runtime.GOOS != goldenFrameOS && !*updateGolden {
		t.Skipf("golden frames are recorded on %s: text rasterises differently elsewhere", goldenFrameOS)
	}
	cases := []struct {
		name     string
		theme    string
		captions bool
	}{
		{"toolbar-light", config.ThemeLight, true},
		{"toolbar-dark", config.ThemeDark, true},
		{"toolbar-compact-light", config.ThemeLight, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assertGoldenFrame(t, c.name, renderToolbarFrame(t, c.theme, c.captions))
		})
	}
}

func assertGoldenFrame(t *testing.T, name string, got *image.RGBA) {
	t.Helper()
	path := filepath.Join("testdata", "golden", name+".png")
	if *updateGolden {
		writeGoldenFrame(t, path, got)
		return
	}
	want := readGoldenFrame(t, path)
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

func writeGoldenFrame(t *testing.T, path string, img *image.RGBA) {
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

func readGoldenFrame(t *testing.T, path string) *image.RGBA {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	img, err := png.Decode(file)
	if err != nil {
		t.Fatal(err)
	}
	rgba, ok := img.(*image.RGBA)
	if !ok {
		rgba = image.NewRGBA(img.Bounds())
		for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y; y++ {
			for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
				rgba.Set(x, y, img.At(x, y))
			}
		}
	}
	return rgba
}
