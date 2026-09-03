package app

import (
	"os"
	"testing"
	"time"

	"github.com/oops1/gogit/internal/config"
)

func TestPreviewFrames(t *testing.T) {
	dir := os.Getenv("GOGIT_PREVIEW_DIR")
	if dir == "" {
		t.Skip("GOGIT_PREVIEW_DIR not set")
	}
	for _, theme := range []string{config.ThemeDark, config.ThemeLight} {
		a := newTestApp(t)
		a.SetTheme(theme)
		a.SetActiveRepository("demo", false)
		a.Engine().SaveFrames(dir + "/" + theme)
		a.Engine().Start()
		time.Sleep(700 * time.Millisecond)
		a.Engine().Stop()
	}
}
