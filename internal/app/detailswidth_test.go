package app

import (
	"testing"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/config"
)

func detailsSideSize(t *testing.T, a *App, side widget.DockSide) int {
	t.Helper()
	return readOnDispatcher(t, a, func() int { return a.Dock().SideSize(side) })
}

func TestTheDetailsPaneWidensToShowEveryTab(t *testing.T) {
	a := newTestApp(t)
	tabs := a.Widget("detailsTabs").(*widget.TabControl)
	need := readOnDispatcher(t, a, func() int { return detailsTabsWidth(tabs) })

	readOnDispatcher(t, a, func() bool {
		a.Dock().SetSideSize(widget.DockRight, 80)
		a.keepDetailsTabsVisible()
		return true
	})
	if got := detailsSideSize(t, a, widget.DockRight); got < need {
		t.Fatalf("details side = %d, want at least %d", got, need)
	}

	readOnDispatcher(t, a, func() bool {
		a.Dock().SetSideSize(widget.DockRight, need+120)
		a.keepDetailsTabsVisible()
		return true
	})
	if got := detailsSideSize(t, a, widget.DockRight); got != need+120 {
		t.Fatalf("a wide details side changed to %d", got)
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

func TestTheDetailsPaneOnTheBottomOrMissingIsLeftAlone(t *testing.T) {
	a := newTestApp(t)
	readOnDispatcher(t, a, func() bool {
		a.Dock().FindPane(paneDetails).Dock(widget.DockBottom)
		a.Dock().SetSideSize(widget.DockBottom, 90)
		a.keepDetailsTabsVisible()
		return true
	})
	if got := detailsSideSize(t, a, widget.DockBottom); got != 90 {
		t.Fatalf("bottom side = %d, want it untouched", got)
	}

	bare, err := NewFromXAML(config.Default(), config.Paths{Dir: t.TempDir()}, []byte(completeWindowXAML()), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(bare.Close)
	bare.keepDetailsTabsVisible()
}
