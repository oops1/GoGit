//go:build !windows

package systheme

import "testing"

func TestDetectUsesEnvironment(t *testing.T) {
	t.Setenv("GTK_THEME", "Adwaita:dark")
	if Detect() != Dark {
		t.Fatal("GTK_THEME dark must win")
	}
	t.Setenv("GTK_THEME", "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if Detect() != Unknown {
		t.Fatal("empty config dir must be unknown")
	}
}
