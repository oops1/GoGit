package compare

import (
	"image"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/oops1/headless-gui/v3/engine"
	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/i18n"
)

const previewLeft = `package config

import "os"

type Config struct {
	Path    string
	Verbose bool
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parse(data)
}
`

const previewRight = `package config

import (
	"fmt"
	"os"
)

type Config struct {
	Path     string
	Verbose  bool
	Language string
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	return parse(data)
}
`

func TestPreviewCompareWindow(t *testing.T) {
	dir := os.Getenv("GOGIT_PREVIEW_DIR")
	if dir == "" {
		t.Skip("GOGIT_PREVIEW_DIR not set")
	}
	files := t.TempDir()
	left := filepath.Join(files, "main", "config.go")
	right := filepath.Join(files, "feature", "config.go")
	for path, body := range map[string]string{left: previewLeft, right: previewRight} {
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, variant := range []struct {
		name  string
		lang  string
		theme *widget.Theme
	}{
		{"light", "ru", widget.Win11LightTheme()},
		{"dark", "ru", widget.Win11DarkTheme()},
		{"en", "en", widget.Win11LightTheme()},
	} {
		if _, err := i18n.Install(""); err != nil {
			t.Fatal(err)
		}
		i18n.Apply(variant.lang)
		eng := engine.New(1180, 760, 30)
		eng.SetTheme(variant.theme)
		root := widget.NewPanel(variant.theme.WindowBG)
		root.SetBounds(image.Rect(0, 0, 1180, 760))
		eng.SetRoot(root)

		view, err := NewView()
		if err != nil {
			t.Fatal(err)
		}
		if err := view.Load(widget.DiffLeft, left); err != nil {
			t.Fatal(err)
		}
		if err := view.Load(widget.DiffRight, right); err != nil {
			t.Fatal(err)
		}
		eng.ShowModal(view.Dialog())
		view.Restyle(variant.theme)

		eng.SaveFrames(filepath.Join(dir, "compare-"+variant.name))
		eng.Start()
		time.Sleep(700 * time.Millisecond)
		eng.Stop()
	}
}
