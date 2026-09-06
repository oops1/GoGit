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
	sections := []string{"general", "git", "credentials", "ssh"}
	for _, theme := range themes {
		for _, section := range sections {
			widget.ClearStrings()
			if _, err := i18n.Install(""); err != nil {
				t.Fatal(err)
			}
			i18n.Apply("en")

			eng := engine.New(previewCanvasWidth, previewCanvasHeight, 30)
			eng.SetTheme(theme.theme)
			root := widget.NewPanel(theme.theme.WindowBG)
			root.SetBounds(image.Rect(0, 0, previewCanvasWidth, previewCanvasHeight))
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
			view.SetSection(section)
			eng.ShowModal(view.Dialog())

			eng.SaveFrames(dir + "/settings-" + section + "-" + theme.name)
			eng.Start()
			time.Sleep(700 * time.Millisecond)
			eng.Stop()

			widget.ClearStrings()
		}
	}
}
