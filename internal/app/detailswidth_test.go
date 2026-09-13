package app

import (
	"testing"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/config"
)

func detailsMinSize(t *testing.T, a *App) int {
	t.Helper()
	return readOnDispatcher(t, a, func() int { return a.Dock().FindPane(paneDetails).MinSize })
}

func TestTheDetailsPaneCannotBeNarrowerThanItsTabs(t *testing.T) {
	a := newTestApp(t)
	tabs := a.Widget("detailsTabs").(*widget.TabControl)
	need := readOnDispatcher(t, a, func() int { return detailsTabsWidth(tabs) })

	if got := detailsMinSize(t, a); got != need {
		t.Fatalf("details minimum = %d, want %d", got, need)
	}
	width := readOnDispatcher(t, a, func() int {
		dock := a.Dock()
		dock.SetSideSize(dock.FindPane(paneDetails).Side(), 80)
		dock.SetBounds(dock.Bounds())
		return dock.FindPane(paneDetails).Bounds().Dx()
	})
	if width < need {
		t.Fatalf("details pane dragged to %d, narrower than its tabs %d", width, need)
	}
}

func TestTheDetailsWidthCountsOnlyVisibleTabs(t *testing.T) {
	a := newTestApp(t)
	tabs := a.Widget("detailsTabs").(*widget.TabControl)

	width := readOnDispatcher(t, a, func() int { return detailsTabsWidth(tabs) })
	narrower := readOnDispatcher(t, a, func() int {
		tabs.SetTabVisible(2, false)
		defer tabs.SetTabVisible(2, true)
		return detailsTabsWidth(tabs)
	})

	if narrower >= width || width <= detailsTabsMargin {
		t.Fatalf("width = %d, without a tab = %d", width, narrower)
	}
}

func TestTheDetailsMinimumFollowsTheSideItIsDockedTo(t *testing.T) {
	a := newTestApp(t)
	side := readOnDispatcher(t, a, func() widget.DockSide { return a.Dock().FindPane(paneDetails).Side() })
	need := detailsMinSize(t, a)

	readOnDispatcher(t, a, func() bool { a.Dock().FindPane(paneDetails).Dock(widget.DockBottom); return true })
	if got := detailsMinSize(t, a); got != 0 {
		t.Fatalf("details minimum at the bottom = %d, want none", got)
	}
	readOnDispatcher(t, a, func() bool { a.Dock().FindPane(paneDetails).Dock(side); return true })
	if got := detailsMinSize(t, a); got != need {
		t.Fatalf("details minimum back at the side = %d, want %d", got, need)
	}

	bare, err := NewFromXAML(config.Default(), config.Paths{Dir: t.TempDir()}, []byte(completeWindowXAML()), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(bare.Close)
	bare.keepDetailsTabsVisible()
}
