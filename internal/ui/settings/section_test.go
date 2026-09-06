package settings

import (
	"errors"
	"image/color"
	"testing"

	"github.com/oops1/headless-gui/v3/widget"
	"github.com/oops1/headless-gui/v3/widget/svg"

	"github.com/oops1/gogit/internal/config"
	"github.com/oops1/gogit/internal/i18n"
)

func TestModifiedIsFalseImmediatelyAfterOpen(t *testing.T) {
	v := newTestView(t, []string{"en", "ru"}, Model{Language: "en", LogMaxCount: 500})
	if v.Modified() {
		t.Fatal("Modified() must be false right after opening")
	}
	if v.okBtn.IsEnabled() {
		t.Fatal("save must start disabled")
	}
}

func TestSectionDefaultsToGeneralWithMatchingTitle(t *testing.T) {
	v := newTestView(t, []string{"en"}, Model{})
	if v.Section() != "general" {
		t.Fatalf("Section() = %q, want general", v.Section())
	}
	if !v.sectionGeneral.IsVisible() {
		t.Fatal("sectionGeneral must be visible by default")
	}
	for _, hidden := range []*widget.Grid{v.sectionGit, v.sectionCredentials, v.sectionSSH} {
		if hidden.IsVisible() {
			t.Fatal("non-active sections must start hidden")
		}
	}
	want := i18n.T("Dialog.Settings.General.Title")
	if got := v.sectionTitle.Text(); got != want {
		t.Fatalf("sectionTitle = %q, want %q", got, want)
	}
}

func TestSetSectionSwitchesVisibilityTitleAndNavSelection(t *testing.T) {
	v := newTestView(t, []string{"en"}, Model{})
	v.SetSection("credentials")

	if v.Section() != "credentials" {
		t.Fatalf("Section() = %q, want credentials", v.Section())
	}
	if !v.sectionCredentials.IsVisible() {
		t.Fatal("sectionCredentials must become visible")
	}
	if v.sectionGeneral.IsVisible() {
		t.Fatal("sectionGeneral must become hidden")
	}
	want := i18n.T("Dialog.Settings.Credentials.Title")
	if got := v.sectionTitle.Text(); got != want {
		t.Fatalf("sectionTitle = %q, want %q", got, want)
	}
	if got := v.nav.Selected(); got != 2 {
		t.Fatalf("nav selection = %d, want the credentials item", got)
	}
}

func TestSetSectionIsANoOpWhenAlreadyActive(t *testing.T) {
	v := newTestView(t, []string{"en"}, Model{})
	v.SetSection("general")
	if v.Section() != "general" {
		t.Fatalf("Section() = %q, want general", v.Section())
	}
	if !v.sectionGeneral.IsVisible() {
		t.Fatal("sectionGeneral must stay visible")
	}
}

func TestSetSectionFallsBackToDefaultForUnknownID(t *testing.T) {
	v := newTestView(t, []string{"en"}, Model{})
	v.SetSection("git")
	v.SetSection("does-not-exist")
	if v.Section() != defaultSectionID {
		t.Fatalf("Section() = %q, want %q", v.Section(), defaultSectionID)
	}
}

func TestSectionStatePersistsAcrossSwitches(t *testing.T) {
	v := newTestView(t, []string{"en"}, Model{})
	v.SetSection("git")
	v.pullStrategy.SetText("rebase")
	v.SetSection("general")
	v.SetSection("git")
	if got := v.pullStrategy.GetText(); got != "rebase" {
		t.Fatalf("pullStrategy = %q, want rebase after switching back", got)
	}
}

func TestClickingNavItemSwitchesSection(t *testing.T) {
	v := newTestView(t, []string{"en"}, Model{})
	item := v.nav.ItemRect(1)
	x, y := item.Min.X+8, item.Min.Y+item.Dy()/2
	ev := widget.MouseEvent{Button: widget.MouseLeft, X: x, Y: y, Pressed: true}
	v.nav.OnMouseButton(ev)
	ev.Pressed = false
	v.nav.OnMouseButton(ev)

	if v.Section() != "git" {
		t.Fatalf("Section() = %q, want git", v.Section())
	}
}

func TestDialogIsResizableWithDeclaredMinimumSize(t *testing.T) {
	v := newTestView(t, []string{"en"}, Model{})
	if !v.dlg.IsResizable() {
		t.Fatal("dialog must be resizable")
	}
	w, h := v.dlg.MinSize()
	if w != dialogMinWidth || h != dialogMinHeight {
		t.Fatalf("MinSize() = %d,%d want %d,%d", w, h, dialogMinWidth, dialogMinHeight)
	}
}

func TestDialogContentStretchesOnResize(t *testing.T) {
	v := newTestView(t, []string{"en"}, Model{})
	before := v.root.Bounds()
	size := v.dlg.Bounds()

	v.dlg.Resize(size.Dx()+200, size.Dy()+150)

	after := v.root.Bounds()
	if after.Dx() <= before.Dx() || after.Dy() <= before.Dy() {
		t.Fatalf("content did not grow: before=%v after=%v", before, after)
	}
	if after != v.dlg.ContentBounds() {
		t.Fatalf("content bounds = %v, want %v", after, v.dlg.ContentBounds())
	}
}

func TestSectionTitleKeyReturnsEmptyForUnknownID(t *testing.T) {
	if got := sectionTitleKey("bogus"); got != "" {
		t.Fatalf("sectionTitleKey(bogus) = %q, want empty", got)
	}
}

func TestIsKnownSectionRejectsUnknownID(t *testing.T) {
	if isKnownSection("bogus") {
		t.Fatal("isKnownSection(bogus) must be false")
	}
	for _, s := range sectionOrder {
		if !isKnownSection(s.id) {
			t.Fatalf("isKnownSection(%q) must be true", s.id)
		}
	}
}

func TestBuildSearchIconReturnsNilWhenParseFails(t *testing.T) {
	prev := parseSearchIconSVG
	parseSearchIconSVG = func([]byte) (*svg.Document, error) { return nil, errors.New("boom") }
	defer func() { parseSearchIconSVG = prev }()

	if buildSearchIcon(color.RGBA{}) != nil {
		t.Fatal("expected nil icon when the SVG fails to parse")
	}
}

func TestBuildSearchIconRendersASquareImage(t *testing.T) {
	img := buildSearchIcon(color.RGBA{R: 128, G: 128, B: 128, A: 255})
	if img == nil {
		t.Fatal("expected a non-nil icon")
	}
	b := img.Bounds()
	if b.Dx() != searchIconSize || b.Dy() != searchIconSize {
		t.Fatalf("icon bounds = %v, want %dx%d", b, searchIconSize, searchIconSize)
	}
}

func TestModifiedTrackingCoversEveryWiredWidget(t *testing.T) {
	cases := []struct {
		name   string
		change func(v *View)
	}{
		{"language", func(v *View) { v.language.SetSelected(1); v.language.OnChange(1, "ru") }},
		{"theme", func(v *View) { v.theme.SetSelected(themeIndex(config.ThemeDark)); v.theme.OnChange(0, "") }},
		{"showToolbar", func(v *View) { clickCheckBox(v.showToolbar) }},
		{"toolbarCaptions", func(v *View) { clickCheckBox(v.toolbarCaptions) }},
		{"showStatusBar", func(v *View) { clickCheckBox(v.showStatusBar) }},
		{"journalFullAuthorName", func(v *View) { clickCheckBox(v.journalFullAuthorName) }},
		{"logMaxCount", func(v *View) { v.logMaxCount.SetValue(v.logMaxCount.Value() + 50) }},
		{"autoFetch", func(v *View) { clickCheckBox(v.autoFetch) }},
		{"fetchInterval", func(v *View) { v.fetchInterval.SetValue(v.fetchInterval.Value() + 10) }},
		{"workTreeDepth", func(v *View) { v.workTreeDepth.SetValue(v.workTreeDepth.Value() + 1) }},
		{"pullStrategy", func(v *View) { v.pullStrategy.SetText("rebase"); v.pullStrategy.OnChange("rebase") }},
		{"defaultRemote", func(v *View) { v.defaultRemote.SetText("upstream"); v.defaultRemote.OnChange("upstream") }},
		{"pruneOnFetch", func(v *View) { clickCheckBox(v.pruneOnFetch) }},
		{"shallowDepth", func(v *View) { v.shallowDepth.SetValue(v.shallowDepth.Value() + 1) }},
		{"credentialSource", func(v *View) { v.credentialSource.SetSelected(1); v.credentialSource.OnChange(1, "") }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v := newTestView(t, []string{"en", "ru"}, Model{})
			if v.Modified() || v.okBtn.IsEnabled() {
				t.Fatal("must start unmodified with save disabled")
			}
			c.change(v)
			if !v.Modified() {
				t.Fatalf("%s: expected Modified() = true after the change", c.name)
			}
			if !v.okBtn.IsEnabled() {
				t.Fatalf("%s: expected save to become enabled", c.name)
			}
		})
	}
}
