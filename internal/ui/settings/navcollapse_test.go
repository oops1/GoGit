package settings

import (
	"testing"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/i18n"
)

func TestNavigationStartsExpanded(t *testing.T) {
	v := newTestView(t, []string{"en"}, Model{})

	if v.navCollapsed {
		t.Fatal("navigation must start expanded")
	}
	if v.root.ColDefs[0].Value != navWidth {
		t.Fatalf("nav column = %v, want %d", v.root.ColDefs[0].Value, navWidth)
	}
	if got := v.nav.Caption(0); got != i18n.T("Dialog.Settings.Nav.General") {
		t.Fatalf("nav caption = %q, want the section name", got)
	}
	if got := v.navToggle.Text; got != i18n.T("Dialog.Settings.Nav.Collapse") {
		t.Fatalf("toggle glyph = %q, want the collapse glyph", got)
	}
}

func TestTogglingNavigationShrinksItToIconsAndBack(t *testing.T) {
	v := newTestView(t, []string{"en"}, Model{})

	v.toggleNav()

	if v.root.ColDefs[0].Value != navCollapsedWidth {
		t.Fatalf("collapsed nav column = %v, want %d", v.root.ColDefs[0].Value, navCollapsedWidth)
	}
	if got := v.nav.Caption(0); got != "" {
		t.Fatalf("collapsed nav caption = %q, want it empty", got)
	}
	if v.navTitle.IsVisible() {
		t.Fatal("the window title must hide while the navigation is collapsed")
	}
	if got := v.navToggle.Text; got != i18n.T("Dialog.Settings.Nav.Expand") {
		t.Fatalf("toggle glyph = %q, want the expand glyph", got)
	}

	v.toggleNav()

	if v.root.ColDefs[0].Value != navWidth {
		t.Fatalf("restored nav column = %v, want %d", v.root.ColDefs[0].Value, navWidth)
	}
	if got := v.nav.Caption(0); got != i18n.T("Dialog.Settings.Nav.General") {
		t.Fatalf("restored nav caption = %q, want the section name", got)
	}
	if !v.navTitle.IsVisible() {
		t.Fatal("the window title must come back with the navigation")
	}
}

func TestSearchKeepsTheNavigationCollapsed(t *testing.T) {
	v := newTestView(t, []string{"en"}, Model{})
	v.toggleNav()

	v.applySearch("journal")

	if got := v.nav.Caption(0); got != "" {
		t.Fatalf("nav caption while searching = %q, want it empty", got)
	}
}

func TestSetCaptionIgnoresIndexesOutsideTheNavigation(t *testing.T) {
	v := newTestView(t, []string{"en"}, Model{})

	v.nav.SetCollapsed(false, []string{"only one"})
	v.nav.SetCaption(-1, "x")
	v.nav.SetCaption(len(sectionOrder), "x")

	if got := v.nav.Caption(0); got != "only one" {
		t.Fatalf("nav caption = %q, want %q", got, "only one")
	}
	if got := v.nav.Caption(-1); got != "" {
		t.Fatalf("Caption(-1) = %q, want it empty", got)
	}
	if got := v.nav.Caption(len(sectionOrder)); got != "" {
		t.Fatalf("Caption past the end = %q, want it empty", got)
	}
}

func TestNavigationBackdropTakesThePanelColourFromTheTheme(t *testing.T) {
	v := newTestView(t, []string{"en"}, Model{})
	theme := widget.Win11DarkTheme()

	v.navBackground.ApplyTheme(theme)

	if v.navBackground.Background != theme.PanelBG {
		t.Fatalf("backdrop background = %v, want %v", v.navBackground.Background, theme.PanelBG)
	}
}
