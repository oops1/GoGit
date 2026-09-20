package app

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/config"
	gitconfig "github.com/oops1/gogit/internal/gitcore/config"
	"github.com/oops1/gogit/internal/gitcore/ops"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/branches"
)

func branchesPaneMenu(t *testing.T, a *App) []widget.MenuItem {
	t.Helper()
	return readOnDispatcher(t, a, a.branchesPaneMenuItems)
}

func clickViewMenuItem(t *testing.T, a *App, items []widget.MenuItem, key string) {
	t.Helper()
	runOnDispatcher(t, a, func() { clickMenuItem(t, items, key) })
}

func TestTheBranchesPaneTitleCarriesTheViewMenuButton(t *testing.T) {
	a := newTestApp(t)
	buttons := readOnDispatcher(t, a, func() []widget.DockPaneButton {
		return a.Dock().FindPane(paneBranches).TitleButtons()
	})
	if len(buttons) != 1 {
		t.Fatalf("title buttons = %d, want one", len(buttons))
	}
	if buttons[0].Tooltip != i18n.T("Pane.Branches.ViewMenu") {
		t.Fatalf("tooltip = %q", buttons[0].Tooltip)
	}
	if buttons[0].Icon != nil {
		t.Fatal("the view button must stay the engine's own hamburger glyph")
	}
	if buttons[0].MenuFunc == nil {
		t.Fatal("the button must build its menu on every opening")
	}
	if len(readOnDispatcher(t, a, buttons[0].MenuFunc)) == 0 {
		t.Fatal("the button menu is empty")
	}
}

func TestTheViewMenuKeepsItsOrderAndTheStoredChoices(t *testing.T) {
	a := newTestApp(t)
	items := branchesPaneMenu(t, a)
	want := []string{
		i18n.T("Menu.Branches.Sort"),
		"-",
		i18n.T("Menu.Branches.FlowSections"),
		"-",
		i18n.T("Menu.Branches.Group"),
		i18n.T("Menu.Branches.Group.ExceptSingles"),
		i18n.T("Menu.Branches.Group.GroupsFirst"),
		i18n.T("Menu.Branches.Group.AfterLastSlash"),
		"-",
		i18n.T("Menu.Branches.SelectObsolete"),
	}
	if got := menuTexts(items); !slices.Equal(got, want) {
		t.Fatalf("menu = %v, want %v", got, want)
	}

	sort := items[0].SubItems
	if len(sort) != len(branchSortChoices) {
		t.Fatalf("sort items = %v", menuTexts(sort))
	}
	for i, item := range sort {
		if !item.Checkable || item.RadioGroup != branchesSortRadioGroup {
			t.Fatalf("sort item %d = %+v, want a radio item", i, item)
		}
		if item.Checked != (i == 0) {
			t.Fatalf("sort item %q checked = %v, want the stored sort by name", item.Text, item.Checked)
		}
	}
	if !items[4].Checked {
		t.Fatal("grouping by path must start checked")
	}
	if items[5].Disabled || items[6].Disabled || items[7].Disabled {
		t.Fatal("the grouping sub-options must be reachable while grouping is on")
	}
	if !items[len(items)-1].Disabled {
		t.Fatal("selecting obsolete branches makes no sense without a repository")
	}
}

func TestTurningGroupingOffGreysItsSubOptions(t *testing.T) {
	a, paths := newTestAppWithPaths(t)
	clickViewMenuItem(t, a, branchesPaneMenu(t, a), "Menu.Branches.Group")

	items := branchesPaneMenu(t, a)
	if items[4].Checked {
		t.Fatal("grouping by path must be off now")
	}
	if !items[5].Disabled || !items[6].Disabled || !items[7].Disabled {
		t.Fatal("the grouping sub-options must be greyed out while grouping is off")
	}
	if readOnDispatcher(t, a, a.branchesView.Options).Grouping.ByPath {
		t.Fatal("the tree must stop grouping by path")
	}
	stored, err := config.Load(paths.ConfigFile())
	if err != nil {
		t.Fatal(err)
	}
	if stored.UI.Branches.GroupByPath {
		t.Fatal("config.toml must remember that grouping is off")
	}
}

func TestEveryViewMenuToggleSurvivesARestart(t *testing.T) {
	a, paths := newTestAppWithPaths(t)
	for _, key := range []string{
		"Menu.Branches.FlowSections",
		"Menu.Branches.Group.ExceptSingles",
		"Menu.Branches.Group.GroupsFirst",
		"Menu.Branches.Group.AfterLastSlash",
	} {
		clickViewMenuItem(t, a, branchesPaneMenu(t, a), key)
	}
	clickViewMenuItem(t, a, branchesPaneMenu(t, a)[0].SubItems, "Menu.Branches.Sort.CommitTime")

	stored, err := config.Load(paths.ConfigFile())
	if err != nil {
		t.Fatal(err)
	}
	want := config.BranchesPane{
		Sort:                config.BranchSortCommitTime,
		FlowSections:        true,
		GroupByPath:         true,
		GroupExceptSingles:  true,
		GroupsFirst:         true,
		GroupAfterLastSlash: true,
	}
	if stored.UI.Branches != want {
		t.Fatalf("stored options = %+v, want %+v", stored.UI.Branches, want)
	}

	restarted := newTestAppWithConfig(t, stored)
	options := readOnDispatcher(t, restarted, restarted.branchesView.Options)
	if options != branchesPaneOptions(want) {
		t.Fatalf("options after restart = %+v, want %+v", options, branchesPaneOptions(want))
	}
	if options.Sort != branches.SortByCommitTime || !options.FlowSections {
		t.Fatalf("options after restart = %+v", options)
	}
}

func TestPickingTheSortAlreadyInUseChangesNothing(t *testing.T) {
	a, paths := newTestAppWithPaths(t)
	clickViewMenuItem(t, a, branchesPaneMenu(t, a)[0].SubItems, "Menu.Branches.Sort.Name")
	if _, err := config.Load(paths.ConfigFile()); err != nil {
		t.Fatal(err)
	}
	if readOnDispatcher(t, a, a.branchesView.Options).Sort != branches.SortByName {
		t.Fatal("the sort must stay by name")
	}
}

func TestPickingASortThatNeedsNoCommitsOnlyChangesTheTree(t *testing.T) {
	a, paths := newTestAppWithPaths(t)
	clickViewMenuItem(t, a, branchesPaneMenu(t, a)[0].SubItems, "Menu.Branches.Sort.ReverseNumbers")

	if got := readOnDispatcher(t, a, a.branchesView.Options).Sort; got != branches.SortByNameReverseNumbers {
		t.Fatalf("sort = %v, want by name with numbers in reverse order", got)
	}
	stored, err := config.Load(paths.ConfigFile())
	if err != nil {
		t.Fatal(err)
	}
	if stored.UI.Branches.Sort != config.BranchSortNameReverseNumbers {
		t.Fatalf("stored sort = %q", stored.UI.Branches.Sort)
	}
}

func TestSortingByCommitTimePutsTheNewestBranchOnTop(t *testing.T) {
	a, target := forkedApp(t, false)
	clickViewMenuItem(t, a, branchesPaneMenu(t, a)[0].SubItems, "Menu.Branches.Sort.CommitTime")
	waitForWorkingIdle(t, a)

	snap := readOnDispatcher(t, a, func() branches.Snapshot {
		loaded, err := loadBranchSnapshot(a.opened().store)
		if err != nil {
			t.Error(err)
		}
		a.enrichBranchSnapshot(a.opened(), &loaded)
		return loaded
	})
	if len(snap.Local) == 0 {
		t.Fatalf("no local branches in %s", target)
	}
	for _, b := range snap.Local {
		if b.When.IsZero() {
			t.Fatalf("branch %s has no commit time", b.Name)
		}
	}
}

func TestSelectObsoleteBranchesSaysHowManyAreLeftOver(t *testing.T) {
	a, target := forkedApp(t, false)
	runOnDispatcher(t, a, func() { a.selectObsoleteBranches() })
	if got := statusTextOf(t, a); got != i18n.T("Status.ObsoleteBranchesNone") {
		t.Fatalf("status without obsolete branches = %q", got)
	}

	branch := firstLocalBranchName(t, a)
	setTestUpstream(t, target, branch, "origin")
	runOnDispatcher(t, a, a.RefreshRepository)
	waitForWorkingIdle(t, a)
	runOnDispatcher(t, a, func() { a.selectObsoleteBranches() })

	if got := statusTextOf(t, a); got != i18n.Tf("Status.ObsoleteBranches", 1) {
		t.Fatalf("status = %q, want one obsolete branch", got)
	}
}

func statusTextOf(t *testing.T, a *App) string {
	t.Helper()
	return readOnDispatcher(t, a, a.statusLabel.Text)
}

func firstLocalBranchName(t *testing.T, a *App) string {
	t.Helper()
	return readOnDispatcher(t, a, func() string {
		snap, err := loadBranchSnapshot(a.opened().store)
		if err != nil {
			t.Error(err)
		}
		for _, b := range snap.Local {
			if b.Name.Short() != snap.Current {
				return b.Name.Short()
			}
		}
		t.Error("the fixture has no branch besides the current one")
		return ""
	})
}

func setTestUpstream(t *testing.T, target, branch, remote string) {
	t.Helper()
	file, ok := openRepoAt(t, target).Config().File(gitconfig.LevelLocal)
	if !ok {
		t.Fatal("the repository has no local config")
	}
	if err := file.Set("branch."+branch+".remote", remote); err != nil {
		t.Fatal(err)
	}
	if err := file.Set("branch."+branch+".merge", "refs/heads/"+branch); err != nil {
		t.Fatal(err)
	}
	if err := file.Save(file.Path()); err != nil {
		t.Fatal(err)
	}
}

type paneFlow struct {
	layout     ops.FlowConfig
	configured bool
}

func paneFlowOf(t *testing.T, a *App) paneFlow {
	t.Helper()
	return readOnDispatcher(t, a, func() paneFlow {
		layout, configured := a.branchesView.Flow()
		return paneFlow{layout: layout, configured: configured}
	})
}

func TestTheFlowLayoutReachesTheBranchesPane(t *testing.T) {
	a, _ := flowReadyApp(t, true)
	if got := paneFlowOf(t, a); !got.configured || got.layout.FeaturePrefix != "topic/" {
		t.Fatalf("flow in the pane = %+v", got)
	}

	runOnDispatcher(t, a, a.CloseRepository)
	if got := paneFlowOf(t, a); got.configured || got.layout != (ops.FlowConfig{}) {
		t.Fatalf("flow after closing = %+v", got)
	}
}

func TestTheViewMenuButtonKeepsItsTooltipAfterALanguageSwitch(t *testing.T) {
	a := newTestApp(t)
	runOnDispatcher(t, a, func() { a.SetLanguage("ru") })
	tooltip := readOnDispatcher(t, a, func() string {
		return a.Dock().FindPane(paneBranches).TitleButtons()[0].Tooltip
	})
	if tooltip != i18n.T("Pane.Branches.ViewMenu") {
		t.Fatalf("tooltip = %q", tooltip)
	}
	if tooltip == "" {
		t.Fatal("the tooltip went missing")
	}
}

func TestWiringTheViewMenuWithoutTheBranchesPaneIsHarmless(t *testing.T) {
	widget.ClearStrings()
	t.Cleanup(widget.ClearStrings)
	a, err := NewFromXAML(config.Default(), config.Paths{Dir: t.TempDir()}, []byte(completeWindowXAML()), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)
	if a.Dock().FindPane(paneBranches) != nil {
		t.Fatal("this window has no docked branches pane")
	}
	runOnDispatcher(t, a, a.wireBranchesPaneButtons)
}

func TestEnrichingWithoutAnOpenRepositoryIsHarmless(t *testing.T) {
	a := newTestApp(t)
	snap := branches.Snapshot{}
	runOnDispatcher(t, a, func() { a.enrichBranchSnapshot(nil, &snap) })
	if len(snap.Local) != 0 {
		t.Fatalf("snapshot = %+v", snap)
	}
}

func TestBranchesPaneOptionsIgnoreAnUnknownSort(t *testing.T) {
	options := branchesPaneOptions(config.BranchesPane{Sort: "whenever"})
	if options.Sort != branches.SortByName {
		t.Fatalf("sort = %v, want by name", options.Sort)
	}
}

func TestConfigDirectoryIsUsedForTheStoredOptions(t *testing.T) {
	a, paths := newTestAppWithPaths(t)
	if filepath.Dir(paths.ConfigFile()) != paths.Dir {
		t.Fatalf("config file %q is outside %q", paths.ConfigFile(), paths.Dir)
	}
	runOnDispatcher(t, a, a.saveBranchesPaneOptions)
}

func TestAViewMenuToggleLogsAFailedConfigSave(t *testing.T) {
	widget.ClearStrings()
	t.Cleanup(widget.ClearStrings)
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "config.toml"), 0o700); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	a, err := New(config.Default(), config.Paths{Dir: dir}, logger)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)

	clickViewMenuItem(t, a, branchesPaneMenu(t, a), "Menu.Branches.Group")

	if !strings.Contains(buf.String(), "save config failed") {
		t.Fatalf("expected the failed save to be logged: %s", buf.String())
	}
}
