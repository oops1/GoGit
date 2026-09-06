package settings

import (
	"testing"

	"github.com/oops1/gogit/internal/i18n"
)

func TestNavigationStartsExpandedWithTheChosenSection(t *testing.T) {
	v := newTestView(t, []string{"en"}, Model{})

	if v.nav.IsCollapsed() {
		t.Fatal("navigation must start expanded")
	}
	if got := v.nav.Caption(0); got != i18n.T("Dialog.Settings.Nav.General") {
		t.Fatalf("nav caption = %q, want the section name", got)
	}
	if got := v.nav.Selected(); got != 0 {
		t.Fatalf("selected item = %d, want the first one", got)
	}
}

func TestTheTitleBarButtonCollapsesTheNavigationToIcons(t *testing.T) {
	v := newTestView(t, []string{"en"}, Model{})
	expanded := v.nav.Width()

	v.Dialog().SetNavCollapsed(true)

	if !v.nav.IsCollapsed() {
		t.Fatal("the title bar button must collapse the navigation panel")
	}
	if v.nav.Width() >= expanded {
		t.Fatalf("collapsed width = %d, want less than %d", v.nav.Width(), expanded)
	}

	v.Dialog().SetNavCollapsed(false)

	if v.nav.IsCollapsed() {
		t.Fatal("the button must bring the navigation back")
	}
	if got := v.nav.Width(); got != expanded {
		t.Fatalf("restored width = %d, want %d", got, expanded)
	}
}

func TestChoosingAnItemInTheNavigationSwitchesTheSection(t *testing.T) {
	v := newTestView(t, []string{"en"}, Model{})

	v.nav.OnSelect(1)

	if got := v.Section(); got != "git" {
		t.Fatalf("section = %q, want %q", got, "git")
	}
	v.nav.OnSelect(-1)
	v.nav.OnSelect(len(sectionOrder))
	if got := v.Section(); got != "git" {
		t.Fatalf("section = %q, want it unchanged by an index outside the list", got)
	}
}

func TestSearchCountsAppearNextToTheSectionName(t *testing.T) {
	v := newTestView(t, []string{"en"}, Model{})

	v.applySearch("journal")

	if got := v.nav.Caption(0); got == i18n.T("Dialog.Settings.Nav.General") {
		t.Fatal("the matching section must show how many settings matched")
	}
}

func TestCaptionIgnoresIndexesOutsideTheNavigation(t *testing.T) {
	v := newTestView(t, []string{"en"}, Model{})

	v.nav.SetCaption(-1, "x")
	v.nav.SetCaption(len(sectionOrder), "x")

	if got := v.nav.Caption(-1); got != "" {
		t.Fatalf("Caption(-1) = %q, want it empty", got)
	}
	if got := v.nav.Caption(len(sectionOrder)); got != "" {
		t.Fatalf("Caption past the end = %q, want it empty", got)
	}
	if got := v.nav.Caption(0); got != i18n.T("Dialog.Settings.Nav.General") {
		t.Fatalf("nav caption = %q, want it untouched", got)
	}
}

func TestSelectingAnUnknownSectionLeavesTheNavigationAlone(t *testing.T) {
	v := newTestView(t, []string{"en"}, Model{})

	v.nav.SetSelectedSection("nothing")

	if got := v.nav.Selected(); got != 0 {
		t.Fatalf("selected item = %d, want it unchanged", got)
	}
}
