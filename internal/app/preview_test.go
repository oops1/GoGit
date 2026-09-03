package app

import (
	"os"
	"path/filepath"
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

func TestPreviewOpenedRepository(t *testing.T) {
	dir := os.Getenv("GOGIT_PREVIEW_DIR")
	if dir == "" {
		t.Skip("GOGIT_PREVIEW_DIR not set")
	}
	target := filepath.Join(t.TempDir(), "demo")
	initTestRepoWithBranch(t, target, "main")
	addBranchAndTag(t, target, "feature/preview", "v1.0")

	for _, theme := range []string{config.ThemeDark, config.ThemeLight} {
		cfg := config.Default()
		cfg.Repositories = []config.Repository{{ID: "r1", Name: "demo", Path: target}}
		a := newTestAppWithConfig(t, cfg)
		a.SetTheme(theme)
		a.ActivateRepository("r1")
		a.Engine().SaveFrames(dir + "/opened-" + theme)
		a.Engine().Start()
		time.Sleep(700 * time.Millisecond)
		a.Engine().Stop()
	}
}
