package app

import (
	"path/filepath"
	"testing"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/config"
	"github.com/oops1/gogit/internal/ui/icons"
)

func TestToolbarButtonsHaveIconsAfterConstruction(t *testing.T) {
	a := newTestApp(t)
	for _, name := range toolbarButtons {
		btn := a.Widget(name).(*widget.Button)
		if btn.Icon == nil {
			t.Fatalf("button %q has no icon", name)
		}
		if btn.IconSize != toolbarIconSize {
			t.Fatalf("button %q icon size = %d, want %d", name, btn.IconSize, toolbarIconSize)
		}
		if btn.IconPos != widget.IconTop {
			t.Fatalf("button %q icon position = %v, want IconTop", name, btn.IconPos)
		}
	}
}

func TestToolbarButtonsKeepTheirOwnColoursInBothThemes(t *testing.T) {
	a := newTestApp(t)
	a.SetTheme(config.ThemeDark)
	dark := a.Widget("btnPull").(*widget.Button).Icon

	a.SetTheme(config.ThemeLight)
	light := a.Widget("btnPull").(*widget.Button).Icon

	if dark == nil || light == nil {
		t.Fatal("expected non-nil icons in both themes")
	}
	if dark != light {
		t.Fatal("a toolbar icon carries its own colours and must not be repainted per theme")
	}
	if dark != icons.ToolbarPlain("pull", toolbarIconSize) {
		t.Fatal("the toolbar must draw the icon as it is drawn in the asset")
	}
}

func TestToolbarDropsCaptionsWhenTheSettingIsOff(t *testing.T) {
	a := newTestApp(t)
	a.cfg.UI.ToolbarCaptions = false
	a.applyToolbarIcons(themeFor(a.EffectiveTheme()))

	btn := a.Widget("btnPull").(*widget.Button)
	if btn.IconPos != widget.IconOnly {
		t.Fatalf("icon position = %v, want IconOnly", btn.IconPos)
	}
	if got := btn.Bounds().Dx(); got != toolbarCompactWidth {
		t.Fatalf("button width = %d, want %d", got, toolbarCompactWidth)
	}
}
func TestActiveRepositoryIconDiffersFromAnInactiveRepository(t *testing.T) {
	dir := t.TempDir()
	activePath := filepath.Join(dir, "active")
	inactivePath := filepath.Join(dir, "inactive")
	initTestRepo(t, activePath)
	initTestRepo(t, inactivePath)
	cfg := config.Default()
	cfg.Repositories = []config.Repository{
		{ID: "r1", Name: "Active", Path: activePath},
		{ID: "r2", Name: "Inactive", Path: inactivePath},
	}
	a := newTestAppWithConfig(t, cfg)
	a.ActivateRepository("r1")

	active, ok := a.reposView.Item("r1")
	if !ok {
		t.Fatal("active item missing")
	}
	inactive, ok := a.reposView.Item("r2")
	if !ok {
		t.Fatal("inactive item missing")
	}
	if active.Icon == inactive.Icon {
		t.Fatal("the active repository must use a different icon than an inactive one")
	}
	want := icons.TreeTinted("repository", 16, themeFor(a.EffectiveTheme()).Accent)
	if active.Icon != want {
		t.Fatal("the active repository must use the accent-tinted repository icon")
	}
	wantInactive := icons.TreeTinted("repository", 16, themeFor(a.EffectiveTheme()).Disabled)
	if inactive.Icon != wantInactive {
		t.Fatal("a repository that is not open must use the muted repository icon")
	}
}

func TestActiveRepositoryWithWorkingCopyChangesShowsTheModifiedIcon(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	buildWorkingRepoFixture(t, target)
	a := activatedWorkingApp(t, target)
	waitForWorkingRows(t, a, 5)
	waitForPostQueueDrain(t, a)

	item, ok := a.reposView.Item("r1")
	if !ok {
		t.Fatal("item missing")
	}
	want := icons.TreeTinted("repository_modified", 16, themeFor(a.EffectiveTheme()).Accent)
	if item.Icon != want {
		t.Fatal("a repository with working copy changes must show the modified icon")
	}
}

func TestActiveRepositoryWithACleanWorkingCopyKeepsThePlainIcon(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	initTestRepoWithBranch(t, target, "main")
	a := activatedWorkingApp(t, target)
	waitForWorkingRows(t, a, 0)
	waitForPostQueueDrain(t, a)

	item, ok := a.reposView.Item("r1")
	if !ok {
		t.Fatal("item missing")
	}
	want := icons.TreeTinted("repository", 16, themeFor(a.EffectiveTheme()).Accent)
	if item.Icon != want {
		t.Fatal("a repository with a clean working copy must show the plain icon")
	}
}

func TestActiveRepositoryWithOnlyUnmodifiedTrackedFilesKeepsThePlainIcon(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	buildCleanTrackedRepoFixture(t, target)
	a := activatedWorkingApp(t, target)
	waitForWorkingRows(t, a, 1)
	waitForPostQueueDrain(t, a)

	item, ok := a.reposView.Item("r1")
	if !ok {
		t.Fatal("item missing")
	}
	want := icons.TreeTinted("repository", 16, themeFor(a.EffectiveTheme()).Accent)
	if item.Icon != want {
		t.Fatal("an unmodified tracked file reported by IncludeUnmodified must not trigger the modified icon")
	}
}

func TestActiveRepositoryWithAMissingPathShowsTheMissingIcon(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.Repositories = []config.Repository{{ID: "r1", Name: "Gone", Path: filepath.Join(dir, "gone")}}
	cfg.ActiveRepository = "r1"
	a := newTestAppWithConfig(t, cfg)

	item, ok := a.reposView.Item("r1")
	if !ok {
		t.Fatal("item missing")
	}
	want := icons.TreeTinted("repository_missing", 16, themeFor(a.EffectiveTheme()).Accent)
	if item.Icon != want {
		t.Fatal("an active repository whose path is gone must show the missing icon")
	}
}

func TestRepoTreeStateIsEmptyWithoutAnActiveRepository(t *testing.T) {
	a := newTestApp(t)
	if state := a.repoTreeState(); len(state) != 0 {
		t.Fatalf("state = %+v, want empty", state)
	}
}
