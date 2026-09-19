package toolbar

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

	"github.com/oops1/gogit/internal/i18n"
)

const goldenFrameOS = "windows"

var updateGolden = flag.Bool("update", false, "rewrite golden frames in testdata/golden")

func goldenModel() *Model {
	catalog := []Entry{
		{ID: SeparatorID, Label: "—— separator ——"},
		{ID: StretchID, Label: "<-> stretch <->"},
		{ID: "local.commit", Label: "Commit"},
		{ID: "local.discard", Label: "Discard"},
		{ID: "query.log", Label: "Log"},
		{ID: "branch.merge", Label: "Merge"},
		{ID: "remote.pull", Label: "Pull"},
		{ID: "remote.push", Label: "Push"},
		{ID: "local.stage", Label: "Stage"},
		{ID: "remote.sync", Label: "Sync"},
		{ID: "local.unstage", Label: "Unstage"},
	}
	row := []string{"remote.pull", "remote.sync", "remote.push", SeparatorID, "local.commit", StretchID, "query.log"}
	return NewModel(catalog, row, row, true)
}

func renderDialogFrame(t *testing.T, theme *widget.Theme) *image.RGBA {
	t.Helper()
	widget.ClearStrings()
	t.Cleanup(widget.ClearStrings)
	if _, err := i18n.Install(""); err != nil {
		t.Fatal(err)
	}
	i18n.Apply("en")

	eng := engine.New(1, 1, 30)
	t.Cleanup(eng.Stop)
	eng.SetTheme(theme)

	view, err := NewView(goldenModel())
	if err != nil {
		t.Fatal(err)
	}
	pick(view.selected, 4)
	pick(view.available, 2)

	dlg := view.Dialog()
	b := dlg.Bounds()
	canvas := engine.New(b.Dx(), b.Dy(), 30)
	t.Cleanup(canvas.Stop)
	canvas.SetTheme(theme)
	widget.ApplyThemeTree(dlg, theme)
	view.Restyle(theme)
	canvas.SetRoot(dlg)
	frame := canvas.RenderOnce()
	if frame == nil {
		t.Fatal("engine produced no frame")
	}
	return frame
}

func TestConfigureToolbarGolden(t *testing.T) {
	if runtime.GOOS != goldenFrameOS && !*updateGolden {
		t.Skipf("golden frames are recorded on %s: text and window chrome rasterise differently elsewhere", goldenFrameOS)
	}
	for _, c := range []struct {
		name  string
		theme *widget.Theme
	}{
		{"configure-light", widget.Win11LightTheme()},
		{"configure-dark", widget.Win11DarkTheme()},
	} {
		t.Run(c.name, func(t *testing.T) {
			assertGolden(t, c.name, renderDialogFrame(t, c.theme))
		})
	}
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
