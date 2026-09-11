//go:build !windows

package systheme

import (
	"image/color"
	"os"
	"path/filepath"
	"testing"
)

func TestDetectAccentReadsTheDesktopFiles(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	if DetectAccent().Known {
		t.Fatal("a desktop without a kdeglobals reports no accent")
	}

	body := []byte("[General]\nAccentColor=146,54,180\n")
	if err := os.WriteFile(filepath.Join(dir, "kdeglobals"), body, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := DetectAccent(); got.Base != (color.RGBA{R: 146, G: 54, B: 180, A: 0xFF}) {
		t.Fatalf("accent = %+v, want the one in kdeglobals", got)
	}
}
