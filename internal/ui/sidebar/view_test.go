package sidebar

import (
	"image"
	"image/color"
	"testing"

	"github.com/oops1/headless-gui/v3/engine"
	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/i18n"
)

func newTestView(t *testing.T) *View {
	t.Helper()
	if _, err := i18n.Install(""); err != nil {
		t.Fatal(err)
	}
	i18n.Apply("en")
	return NewView()
}

func labelled(text string) *widget.Label {
	return widget.NewLabel(text, widget.CurrentTheme().LabelText)
}

func testParts() Parts {
	return Parts{
		Repositories: labelled("repositories"),
		Branches:     labelled("branches"),
		Files:        labelled("files"),
		Diff:         labelled("diff"),
		Journal:      labelled("journal"),
		Details:      labelled("details"),
	}
}

func TestANewViewStartsOnTheWorkingCopyAndNamesEverySection(t *testing.T) {
	v := newTestView(t)

	items := v.nav.Items()
	if v.Section() != SectionWorkingCopy || v.nav.Selected() != int(SectionWorkingCopy) {
		t.Fatalf("section = %d, selected = %d", v.Section(), v.nav.Selected())
	}
	if len(items) != len(sections) {
		t.Fatalf("items = %d, want %d", len(items), len(sections))
	}
	for i, s := range sections {
		if items[i].Text != i18n.T(s.key) || items[i].Icon == nil {
			t.Fatalf("item %d = %+v, want %q with an icon", i, items[i], i18n.T(s.key))
		}
	}
}

func TestEachSectionShowsItsOwnContent(t *testing.T) {
	v := newTestView(t)
	parts := testParts()
	var chosen []Section
	v.OnSelect = func(s Section) { chosen = append(chosen, s) }
	v.Mount(parts)

	if v.shown != v.work {
		t.Fatal("the working copy does not show the work area")
	}
	for _, tt := range []struct {
		section Section
		want    widget.Widget
	}{
		{SectionRepositories, parts.Repositories},
		{SectionIndex, v.work},
		{SectionBranches, parts.Branches},
		{SectionTags, parts.Branches},
		{SectionRemotes, parts.Branches},
		{SectionStash, v.notYet},
		{SectionSubmodules, v.notYet},
		{SectionWorkingCopy, v.work},
	} {
		v.Select(tt.section)
		if v.shown != tt.want || v.nav.Selected() != int(tt.section) || len(v.host.Children()) != 1 {
			t.Fatalf("section %d shows %v with %d children", tt.section, v.shown, len(v.host.Children()))
		}
	}
	if len(chosen) != 8 || chosen[0] != SectionRepositories {
		t.Fatalf("chosen = %v", chosen)
	}

	v.Select(Section(len(sections)))
	v.Select(-1)

	if v.Section() != SectionWorkingCopy || len(chosen) != 8 {
		t.Fatalf("an unknown section changed the view: %d, %v", v.Section(), chosen)
	}
}

func TestClickingASectionInTheNavigationSelectsIt(t *testing.T) {
	v := newTestView(t)
	parts := testParts()
	v.Mount(parts)

	v.nav.OnSelect(int(SectionTags))

	if v.Section() != SectionTags || v.shown != parts.Branches {
		t.Fatalf("section = %d, shown = %v", v.Section(), v.shown)
	}
}

func TestUnmountingHandsThePartsBackAndEmptiesTheView(t *testing.T) {
	v := newTestView(t)
	parts := testParts()
	v.Mount(parts)

	got := v.Unmount()

	if got != parts {
		t.Fatalf("parts = %+v, want the mounted ones", got)
	}
	if len(v.host.Children()) != 0 || v.work != nil || v.splits != nil {
		t.Fatalf("host children = %d, work = %v, splits = %v", len(v.host.Children()), v.work, v.splits)
	}
}

func TestASectionWithoutItsPartShowsTheNote(t *testing.T) {
	v := newTestView(t)
	parts := testParts()
	parts.Repositories = nil
	v.Mount(parts)

	v.Select(SectionRepositories)

	if v.shown != v.notYet || v.notYet.Text() != i18n.T("Sidebar.NotYet") {
		t.Fatalf("shown = %v, note = %q", v.shown, v.notYet.Text())
	}
}

func TestCountsSitBesideTheirSections(t *testing.T) {
	v := newTestView(t)
	v.SetCount(SectionWorkingCopy, 5)
	v.SetCount(SectionStash, NoCount)

	if text, ok := v.CountText(SectionWorkingCopy); !ok || text != "5" {
		t.Fatalf("working copy count = %q, %v", text, ok)
	}
	if text, ok := v.CountText(SectionStash); !ok || text != "—" {
		t.Fatalf("stash count = %q, %v", text, ok)
	}
	if _, ok := v.CountText(SectionIndex); ok {
		t.Fatal("a section without a count shows one")
	}

	eng := engine.New(320, 420, 30)
	t.Cleanup(eng.Stop)
	v.Root().SetBounds(image.Rect(0, 0, 320, 420))
	eng.SetRoot(v.Root())
	_ = eng.RenderOnce()
	v.nav.SetCollapsed(true)
	_ = eng.RenderOnce()
}

func TestRestyleReachesThePartsThatAreNotShown(t *testing.T) {
	v := newTestView(t)
	parts := testParts()
	stale := color.RGBA{R: 1, G: 2, B: 3, A: 255}
	for _, part := range []widget.Widget{parts.Repositories, parts.Branches, parts.Files, parts.Details} {
		part.(*widget.Label).TextColor = stale
	}
	v.Mount(parts)
	v.Select(SectionRepositories)
	dark := widget.Win11DarkTheme()

	v.Restyle(dark)

	for name, part := range map[string]widget.Widget{"branches": parts.Branches, "files": parts.Files, "details": parts.Details} {
		if got := part.(*widget.Label).TextColor; got != dark.LabelText {
			t.Fatalf("hidden %s colour = %v, want %v", name, got, dark.LabelText)
		}
	}
	if got := parts.Repositories.(*widget.Label).TextColor; got != stale {
		t.Fatalf("the shown part was repainted by the view: %v", got)
	}
}

func TestTheViewFollowsTheThemeAndTheLanguage(t *testing.T) {
	v := newTestView(t)
	dark := widget.Win11DarkTheme()

	v.Restyle(dark)
	i18n.Apply("ru")
	t.Cleanup(func() { i18n.Apply("en") })
	v.Retitle()

	v.nav.mu.Lock()
	countColor := v.nav.countColor
	v.nav.mu.Unlock()
	if countColor != dark.SecondaryText || v.notYet.TextColor != dark.SecondaryText {
		t.Fatalf("count colour = %v, note colour = %v", countColor, v.notYet.TextColor)
	}
	if v.nav.Items()[int(SectionBranches)].Text != i18n.T("Sidebar.Section.Branches") {
		t.Fatalf("branches = %q after switching the language", v.nav.Items()[int(SectionBranches)].Text)
	}
}
