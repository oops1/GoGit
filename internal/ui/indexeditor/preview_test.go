package indexeditor

import (
	"image"
	"os"
	"testing"
	"time"

	"github.com/oops1/headless-gui/v3/engine"
	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/i18n"
)

const previewHead = "# Настройки сборки\nВерсия: 1.3\nАвтор: команда проекта\nПапка вывода: dist\nТесты: запускать\nЖурнал: подробный\n"

const previewIndex = "# Настройки сборки\nВерсия: 1.4\nАвтор: команда проекта\nПапка вывода: dist\nТесты: запускать\nЖурнал: подробный\n"

const previewWorking = "# Настройки сборки\nВерсия: 1.4\nАвтор: команда проекта\nПапка вывода: build\nТесты: запускать\nЖурнал: подробный\nСжатие: включено\n"

func TestPreviewIndexEditorDialog(t *testing.T) {
	dir := os.Getenv("GOGIT_PREVIEW_DIR")
	if dir == "" {
		t.Skip("GOGIT_PREVIEW_DIR not set")
	}
	for _, variant := range []struct {
		name  string
		theme *widget.Theme
	}{
		{"light", widget.Win11LightTheme()},
		{"dark", widget.Win11DarkTheme()},
	} {
		if _, err := i18n.Install(""); err != nil {
			t.Fatal(err)
		}
		i18n.Apply("ru")

		eng := engine.New(1280, 800, 30)
		eng.SetTheme(variant.theme)
		root := widget.NewPanel(variant.theme.WindowBG)
		root.SetBounds(image.Rect(0, 0, 1280, 800))
		eng.SetRoot(root)

		view, err := NewView()
		if err != nil {
			t.Fatal(err)
		}
		view.Show(File{
			Path:      "настройки.txt",
			HeadLabel: "main",
			Head:      []byte(previewHead),
			Index:     []byte(previewIndex),
			Working:   []byte(previewWorking),
		})
		view.Merge().NextConflict()
		eng.ShowModal(view.Dialog())
		view.Restyle(variant.theme)

		eng.SaveFrames(dir + "/index-editor-" + variant.name)
		eng.Start()
		time.Sleep(900 * time.Millisecond)
		eng.Stop()
	}
}
