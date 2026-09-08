package about

import (
	"image"
	"os"
	"testing"
	"time"

	"github.com/oops1/headless-gui/v3/engine"
	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/i18n"
)

func TestPreviewAboutDialog(t *testing.T) {
	dir := os.Getenv("GOGIT_PREVIEW_DIR")
	if dir == "" {
		t.Skip("GOGIT_PREVIEW_DIR not set")
	}
	variants := []struct {
		name  string
		lang  string
		theme *widget.Theme
	}{
		{"light", "ru", widget.Win11LightTheme()},
		{"dark", "ru", widget.Win11DarkTheme()},
		{"en", "en", widget.Win11LightTheme()},
	}
	for _, variant := range variants {
		widget.ClearStrings()
		if _, err := i18n.Install(""); err != nil {
			t.Fatal(err)
		}
		i18n.Apply(variant.lang)

		eng := engine.New(700, 540, 30)
		eng.SetTheme(variant.theme)
		root := widget.NewPanel(variant.theme.WindowBG)
		root.SetBounds(image.Rect(0, 0, 700, 540))
		eng.SetRoot(root)

		view, err := NewView(Info{Version: "v1.1.0", Architecture: "amd64"})
		if err != nil {
			t.Fatal(err)
		}
		eng.ShowModal(view.Dialog())
		view.Restyle(variant.theme)

		eng.SaveFrames(dir + "/about-" + variant.name)
		eng.Start()
		time.Sleep(500 * time.Millisecond)
		eng.Stop()
		widget.ClearStrings()
	}
}
