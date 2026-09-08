package worktree

import (
	"image"
	"os"
	"testing"
	"time"

	"github.com/oops1/headless-gui/v3/engine"
	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/i18n"
)

func TestPreviewWorktreeDialog(t *testing.T) {
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

		view, err := NewView(eng)
		if err != nil {
			t.Fatal(err)
		}
		view.SetKnown(Known{Branches: []string{"main", "develop"}, Head: "main"})
		view.SetParentDirectory(`C:\repos`)
		view.branchInput.SetText("feature/login")
		view.onBranchTyped()
		eng.ShowModal(view.Dialog())

		eng.SaveFrames(dir + "/worktree-" + variant.name)
		eng.Start()
		time.Sleep(700 * time.Millisecond)
		eng.Stop()
		widget.ClearStrings()
	}
}
