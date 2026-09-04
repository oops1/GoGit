package settings

import (
	"image"
	"os"
	"testing"
	"time"

	"github.com/oops1/headless-gui/v3/engine"
	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/config"
	"github.com/oops1/gogit/internal/i18n"
)

func TestPreviewSettingsDialog(t *testing.T) {
	dir := os.Getenv("GOGIT_PREVIEW_DIR")
	if dir == "" {
		t.Skip("GOGIT_PREVIEW_DIR not set")
	}
	themes := []struct {
		name  string
		theme *widget.Theme
	}{
		{"dark", widget.Win11DarkTheme()},
		{"light", widget.Win11LightTheme()},
	}
	tabs := []struct {
		name  string
		index int
	}{
		{"general", 0},
		{"git", 1},
	}
	for _, theme := range themes {
		for _, tab := range tabs {
			widget.ClearStrings()
			if _, err := i18n.Install(""); err != nil {
				t.Fatal(err)
			}
			i18n.Apply("en")

			eng := engine.New(800, 600, 30)
			eng.SetTheme(theme.theme)
			root := widget.NewPanel(theme.theme.WindowBG)
			root.SetBounds(image.Rect(0, 0, 800, 600))
			eng.SetRoot(root)

			view, err := NewView(eng, []string{"en", "ru"}, Model{
				Language:      "en",
				Theme:         config.ThemeDark,
				ShowToolbar:   true,
				ShowStatusBar: true,
				LogMaxCount:   500,
				AutoFetch:     true,
				FetchInterval: 300,
			})
			if err != nil {
				t.Fatal(err)
			}
			view.tabs.SetActive(tab.index)
			eng.ShowModal(view.Dialog())

			eng.SaveFrames(dir + "/settings-" + tab.name + "-" + theme.name)
			eng.Start()
			time.Sleep(700 * time.Millisecond)
			eng.Stop()

			widget.ClearStrings()
		}
	}
}
