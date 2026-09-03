package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/config"
)

func writeFile(dir, name, content string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600)
}

func newTestAppWithPaths(t *testing.T) (*App, config.Paths) {
	t.Helper()
	widget.ClearStrings()
	t.Cleanup(widget.ClearStrings)
	paths := config.Paths{Dir: t.TempDir()}
	a, err := New(config.Default(), paths, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)
	return a, paths
}
