package app

import (
	"image/color"
	"testing"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/config"
	"github.com/oops1/gogit/internal/ui/branches"
	"github.com/oops1/gogit/internal/ui/settings"
	"github.com/oops1/gogit/internal/ui/sidebar"
)

func TestTheSideBarTakesThePanesAndGivesThemBack(t *testing.T) {
	a := newTestApp(t)
	dock := a.Dock()
	panes := map[string]widget.Widget{}
	for _, id := range []string{paneRepositories, paneBranches, paneFiles, paneJournal, paneDetails} {
		panes[id] = dock.FindPane(id).Content()
	}
	diff := dock.Center()

	readOnDispatcher(t, a, func() bool {
		a.applyLayoutMode(config.LayoutSidebar)
		a.applyLayoutMode(config.LayoutSidebar)
		return true
	})

	for id := range panes {
		if dock.FindPane(id).Content() != nil {
			t.Fatalf("pane %s still holds its content in the side bar mode", id)
		}
	}
	if dock.Center() != nil || widget.IsWidgetVisible(dock) || !widget.IsWidgetVisible(a.sidebar.Root()) || !a.sidebarMounted {
		t.Fatalf("center = %v, dock visible = %v, side bar visible = %v", dock.Center(), widget.IsWidgetVisible(dock), widget.IsWidgetVisible(a.sidebar.Root()))
	}

	readOnDispatcher(t, a, func() bool { a.applyLayoutMode(config.LayoutDocks); return true })

	for id, content := range panes {
		if dock.FindPane(id).Content() != content {
			t.Fatalf("pane %s did not get its content back", id)
		}
	}
	if dock.Center() != diff || !widget.IsWidgetVisible(dock) || widget.IsWidgetVisible(a.sidebar.Root()) || a.sidebarMounted {
		t.Fatal("the dock layout did not come back")
	}
}

func TestAPaneMovedBackFromTheSideBarWearsTheCurrentTheme(t *testing.T) {
	cfg := config.Default()
	cfg.UI.Layout = config.LayoutSidebar
	a := newTestAppWithConfig(t, cfg)
	tree := a.Widget("reposTree").(*widget.TreeViewWidget)

	for _, theme := range []string{config.ThemeDark, config.ThemeLight} {
		got := readOnDispatcher(t, a, func() [2]color.RGBA {
			a.SetTheme(theme)
			return [2]color.RGBA{tree.Tree.Theme.Background, a.theme().WindowBG}
		})
		if got[0] != got[1] {
			t.Fatalf("%s theme: hidden repositories background = %v, want %v", theme, got[0], got[1])
		}
	}

	got := readOnDispatcher(t, a, func() [2]color.RGBA {
		a.applyLayoutMode(config.LayoutDocks)
		return [2]color.RGBA{tree.Tree.Theme.Background, a.theme().WindowBG}
	})
	if got[0] != got[1] {
		t.Fatalf("repositories background after the switch = %v, want %v", got[0], got[1])
	}
}

func TestTheSideBarCountsBranchesTagsRemotesAndFiles(t *testing.T) {
	a := newTestApp(t)

	readOnDispatcher(t, a, func() bool {
		a.showSidebarBranchCounts(branches.Snapshot{
			Local:   make([]branches.Branch, 2),
			Tags:    make([]branches.Tag, 1),
			Remotes: []branches.Remote{{Branches: make([]branches.Branch, 3)}, {Branches: make([]branches.Branch, 1)}},
		})
		a.showSidebarWorkingCounts(7, 2)
		return true
	})

	for section, want := range map[sidebar.Section]string{
		sidebar.SectionBranches:    "2",
		sidebar.SectionTags:        "1",
		sidebar.SectionRemotes:     "4",
		sidebar.SectionStash:       "—",
		sidebar.SectionWorkingCopy: "7",
		sidebar.SectionIndex:       "2",
	} {
		if got, ok := a.sidebar.CountText(section); !ok || got != want {
			t.Fatalf("section %d count = %q, want %q", section, got, want)
		}
	}
}

func TestAWindowWithoutTheMainGridHasNoSideBar(t *testing.T) {
	a, err := NewFromXAML(config.Default(), config.Paths{Dir: t.TempDir()}, []byte(completeWindowXAML()), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)

	a.applyLayoutMode(config.LayoutSidebar)
	a.restyleSidebar(widget.Win11DarkTheme())
	a.retitleSidebar()
	a.showSidebarBranchCounts(branches.Snapshot{})
	a.showSidebarWorkingCounts(1, 1)

	if a.sidebar != nil || a.sidebarMounted {
		t.Fatal("a side bar appeared without the main grid")
	}
}

func TestTheLayoutModeComesFromTheConfigAndTheSettings(t *testing.T) {
	cfg := config.Default()
	cfg.UI.Layout = config.LayoutSidebar
	started := newTestAppWithConfig(t, cfg)
	if !readOnDispatcher(t, started, func() bool { return started.sidebarMounted }) {
		t.Fatal("the side bar mode from the config was not applied at start")
	}

	a := newTestApp(t)
	stubShowSettings(a, settings.Model{
		Language: "en", Theme: config.ThemeSystem, LogMaxCount: 500, FetchInterval: 300,
		Layout: config.LayoutSidebar,
	}, true)
	a.Dispatch(CmdSettings)

	if !readOnDispatcher(t, a, func() bool { return a.sidebarMounted }) || a.Config().UI.Layout != config.LayoutSidebar {
		t.Fatalf("mounted = %v, layout = %q", a.sidebarMounted, a.Config().UI.Layout)
	}
	readOnDispatcher(t, a, func() bool {
		a.SetTheme(config.ThemeDark)
		a.SetLanguage("ru")
		a.SetLanguage("en")
		return true
	})
}

func TestPanesThatDoNotExistAreLeftAlone(t *testing.T) {
	a := newTestApp(t)

	if got := takePaneContent(a.Dock(), "no such pane"); got != nil {
		t.Fatalf("content = %v", got)
	}
	putPaneContent(a.Dock(), "no such pane", widget.NewLabel("x", widget.CurrentTheme().LabelText))
}
