package app

import (
	"testing"

	"github.com/oops1/gogit/internal/config"
	"github.com/oops1/gogit/internal/ui/panetitle"
)

func TestPaneTitlesUseInvertedBackgroundInBothThemes(t *testing.T) {
	a := newTestAppWithConfig(t, config.Default())
	for _, theme := range []string{config.ThemeDark, config.ThemeLight} {
		a.SetTheme(theme)
		panes := a.Dock().Panes()
		if len(panes) == 0 {
			t.Fatal("dock has no panes")
		}
		for _, pane := range panes {
			if got, want := pane.TitleText, panetitle.XOR(pane.TitleBG); got != want {
				t.Fatalf("theme %s pane %q title = %v, want %v", theme, pane.ID, got, want)
			}
			if got, want := pane.TitleTextActive, panetitle.XOR(pane.TitleActiveBG); got != want {
				t.Fatalf("theme %s pane %q active title = %v, want %v", theme, pane.ID, got, want)
			}
		}
	}
}
