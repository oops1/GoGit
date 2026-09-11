package app

import (
	"testing"

	"github.com/oops1/gogit/internal/config"
	"github.com/oops1/gogit/internal/ui/panetitle"
)

func TestPaneTitlesStayQuietInBothThemes(t *testing.T) {
	a := newTestAppWithConfig(t, config.Default())
	for _, name := range []string{config.ThemeDark, config.ThemeLight} {
		a.SetTheme(name)
		theme := a.theme()
		panes := a.Dock().Panes()
		if len(panes) == 0 {
			t.Fatal("dock has no panes")
		}
		for _, pane := range panes {
			if pane.TitleBG != theme.PanelBG {
				t.Fatalf("theme %s pane %q title = %v, want the panel colour", name, pane.ID, pane.TitleBG)
			}
			if pane.TitleActiveBG == theme.Accent {
				t.Fatalf("theme %s pane %q active title is painted in the plain accent", name, pane.ID)
			}
			if pane.TitleActiveBG != panetitle.Tint(theme.PanelBG, theme.Accent) {
				t.Fatalf("theme %s pane %q active title = %v, want the tinted panel colour", name, pane.ID, pane.TitleActiveBG)
			}
		}
	}
}
