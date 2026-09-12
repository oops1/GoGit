package diffview

import (
	"slices"
	"testing"

	"github.com/oops1/headless-gui/v3/widget"
)

func rowY(row int) int { return row*defaultRowHeight + 2 }

func clickRow(eng interface {
	SendMouseButton(x, y int, btn widget.MouseButton, pressed bool)
}, row int) {
	eng.SendMouseButton(40, rowY(row), widget.MouseLeft, true)
	eng.SendMouseButton(40, rowY(row), widget.MouseLeft, false)
}

func TestShiftClickSelectsTheLinesInBetween(t *testing.T) {
	eng, dv := newBoundView(t, func(dv *DiffView) { dv.SetDocument(parseFixture(t, "simple.diff")) })

	clickRow(eng, 1)
	eng.SetModifiers(widget.ModShift)
	clickRow(eng, 4)
	eng.SetModifiers(0)

	want := []LineRef{{0, 0}, {0, 1}, {0, 2}, {0, 3}, {0, 4}}
	if got := dv.SelectedLines(); !slices.Equal(got, want) {
		t.Fatalf("lines = %v, want %v", got, want)
	}
	if got := dv.SelectedHunks(); !slices.Equal(got, []int{0}) {
		t.Fatalf("hunks = %v", got)
	}
}

func TestShiftClickWithoutASelectionStartsTheRangeThere(t *testing.T) {
	eng, dv := newBoundView(t, func(dv *DiffView) { dv.SetDocument(parseFixture(t, "simple.diff")) })

	eng.SetModifiers(widget.ModShift)
	clickRow(eng, 3)
	eng.SetModifiers(0)

	if got := dv.SelectedLines(); !slices.Equal(got, []LineRef{{0, 2}}) {
		t.Fatalf("lines = %v", got)
	}
}

func TestNothingSelectedGivesNoLinesAndNoHunks(t *testing.T) {
	_, dv := newBoundView(t, func(dv *DiffView) { dv.SetDocument(parseFixture(t, "simple.diff")) })

	if dv.SelectedLines() != nil || dv.SelectedHunks() != nil {
		t.Fatalf("lines = %v, hunks = %v", dv.SelectedLines(), dv.SelectedHunks())
	}
}

func TestAHunkHeaderInTheRangeAddsTheHunkButNoLine(t *testing.T) {
	eng, dv := newBoundView(t, func(dv *DiffView) { dv.SetDocument(parseFixture(t, "simple.diff")) })

	clickRow(eng, 0)
	eng.SetModifiers(widget.ModShift)
	clickRow(eng, 1)
	eng.SetModifiers(0)

	if got := dv.SelectedLines(); !slices.Equal(got, []LineRef{{0, 0}}) {
		t.Fatalf("lines = %v", got)
	}
	if got := dv.SelectedHunks(); !slices.Equal(got, []int{0}) {
		t.Fatalf("hunks = %v", got)
	}
}

func missingNewlineDocument() Document {
	return Document{Hunks: []Hunk{{Lines: []Line{
		{Kind: Removed, OldNo: 1, Text: "a"},
		{Kind: NoNewline, Text: "no newline"},
		{Kind: Added, NewNo: 1, Text: "b"},
	}}}}
}

func TestTheMissingNewlineRowIsNeverALineOfItsOwn(t *testing.T) {
	eng, dv := newBoundView(t, func(dv *DiffView) { dv.SetDocument(missingNewlineDocument()) })

	clickRow(eng, 0)
	eng.SetModifiers(widget.ModShift)
	clickRow(eng, 2)
	eng.SetModifiers(0)

	if got := dv.SelectedLines(); !slices.Equal(got, []LineRef{{0, 0}, {0, 2}}) {
		t.Fatalf("lines = %v", got)
	}
}

func TestTheUnifiedModeSelectsOneLinePerRow(t *testing.T) {
	eng, dv := newBoundView(t, func(dv *DiffView) {
		dv.SetDocument(parseFixture(t, "simple.diff"))
		dv.SetMode(Unified)
	})

	clickRow(eng, 4)
	eng.SetModifiers(widget.ModShift)
	clickRow(eng, 5)
	eng.SetModifiers(0)

	if got := dv.SelectedLines(); !slices.Equal(got, []LineRef{{0, 3}, {0, 4}}) {
		t.Fatalf("lines = %v", got)
	}
}

func TestARightClickOpensTheMenuTheOwnerFills(t *testing.T) {
	_, dv := newBoundView(t, func(dv *DiffView) { dv.SetDocument(parseFixture(t, "simple.diff")) })
	dv.OnMenu = func() []widget.MenuItem { return []widget.MenuItem{{Text: "Stage"}} }

	if !dv.OnMouseButton(widget.MouseEvent{X: 40, Y: rowY(4), Button: widget.MouseRight, Pressed: true}) {
		t.Fatal("the right click was not taken")
	}

	if !dv.HasOverlay() || dv.OverlayBounds().Empty() {
		t.Fatal("the menu did not open")
	}
	if _, line, ok := dv.Selected(); !ok || line != 3 {
		t.Fatalf("selection = %d %v, want the clicked row", line, ok)
	}
	if !dv.OnMouseButton(widget.MouseEvent{X: 40, Y: rowY(4), Button: widget.MouseRight}) {
		t.Fatal("releasing the button that opened the menu must be swallowed")
	}
}

func TestARightClickInsideTheRangeKeepsIt(t *testing.T) {
	eng, dv := newBoundView(t, func(dv *DiffView) { dv.SetDocument(parseFixture(t, "simple.diff")) })
	dv.OnMenu = func() []widget.MenuItem { return []widget.MenuItem{{Text: "Stage"}} }
	clickRow(eng, 1)
	eng.SetModifiers(widget.ModShift)
	clickRow(eng, 4)
	eng.SetModifiers(0)

	dv.OnMouseButton(widget.MouseEvent{X: 40, Y: rowY(2), Button: widget.MouseRight, Pressed: true})

	if got := len(dv.SelectedLines()); got != 5 {
		t.Fatalf("lines = %d, want the range kept", got)
	}
}

func TestAMenuWithNothingInItStaysShut(t *testing.T) {
	_, dv := newBoundView(t, func(dv *DiffView) { dv.SetDocument(parseFixture(t, "simple.diff")) })
	dv.OnMenu = func() []widget.MenuItem { return nil }

	if dv.OnMouseButton(widget.MouseEvent{X: 40, Y: rowY(4), Button: widget.MouseRight, Pressed: true}) {
		t.Fatal("an empty menu took the click")
	}
	if dv.HasOverlay() {
		t.Fatal("an empty menu opened")
	}
}

func TestTheOpenMenuGetsTheEventsAndClosesOnAClickElsewhere(t *testing.T) {
	_, dv := newBoundView(t, func(dv *DiffView) { dv.SetDocument(parseFixture(t, "simple.diff")) })
	clicked := 0
	dv.OnMenu = func() []widget.MenuItem {
		return []widget.MenuItem{{Text: "Stage", OnClick: func() { clicked++ }}}
	}
	dv.OnMouseButton(widget.MouseEvent{X: 40, Y: rowY(1), Button: widget.MouseRight, Pressed: true})
	inside := dv.menu.Bounds()
	x, y := inside.Min.X+10, inside.Min.Y+10

	dv.OnMouseMove(x, y)
	dv.OnKeyEvent(widget.KeyEvent{Code: widget.KeyDown, Pressed: true})
	dv.OnMouseButton(widget.MouseEvent{X: x, Y: y, Button: widget.MouseLeft, Pressed: true})
	dv.OnMouseButton(widget.MouseEvent{X: x, Y: y, Button: widget.MouseLeft})

	if clicked != 1 {
		t.Fatalf("menu item ran %d times", clicked)
	}
	dv.OnMouseButton(widget.MouseEvent{X: 40, Y: rowY(1), Button: widget.MouseRight, Pressed: true})
	dv.OnMouseButton(widget.MouseEvent{X: 40, Y: rowY(5), Button: widget.MouseLeft, Pressed: true})
	if dv.HasOverlay() {
		t.Fatal("a click outside the menu left it open")
	}
	if _, line, ok := dv.Selected(); !ok || line != 5 {
		t.Fatalf("selection = %d %v, want the row clicked outside the menu", line, ok)
	}
}

func TestTheOpenMenuIsPaintedAboveTheDiff(t *testing.T) {
	eng, dv := newBoundView(t, func(dv *DiffView) { dv.SetDocument(parseFixture(t, "simple.diff")) })
	dv.OnMenu = func() []widget.MenuItem { return []widget.MenuItem{{Text: "Stage"}} }
	dv.OnMouseButton(widget.MouseEvent{X: 40, Y: rowY(4), Button: widget.MouseRight, Pressed: true})

	_ = eng.RenderOnce()

	if !dv.HasOverlay() {
		t.Fatal("painting the frame closed the menu")
	}
}

func TestAButtonTheViewDoesNotKnowIsLeftAlone(t *testing.T) {
	_, dv := newBoundView(t, func(dv *DiffView) { dv.SetDocument(parseFixture(t, "simple.diff")) })

	if dv.OnMouseButton(widget.MouseEvent{X: 40, Y: rowY(4), Button: widget.MouseButton(99), Pressed: true}) {
		t.Fatal("an unknown button was taken")
	}
}
