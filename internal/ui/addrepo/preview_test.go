package addrepo

import (
	"image"
	"os"
	"testing"
	"time"

	"github.com/oops1/headless-gui/v3/engine"
	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/i18n"
)

func TestPreviewAddRepoDialog(t *testing.T) {
	dir := os.Getenv("GOGIT_PREVIEW_DIR")
	if dir == "" {
		t.Skip("GOGIT_PREVIEW_DIR not set")
	}
	for _, theme := range []struct {
		name  string
		theme *widget.Theme
	}{
		{"dark", widget.Win11DarkTheme()},
		{"light", widget.Win11LightTheme()},
	} {
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

		view, err := NewView(eng, Request{Mode: ModeCreate, Path: `C:\repos\myproject`})
		if err != nil {
			t.Fatal(err)
		}
		eng.ShowModal(view.Dialog())

		eng.SaveFrames(dir + "/addrepo-" + theme.name)
		eng.Start()
		time.Sleep(700 * time.Millisecond)
		eng.Stop()
		widget.ClearStrings()
	}
}
