package reposettings

import (
	"image"
	"os"
	"testing"
	"time"

	"github.com/oops1/headless-gui/v3/engine"
	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/i18n"
)

func TestPreviewRepoSettingsDialog(t *testing.T) {
	dir := os.Getenv("GOGIT_PREVIEW_DIR")
	if dir == "" {
		t.Skip("GOGIT_PREVIEW_DIR not set")
	}
	for _, variant := range []struct {
		name   string
		lang   string
		theme  *widget.Theme
		height int
	}{
		{"dark", "ru", widget.Win11DarkTheme(), 860},
		{"light", "ru", widget.Win11LightTheme(), 860},
		{"en", "en", widget.Win11LightTheme(), 860},
		{"compact", "ru", widget.Win11LightTheme(), 600},
	} {
		widget.ClearStrings()
		if _, err := i18n.Install(""); err != nil {
			t.Fatal(err)
		}
		i18n.Apply(variant.lang)

		eng := engine.New(800, variant.height, 30)
		eng.SetTheme(variant.theme)
		root := widget.NewPanel(variant.theme.WindowBG)
		root.SetBounds(image.Rect(0, 0, 800, variant.height))
		eng.SetRoot(root)

		view, err := NewView()
		if err != nil {
			t.Fatal(err)
		}
		view.SetRemotes([]string{"origin", "backup"})
		view.SetInherited(Inherited{UserName: "Ann Global", UserEmail: "ann@example.com", DefaultRemote: "origin"})
		view.Apply(Settings{
			Name:         "Go.Git",
			Path:         `D:\Projects\GoLang\Go.Git`,
			PullStrategy: PullFF,
			AutoFetch:    AutoFetchOn,
		})
		view.FitHeight(variant.height)
		eng.ShowModal(view.Dialog())
		view.Restyle(variant.theme)
		if !view.Dialog().Bounds().In(root.Bounds()) {
			t.Fatalf("dialog %v does not fit the window %v", view.Dialog().Bounds(), root.Bounds())
		}

		eng.SaveFrames(dir + "/reposettings-" + variant.name)
		eng.Start()
		time.Sleep(700 * time.Millisecond)
		eng.Stop()
		widget.ClearStrings()
	}
}
