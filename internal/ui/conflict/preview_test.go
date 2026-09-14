package conflict

import (
	"image"
	"os"
	"testing"
	"time"

	"github.com/oops1/headless-gui/v3/engine"
	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/gitcore/merge"
	"github.com/oops1/gogit/internal/i18n"
)

const previewBase = "# Настройки сборки\nВерсия: 1.3\nАвтор: команда проекта\nЯзык интерфейса: русский\nПапка вывода: dist\nТесты: запускать\nЖурнал: подробный\nПроверка ссылок: включена\nСжатие: выключено\n"

const previewOurs = "# Настройки сборки\nВерсия: 1.4\nАвтор: команда проекта\nЯзык интерфейса: русский\nПапка вывода: dist\nТесты: запускать\nЖурнал: подробный\nПроверка ссылок: включена\nСжатие: включено\n"

const previewTheirs = "# Настройки сборки\nВерсия: 1.5\nАвтор: команда проекта\nЯзык интерфейса: русский\nПапка вывода: build\nТесты: запускать\nЖурнал: подробный\nПроверка ссылок: включена\nСжатие: максимальное\n"

func TestPreviewConflictDialog(t *testing.T) {
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
			Path:         "настройки.txt",
			OursLabel:    "main",
			BaseLabel:    i18n.T("Dialog.Conflict.Side.Base"),
			TheirsLabel:  "feature",
			Style:        merge.StyleMerge,
			FinalNewline: true,
			Blocks:       merge.Chunks([]byte(previewBase), []byte(previewOurs), []byte(previewTheirs), merge.Options{}),
		})
		view.Merge().ResolveCurrent(widget.MergeTakeTheirs)
		view.Merge().NextConflict()
		eng.ShowModal(view.Dialog())
		view.Restyle(variant.theme)

		eng.SaveFrames(dir + "/conflict-" + variant.name)
		eng.Start()
		time.Sleep(900 * time.Millisecond)
		eng.Stop()
	}
}
