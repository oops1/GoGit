package search

import (
	"image"
	"os"
	"testing"
	"time"

	"github.com/oops1/headless-gui/v3/engine"
	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/i18n"
)

func TestPreviewSearchDialog(t *testing.T) {
	dir := os.Getenv("GOGIT_PREVIEW_DIR")
	if dir == "" {
		t.Skip("GOGIT_PREVIEW_DIR not set")
	}
	for _, lang := range []string{"ru", "en"} {
		widget.ClearStrings()
		if _, err := i18n.Install(""); err != nil {
			t.Fatal(err)
		}
		i18n.Apply(lang)

		theme := widget.Win11LightTheme()
		eng := engine.New(720, 520, 30)
		eng.SetTheme(theme)
		root := widget.NewPanel(theme.WindowBG)
		root.SetBounds(image.Rect(0, 0, 720, 520))
		eng.SetRoot(root)

		view, err := NewView(eng)
		if err != nil {
			t.Fatal(err)
		}
		view.SetRoot(`D:\Projects\GoLang`)
		view.SetResults([]Found{
			{Path: `D:\Projects\GoLang\Go.Git`},
			{Path: `D:\Projects\GoLang\headless-gui`, Worktree: true},
			{Path: `D:\Projects\GoLang\mirror.git`, Bare: true},
		})
		view.SetStatus(i18n.Tf("Dialog.Search.Found", 3), theme.LabelText)
		eng.ShowModal(view.Dialog())
		view.Restyle(theme)

		eng.SaveFrames(dir + "/search-" + lang)
		eng.Start()
		time.Sleep(500 * time.Millisecond)
		eng.Stop()
		widget.ClearStrings()
	}
}
