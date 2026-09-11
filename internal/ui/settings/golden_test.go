package settings

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
	"github.com/oops1/gogit/internal/i18n"
)

const goldenFrameOS = "windows"

var updateGolden = flag.Bool("update", false, "rewrite golden frames in testdata/golden")

func goldenSampleCredentials() []SecretEntry {
	return []SecretEntry{
		{Resource: "https://github.com/oops1/gogit.git", Username: "oops1.vc@gmail.com", Type: "token", Status: StatusSaved},
		{Resource: "https://gitlab.example.com/team/project", Username: "valeriy", Type: "password", Status: StatusAuthRequired},
		{Resource: "https://bitbucket.org/org/repo", Username: "svc-account", Type: "token", Status: StatusError},
	}
}

func goldenSampleKeys() []KeyEntry {
	return []KeyEntry{
		{Host: defaultKeyHost, Path: "C:\\Users\\valer\\.ssh\\id_ed25519", Type: "ed25519", Status: StatusSaved},
		{Host: "github.com", Path: "~/.ssh/id_rsa", Type: "rsa", Status: StatusAuthRequired},
	}
}

func renderSettingsFrame(t *testing.T, theme *widget.Theme, section string) *image.RGBA {
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

	view, err := NewView(eng, []string{"en", "ru"}, Model{
		Language:      "en",
		Theme:         config.ThemeSystem,
		ShowToolbar:   true,
		ShowStatusBar: true,
		LogMaxCount:   500,
		AutoFetch:     true,
		FetchInterval: 300,
		DefaultRemote: "origin",
		PullStrategy:  config.PullStrategyFF,
	})
	if err != nil {
		t.Fatal(err)
	}
	view.SetCredentials(goldenSampleCredentials())
	view.SetKeys(goldenSampleKeys())
	view.SetCredentialSourceInfo(
		"C:\\Users\\valer\\AppData\\Roaming\\GoGit\\vault.db",
		"Argon2id, AES-256-GCM",
		[]CredentialHelperEntry{{Name: "manager-core", Supported: true}},
	)
	view.SetSecretsStatus(i18n.T("Dialog.Settings.Secrets.Status.Saved"), widget.CurrentTheme().SecondaryText)
	view.SetSection(section)

	dlg := view.Dialog()
	widget.ApplyThemeTree(dlg, theme)
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

func TestSettingsGolden(t *testing.T) {
	if runtime.GOOS != goldenFrameOS && !*updateGolden {
		t.Skipf("golden frames are recorded on %s: text and window chrome rasterise differently elsewhere", goldenFrameOS)
	}
	themes := []struct {
		name  string
		theme *widget.Theme
	}{
		{"light", widget.Win11LightTheme()},
		{"dark", widget.Win11DarkTheme()},
	}
	sections := []string{"general", "git", "credentials", "ssh"}

	for _, th := range themes {
		for _, section := range sections {
			t.Run(section+"-"+th.name, func(t *testing.T) {
				got := renderSettingsFrame(t, th.theme, section)
				assertGolden(t, section+"-"+th.name, got)
			})
		}
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
