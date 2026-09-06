package settings

import (
	"image"
	"testing"

	"github.com/oops1/headless-gui/v3/widget"
)

func TestShortSectionFillsTheViewportWithoutScrolling(t *testing.T) {
	v := newTestView(t, []string{"en"}, Model{})
	v.SetSection("general")

	view := v.scroll.Bounds()
	if v.scroll.ContentHeight > view.Dy() {
		t.Fatalf("content height = %d, want it to fit in the viewport %d", v.scroll.ContentHeight, view.Dy())
	}
	if got := v.sectionHost.Bounds().Dy(); got != view.Dy() {
		t.Fatalf("host height = %d, want the viewport height %d", got, view.Dy())
	}
	if got := v.sectionHost.Bounds().Dx(); got != view.Dx() {
		t.Fatalf("host width = %d, want the full viewport width %d", got, view.Dx())
	}
}

func TestLongSectionScrollsAndLeavesRoomForTheScrollbar(t *testing.T) {
	v := newTestView(t, []string{"en"}, Model{})
	v.Dialog().Resize(dialogDefaultWidth, dialogDefaultHeight)
	v.SetSection("credentials")

	view := v.scroll.Bounds()
	if v.scroll.ContentHeight <= view.Dy() {
		t.Fatalf("content height = %d, want more than the viewport %d", v.scroll.ContentHeight, view.Dy())
	}
	if got := v.sectionHost.Bounds().Dy(); got != v.scroll.ContentHeight {
		t.Fatalf("host height = %d, want the content height %d", got, v.scroll.ContentHeight)
	}
	if got, want := v.sectionHost.Bounds().Dx(), view.Dx()-scrollbarWidth; got != want {
		t.Fatalf("host width = %d, want %d", got, want)
	}
}

func TestCollapsingTheCredentialFormRemovesTheScrolling(t *testing.T) {
	v := newTestView(t, []string{"en"}, Model{})
	v.Dialog().Resize(dialogDefaultWidth, dialogDefaultHeight)
	v.SetSection("credentials")
	scrolled := v.scroll.ContentHeight

	v.credentialForm.SetExpanded(false)

	if v.scroll.ContentHeight >= scrolled {
		t.Fatalf("content height = %d, want less than %d after collapsing the form", v.scroll.ContentHeight, scrolled)
	}
	if v.scroll.ContentHeight > v.scroll.Bounds().Dy() {
		t.Fatal("the collapsed section must fit in the viewport")
	}
}

func TestCollapsingTheSSHFormShrinksTheSection(t *testing.T) {
	v := newTestView(t, []string{"en"}, Model{})
	v.SetSection("ssh")
	expanded := v.scroll.ContentHeight

	v.sshForm.SetExpanded(false)

	if want := expanded - (sshFormRowExpanded - expanderRowCollapsed); v.scroll.ContentHeight != want {
		t.Fatalf("content height = %d, want %d", v.scroll.ContentHeight, want)
	}
}

func TestResizingTheDialogRelaysOutTheScrolledSection(t *testing.T) {
	v := newTestView(t, []string{"en"}, Model{})
	v.SetSection("credentials")

	v.Dialog().Resize(dialogDefaultWidth+200, dialogDefaultHeight)

	if got, want := v.sectionHost.Bounds().Dx(), v.scroll.Bounds().Dx()-scrollbarWidth; got != want {
		t.Fatalf("host width after resize = %d, want %d", got, want)
	}
}

func TestSectionHeightIsZeroWhileNoSectionIsChosen(t *testing.T) {
	v := &View{section: "nothing"}

	if got := v.sectionContentHeight(); got != 0 {
		t.Fatalf("content height = %d, want 0", got)
	}
}

func TestGridPixelHeightCountsOnlyFixedRows(t *testing.T) {
	grid := widget.NewGrid()
	grid.RowDefs = []widget.GridDefinition{
		{Mode: widget.GridSizePixel, Value: 30},
		{Mode: widget.GridSizeStar, Value: 1},
		{Mode: widget.GridSizePixel, Value: 12},
	}

	if got := gridPixelHeight(grid); got != 42 {
		t.Fatalf("gridPixelHeight = %d, want 42", got)
	}
}

func TestScrollerKeepsTheOffsetInsideTheContent(t *testing.T) {
	v := newTestView(t, []string{"en"}, Model{})
	v.Dialog().Resize(dialogDefaultWidth, dialogDefaultHeight)
	v.SetSection("credentials")
	v.scroll.SetScrollY(v.scroll.ContentHeight)
	scrolled := v.scroll.ScrollY()

	v.credentialForm.SetExpanded(false)

	if v.scroll.ScrollY() >= scrolled {
		t.Fatalf("scroll offset = %d, want it clamped below %d once the content shrank", v.scroll.ScrollY(), scrolled)
	}
}

func TestScrollerLaysOutTheHostOnEveryBoundsChange(t *testing.T) {
	v := newTestView(t, []string{"en"}, Model{})
	v.Dialog().Resize(dialogDefaultWidth, dialogDefaultHeight)
	v.SetSection("credentials")
	b := v.scroll.Bounds()

	v.scroll.SetBounds(image.Rect(b.Min.X, b.Min.Y, b.Max.X-100, b.Max.Y))

	if got, want := v.sectionHost.Bounds().Dx(), b.Dx()-100-scrollbarWidth; got != want {
		t.Fatalf("host width = %d, want %d", got, want)
	}
}
