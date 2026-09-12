package merge

import (
	"image"
	"os"
	"testing"
	"time"

	"github.com/oops1/headless-gui/v3/engine"
	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/i18n"
)

func TestPreviewMergeDialog(t *testing.T) {
	dir := os.Getenv("GOGIT_PREVIEW_DIR")
	if dir == "" {
		t.Skip("GOGIT_PREVIEW_DIR not set")
	}
	for _, variant := range []struct {
		name  string
		lang  string
		theme *widget.Theme
	}{
		{"dark", "ru", widget.Win11DarkTheme()},
		{"light", "ru", widget.Win11LightTheme()},
		{"en", "en", widget.Win11LightTheme()},
	} {
		widget.ClearStrings()
		if _, err := i18n.Install(""); err != nil {
			t.Fatal(err)
		}
		i18n.Apply(variant.lang)

		eng := engine.New(800, 600, 30)
		eng.SetTheme(variant.theme)
		root := widget.NewPanel(variant.theme.WindowBG)
		root.SetBounds(image.Rect(0, 0, 800, 600))
		eng.SetRoot(root)

		view, err := NewView()
		if err != nil {
			t.Fatal(err)
		}
		view.SetKnown(Known{Current: "main", Candidates: []string{"feature/login", "origin/develop", "v1.3.3"}}, "feature/login")
		eng.ShowModal(view.Dialog())
		view.Restyle(variant.theme)

		eng.SaveFrames(dir + "/merge-" + variant.name)
		eng.Start()
		time.Sleep(700 * time.Millisecond)
		eng.Stop()
		widget.ClearStrings()
	}
}
