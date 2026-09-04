package diffview

import (
	"image"
	"image/color"
	"strconv"
	"testing"

	"github.com/oops1/headless-gui/v3/engine"
	"github.com/oops1/headless-gui/v3/widget"
)

const (
	viewWidth  = 420
	viewHeight = 220
)

func newBoundView(t *testing.T, prepare func(*DiffView)) (*engine.Engine, *DiffView) {
	t.Helper()
	eng := engine.New(viewWidth, viewHeight, 30)
	t.Cleanup(eng.Stop)
	dv := New()
	dv.SetFontFamily("")
	prepare(dv)
	eng.SetRoot(dv)
	return eng, dv
}

func longDocument(lines int) Document {
	hunk := Hunk{Header: "@@ -1," + strconv.Itoa(lines) + " +1," + strconv.Itoa(lines) + " @@ long"}
	for i := range lines {
		switch i % 4 {
		case 0:
			hunk.Lines = append(hunk.Lines, Line{Kind: Context, OldNo: i + 1, NewNo: i + 1,
				Text: "context line " + strconv.Itoa(i) + " with a fairly long tail of characters"})
		case 1, 2:
			hunk.Lines = append(hunk.Lines, Line{Kind: Removed, OldNo: i + 1, Text: "removed " + strconv.Itoa(i)})
		default:
			hunk.Lines = append(hunk.Lines, Line{Kind: Added, NewNo: i + 1, Text: "added " + strconv.Itoa(i)})
		}
	}
	return Document{OldName: "long.txt", NewName: "long.txt", Hunks: []Hunk{hunk}}
}

func TestNewStartsEmptyInSideBySideMode(t *testing.T) {
	dv := New()
	if dv.Mode() != SideBySide {
		t.Fatalf("mode = %v", dv.Mode())
	}
	if dv.RowCount() != 0 || !dv.Document().IsEmpty() {
		t.Fatal("new view must be empty")
	}
	if dv.FontFamily() != defaultFontFamily || dv.FontSize() != defaultFontSize || dv.RowHeight() != defaultRowHeight {
		t.Fatalf("defaults = %q %v %d", dv.FontFamily(), dv.FontSize(), dv.RowHeight())
	}
	if _, _, ok := dv.Selected(); ok {
		t.Fatal("new view must have no selection")
	}
}

func TestSetDocumentBuildsSideBySideRows(t *testing.T) {
	dv := New()
	dv.SetDocument(parseFixture(t, "simple.diff"))
	if dv.RowCount() != 6 {
		t.Fatalf("rows = %d", dv.RowCount())
	}
}

func TestSetModeRebuildsRows(t *testing.T) {
	dv := New()
	dv.SetDocument(parseFixture(t, "simple.diff"))
	dv.SetMode(Unified)
	if dv.Mode() != Unified {
		t.Fatalf("mode = %v", dv.Mode())
	}
	if dv.RowCount() != 7 {
		t.Fatalf("unified rows = %d", dv.RowCount())
	}
	dv.SetMode(Unified)
	if dv.RowCount() != 7 {
		t.Fatal("repeated SetMode must be a no-op")
	}
}

func TestClearRemovesDocument(t *testing.T) {
	dv := New()
	dv.SetDocument(parseFixture(t, "simple.diff"))
	dv.Clear()
	if dv.RowCount() != 0 || !dv.Document().IsEmpty() {
		t.Fatal("Clear must drop the document")
	}
}

func TestSettersReplaceRenderingParameters(t *testing.T) {
	dv := New()
	dv.SetFontFamily("Courier New")
	dv.SetFontSize(14)
	dv.SetRowHeight(24)
	dv.SetPalette(LightPalette())
	if dv.FontFamily() != "Courier New" || dv.FontSize() != 14 || dv.RowHeight() != 24 {
		t.Fatalf("settings = %q %v %d", dv.FontFamily(), dv.FontSize(), dv.RowHeight())
	}
	if dv.Palette().Background != LightPalette().Background {
		t.Fatal("SetPalette did not apply")
	}
}

func TestApplyThemePicksPaletteByBrightness(t *testing.T) {
	dv := New()
	dv.ApplyTheme(widget.Win11LightTheme())
	if isDarkColor(dv.Palette().Background) {
		t.Fatal("light theme must yield a light background")
	}
	if dv.Palette().AddedBG != LightPalette().AddedBG {
		t.Fatal("light theme must use the light diff colors")
	}
	dv.ApplyTheme(widget.Win11DarkTheme())
	if !isDarkColor(dv.Palette().Background) {
		t.Fatal("dark theme must yield a dark background")
	}
	if dv.Palette().AddedBG != DarkPalette().AddedBG {
		t.Fatal("dark theme must use the dark diff colors")
	}
	dv.ApplyTheme(nil)
	if dv.Palette().Background != DarkPalette().Background {
		t.Fatal("missing theme must fall back to the dark palette")
	}
}

func TestDrawsNothingForEmptyDocument(t *testing.T) {
	frame := renderView(t, 120, 60, func(dv *DiffView) {})
	background := DarkPalette().Background
	for y := range 60 {
		for x := range 120 {
			if frame.RGBAAt(x, y) != background {
				t.Fatalf("pixel (%d,%d) = %v, want the plain background %v", x, y, frame.RGBAAt(x, y), background)
			}
		}
	}
}

func TestDrawsNothingWithoutBounds(t *testing.T) {
	dv := New()
	dv.SetDocument(parseFixture(t, "simple.diff"))
	dv.SetBounds(image.Rectangle{})
	dv.Draw(recordingContext{})
}

func TestDrawsSideBySideDiff(t *testing.T) {
	frame := renderView(t, 420, 160, func(dv *DiffView) {
		dv.SetDocument(parseFixture(t, "simple.diff"))
	})
	assertGolden(t, "side-by-side-dark", frame)
}

func TestDrawsSideBySideDiffInLightTheme(t *testing.T) {
	frame := renderView(t, 420, 160, func(dv *DiffView) {
		dv.ApplyTheme(widget.Win11LightTheme())
		dv.SetDocument(parseFixture(t, "simple.diff"))
	})
	assertGolden(t, "side-by-side-light", frame)
}

func TestDrawsUnifiedDiff(t *testing.T) {
	frame := renderView(t, 420, 160, func(dv *DiffView) {
		dv.SetMode(Unified)
		dv.SetDocument(parseFixture(t, "nonewline.diff"))
	})
	assertGolden(t, "unified-dark", frame)
}

func TestDrawsUnifiedDiffInLightTheme(t *testing.T) {
	frame := renderView(t, 420, 160, func(dv *DiffView) {
		dv.ApplyTheme(widget.Win11LightTheme())
		dv.SetMode(Unified)
		dv.SetDocument(parseFixture(t, "nonewline.diff"))
	})
	assertGolden(t, "unified-light", frame)
}

func TestDrawsPlaceholdersAndSelectionWithBothScrollbars(t *testing.T) {
	frame := renderView(t, 420, 160, func(dv *DiffView) {
		dv.SetDocument(longDocument(40))
		dv.ScrollBy(30, 3*defaultRowHeight)
		dv.selectRow(4, false)
	})
	assertGolden(t, "scrolled-selection", frame)
	assertColumnHasColor(t, frame, 415, DarkPalette().ScrollThumb)
	assertRowHasColor(t, frame, 155, DarkPalette().ScrollThumb)
}

func assertColumnHasColor(t *testing.T, frame *image.RGBA, x int, want color.RGBA) {
	t.Helper()
	for y := frame.Bounds().Min.Y; y < frame.Bounds().Max.Y; y++ {
		if frame.RGBAAt(x, y) == want {
			return
		}
	}
	t.Fatalf("column %d has no pixel of %v", x, want)
}

func assertRowHasColor(t *testing.T, frame *image.RGBA, y int, want color.RGBA) {
	t.Helper()
	for x := frame.Bounds().Min.X; x < frame.Bounds().Max.X; x++ {
		if frame.RGBAAt(x, y) == want {
			return
		}
	}
	t.Fatalf("row %d has no pixel of %v", y, want)
}

func TestDrawsBinaryDocument(t *testing.T) {
	widget.RegisterString("en", "Diff.Binary", "Binary files differ")
	widget.SetLanguage("en")
	t.Cleanup(widget.ClearStrings)
	frame := renderView(t, 260, 80, func(dv *DiffView) {
		dv.SetDocument(parseFixture(t, "binary.diff"))
	})
	assertGolden(t, "binary", frame)
}

func TestDrawsSideBySideNoNewlineMarkerOnBothSides(t *testing.T) {
	doc := Document{Hunks: []Hunk{{Header: "@@ -1 +1 @@", Lines: []Line{
		{Kind: Context, OldNo: 1, NewNo: 1, Text: "a"},
		{Kind: NoNewline, Text: "No newline at end of file"},
	}}}}
	dv := New()
	dv.SetDocument(doc)
	if dv.RowCount() != 3 {
		t.Fatalf("rows = %d", dv.RowCount())
	}
	hunk, line, ok := dv.selectedAfterClickAtRow(t, 2)
	if !ok || hunk != 0 || line != 1 {
		t.Fatalf("selection = %d %d %v", hunk, line, ok)
	}
}

func (dv *DiffView) selectedAfterClickAtRow(t *testing.T, index int) (int, int, bool) {
	t.Helper()
	dv.SetBounds(image.Rect(0, 0, viewWidth, viewHeight))
	dv.selectRow(index, false)
	return dv.Selected()
}

func TestWheelScrollsVertically(t *testing.T) {
	eng, dv := newBoundView(t, func(dv *DiffView) { dv.SetDocument(longDocument(60)) })
	eng.SendMouseButton(10, 10, widget.MouseWheelDown, true)
	_, y := dv.ScrollOffset()
	if y != wheelRows*defaultRowHeight {
		t.Fatalf("scroll after wheel down = %d", y)
	}
	eng.SendMouseButton(10, 10, widget.MouseWheelUp, true)
	if _, y := dv.ScrollOffset(); y != 0 {
		t.Fatalf("scroll after wheel up = %d", y)
	}
	if !dv.OnMouseButton(widget.MouseEvent{Button: widget.MouseWheelDown}) {
		t.Fatal("wheel release must be consumed")
	}
	if _, y := dv.ScrollOffset(); y != 0 {
		t.Fatalf("wheel release changed scroll to %d", y)
	}
}

func TestKeyboardScrolls(t *testing.T) {
	_, dv := newBoundView(t, func(dv *DiffView) { dv.SetDocument(longDocument(200)) })
	press := func(code widget.KeyCode) { dv.OnKeyEvent(widget.KeyEvent{Code: code, Pressed: true}) }

	press(widget.KeyDown)
	if _, y := dv.ScrollOffset(); y != defaultRowHeight {
		t.Fatalf("down = %d", y)
	}
	press(widget.KeyUp)
	if _, y := dv.ScrollOffset(); y != 0 {
		t.Fatalf("up = %d", y)
	}
	press(widget.KeyPageDown)
	_, afterPage := dv.ScrollOffset()
	if afterPage <= defaultRowHeight {
		t.Fatalf("page down = %d", afterPage)
	}
	press(widget.KeyPageUp)
	if _, y := dv.ScrollOffset(); y != 0 {
		t.Fatalf("page up = %d", y)
	}
	press(widget.KeyEnd)
	_, bottom := dv.ScrollOffset()
	if bottom == 0 {
		t.Fatal("end must scroll to the bottom")
	}
	press(widget.KeyRight)
	if x, _ := dv.ScrollOffset(); x != horizontalStep {
		t.Fatalf("right = %d", x)
	}
	press(widget.KeyLeft)
	if x, _ := dv.ScrollOffset(); x != 0 {
		t.Fatalf("left = %d", x)
	}
	press(widget.KeyHome)
	if x, y := dv.ScrollOffset(); x != 0 || y != 0 {
		t.Fatalf("home = %d %d", x, y)
	}
	press(widget.KeyF5)
	if x, y := dv.ScrollOffset(); x != 0 || y != 0 {
		t.Fatalf("unrelated key moved the view to %d %d", x, y)
	}
	dv.OnKeyEvent(widget.KeyEvent{Code: widget.KeyDown})
	if _, y := dv.ScrollOffset(); y != 0 {
		t.Fatalf("key release scrolled to %d", y)
	}
}

func TestClickReportsClickedLine(t *testing.T) {
	eng, dv := newBoundView(t, func(dv *DiffView) { dv.SetDocument(parseFixture(t, "simple.diff")) })
	var gotHunk, gotLine int
	calls := 0
	dv.OnLineClick = func(hunk, line int) {
		gotHunk, gotLine = hunk, line
		calls++
	}
	eng.SendMouseButton(40, 4*defaultRowHeight+2, widget.MouseLeft, true)
	if calls != 1 || gotHunk != 0 || gotLine != 3 {
		t.Fatalf("callback = %d %d after %d calls", gotHunk, gotLine, calls)
	}
	hunk, line, ok := dv.Selected()
	if !ok || hunk != 0 || line != 3 {
		t.Fatalf("selection = %d %d %v", hunk, line, ok)
	}
	eng.SendMouseButton(40, 4*defaultRowHeight+2, widget.MouseLeft, true)
	if calls != 2 {
		t.Fatalf("repeated click calls = %d", calls)
	}
}

func TestClickOnRightPaneReportsTheAddedLine(t *testing.T) {
	eng, dv := newBoundView(t, func(dv *DiffView) { dv.SetDocument(parseFixture(t, "simple.diff")) })
	var gotLine int
	dv.OnLineClick = func(_, line int) { gotLine = line }
	eng.SendMouseButton(viewWidth-60, 4*defaultRowHeight+2, widget.MouseLeft, true)
	if gotLine != 4 {
		t.Fatalf("line = %d, want the added line", gotLine)
	}
}

func TestClickOnHunkHeaderReportsNoLine(t *testing.T) {
	eng, dv := newBoundView(t, func(dv *DiffView) { dv.SetDocument(parseFixture(t, "multihunk.diff")) })
	gotLine := 0
	dv.OnLineClick = func(_, line int) { gotLine = line }
	eng.SendMouseButton(40, 2, widget.MouseLeft, true)
	if gotLine != -1 {
		t.Fatalf("line = %d, want -1 for a hunk header", gotLine)
	}
}

func TestClickBelowTheLastRowIsIgnored(t *testing.T) {
	eng, dv := newBoundView(t, func(dv *DiffView) {
		dv.SetDocument(Document{Hunks: []Hunk{{Lines: []Line{{Kind: Context, OldNo: 1, NewNo: 1, Text: "a"}}}}})
	})
	called := false
	dv.OnLineClick = func(int, int) { called = true }
	eng.SendMouseButton(40, viewHeight-4, widget.MouseLeft, true)
	if called {
		t.Fatal("click below the content must be ignored")
	}
	if _, _, ok := dv.Selected(); ok {
		t.Fatal("click below the content must not select")
	}
}

func TestClickWithoutHandlerOnlySelects(t *testing.T) {
	_, dv := newBoundView(t, func(dv *DiffView) { dv.SetDocument(parseFixture(t, "simple.diff")) })
	dv.selectRow(2, false)
	dv.selectRow(2, false)
	if _, line, ok := dv.Selected(); !ok || line != 1 {
		t.Fatalf("selection = %d %v", line, ok)
	}
	dv.selectRow(99, false)
	if _, line, _ := dv.Selected(); line != 1 {
		t.Fatalf("out of range selection changed the row to %d", line)
	}
}

func TestSelectedIsResetWhenDocumentChanges(t *testing.T) {
	_, dv := newBoundView(t, func(dv *DiffView) { dv.SetDocument(parseFixture(t, "simple.diff")) })
	dv.selectRow(2, false)
	dv.SetDocument(parseFixture(t, "multihunk.diff"))
	if _, _, ok := dv.Selected(); ok {
		t.Fatal("a new document must clear the selection")
	}
}

func TestVerticalScrollbarThumbDrag(t *testing.T) {
	eng, dv := newBoundView(t, func(dv *DiffView) { dv.SetDocument(longDocument(200)) })
	if !dv.WantsCapture(widget.MouseEvent{X: viewWidth - 4, Y: 4, Button: widget.MouseLeft, Pressed: true}) {
		t.Fatal("thumb press must request capture")
	}
	eng.SendMouseButton(viewWidth-4, 4, widget.MouseLeft, true)
	eng.SendMouseMove(viewWidth-4, 60)
	_, y := dv.ScrollOffset()
	if y == 0 {
		t.Fatal("dragging the thumb must scroll")
	}
	eng.SendMouseButton(viewWidth-4, 60, widget.MouseLeft, false)
	eng.SendMouseMove(viewWidth-4, 120)
	if _, after := dv.ScrollOffset(); after != y {
		t.Fatalf("scroll after release = %d, want %d", after, y)
	}
}

func TestHorizontalScrollbarThumbDrag(t *testing.T) {
	eng, dv := newBoundView(t, func(dv *DiffView) { dv.SetDocument(longDocument(200)) })
	if !dv.WantsCapture(widget.MouseEvent{X: 4, Y: viewHeight - 4, Button: widget.MouseLeft, Pressed: true}) {
		t.Fatal("horizontal thumb press must request capture")
	}
	eng.SendMouseButton(4, viewHeight-4, widget.MouseLeft, true)
	eng.SendMouseMove(120, viewHeight-4)
	if x, _ := dv.ScrollOffset(); x == 0 {
		t.Fatal("dragging the horizontal thumb must scroll")
	}
	eng.SendMouseButton(120, viewHeight-4, widget.MouseLeft, false)
}

func TestScrollbarTrackClickJumps(t *testing.T) {
	eng, dv := newBoundView(t, func(dv *DiffView) { dv.SetDocument(longDocument(200)) })
	eng.SendMouseButton(viewWidth-4, viewHeight-40, widget.MouseLeft, true)
	_, y := dv.ScrollOffset()
	if y == 0 {
		t.Fatal("clicking the vertical track must scroll")
	}
	eng.SendMouseButton(viewWidth-4, viewHeight-40, widget.MouseLeft, false)
	eng.SendMouseButton(viewWidth-120, viewHeight-4, widget.MouseLeft, true)
	if x, _ := dv.ScrollOffset(); x == 0 {
		t.Fatal("clicking the horizontal track must scroll")
	}
}

func TestReleaseWithoutDragIsNotConsumed(t *testing.T) {
	_, dv := newBoundView(t, func(dv *DiffView) { dv.SetDocument(longDocument(40)) })
	if dv.OnMouseButton(widget.MouseEvent{Button: widget.MouseLeft, Pressed: false}) {
		t.Fatal("release without a drag must not be consumed")
	}
	dv.OnMouseMove(10, 10)
	if _, y := dv.ScrollOffset(); y != 0 {
		t.Fatalf("move without a drag scrolled to %d", y)
	}
}

func TestUnhandledButtonsAreIgnored(t *testing.T) {
	_, dv := newBoundView(t, func(dv *DiffView) { dv.SetDocument(longDocument(40)) })
	if dv.OnMouseButton(widget.MouseEvent{Button: widget.MouseRight, Pressed: true}) {
		t.Fatal("right button must not be consumed")
	}
	if dv.WantsCapture(widget.MouseEvent{Button: widget.MouseRight, Pressed: true}) {
		t.Fatal("right button must not request capture")
	}
	if dv.WantsCapture(widget.MouseEvent{Button: widget.MouseLeft}) {
		t.Fatal("release must not request capture")
	}
}

func TestDisabledViewIgnoresInput(t *testing.T) {
	_, dv := newBoundView(t, func(dv *DiffView) { dv.SetDocument(longDocument(40)) })
	dv.SetEnabled(false)
	if dv.OnMouseButton(widget.MouseEvent{X: 10, Y: 10, Button: widget.MouseWheelDown, Pressed: true}) {
		t.Fatal("disabled view must not consume mouse input")
	}
	dv.OnKeyEvent(widget.KeyEvent{Code: widget.KeyDown, Pressed: true})
	if _, y := dv.ScrollOffset(); y != 0 {
		t.Fatalf("disabled view scrolled to %d", y)
	}
}

func TestFocusStateIsTracked(t *testing.T) {
	dv := New()
	if dv.IsFocused() {
		t.Fatal("new view must not be focused")
	}
	dv.SetFocused(true)
	if !dv.IsFocused() {
		t.Fatal("SetFocused did not apply")
	}
}

func TestScrollIsClampedToTheContent(t *testing.T) {
	_, dv := newBoundView(t, func(dv *DiffView) { dv.SetDocument(parseFixture(t, "simple.diff")) })
	dv.ScrollBy(0, 10_000)
	dv.ScrollBy(-10_000, -10_000)
	if x, y := dv.ScrollOffset(); x != 0 || y != 0 {
		t.Fatalf("scroll = %d %d, want the content to fit", x, y)
	}
}

func TestCaptureManagerIsUsedForThumbDrag(t *testing.T) {
	_, dv := newBoundView(t, func(dv *DiffView) { dv.SetDocument(longDocument(200)) })
	manager := &countingCapture{}
	dv.SetCaptureManager(manager)
	dv.OnMouseButton(widget.MouseEvent{X: viewWidth - 4, Y: 4, Button: widget.MouseLeft, Pressed: true})
	dv.OnMouseButton(widget.MouseEvent{X: viewWidth - 4, Y: 4, Button: widget.MouseLeft})
	if manager.captured != 1 || manager.released != 1 {
		t.Fatalf("capture = %d, release = %d", manager.captured, manager.released)
	}
}

func TestDragWithoutCaptureManagerStillScrolls(t *testing.T) {
	_, dv := newBoundView(t, func(dv *DiffView) { dv.SetDocument(longDocument(200)) })
	dv.SetCaptureManager(nil)
	dv.OnMouseButton(widget.MouseEvent{X: viewWidth - 4, Y: 4, Button: widget.MouseLeft, Pressed: true})
	dv.OnMouseMove(viewWidth-4, 80)
	if _, y := dv.ScrollOffset(); y == 0 {
		t.Fatal("drag without a capture manager must still scroll")
	}
	if !dv.OnMouseButton(widget.MouseEvent{X: viewWidth - 4, Y: 80, Button: widget.MouseLeft}) {
		t.Fatal("release after a drag must be consumed")
	}
}

func TestMeasureCacheSurvivesOverflowAndFontChange(t *testing.T) {
	dv := New()
	for i := range measureCacheMax + 8 {
		dv.measure(strconv.Itoa(i), defaultFontSize)
	}
	first := dv.measure("0", defaultFontSize)
	if first != dv.measure("0", defaultFontSize) {
		t.Fatal("cached measurement changed")
	}
	if dv.measure("0", 20) == 0 {
		t.Fatal("measurement for a new size must be computed")
	}
}

func TestRunePrefixWidthCoversWholeText(t *testing.T) {
	dv := New()
	if dv.runePrefixWidth("abc", 0, defaultFontSize) != 0 {
		t.Fatal("empty prefix must be zero wide")
	}
	whole := dv.runePrefixWidth("abc", 3, defaultFontSize)
	if whole != dv.measure("abc", defaultFontSize) {
		t.Fatal("full prefix must equal the whole text")
	}
	if dv.runePrefixWidth("abc", 9, defaultFontSize) != whole {
		t.Fatal("prefix beyond the text must equal the whole text")
	}
	if dv.runePrefixWidth("абв", 2, defaultFontSize) == 0 {
		t.Fatal("multibyte prefix must be measured")
	}
}

func TestZeroSizedViewHasNoRowsToHit(t *testing.T) {
	dv := New()
	dv.SetDocument(longDocument(10))
	dv.SetRowHeight(0)
	dv.SetBounds(image.Rect(0, 0, viewWidth, viewHeight))
	g := dv.layout(dv.snapshot())
	if g.rowAt(10) != -1 {
		t.Fatal("zero row height must report no row")
	}
	if first, last := g.visibleRows(10); first != 0 || last != 0 {
		t.Fatalf("visible rows = %d %d", first, last)
	}
	if g.rowAt(-1000) != -1 {
		t.Fatal("a point above the content must report no row")
	}
}

func TestGeometryHelpersHandleDegenerateSizes(t *testing.T) {
	if thumbLength(40, 40, 0) != 40 {
		t.Fatal("empty content must fill the track")
	}
	if thumbOffset(40, 40, 100, 10, 0) != 0 {
		t.Fatal("no scroll range must keep the thumb at the start")
	}
	if scrollForPoint(10, 10, 10, 1000, 100) != 0 {
		t.Fatal("a track shorter than the thumb must not scroll")
	}
	if dragDelta(10, 10, 10, 1000, 100) != 0 {
		t.Fatal("a track shorter than the thumb must not drag")
	}
	if textColumnWidth(Unified, 4, 40) != 0 {
		t.Fatal("a narrow unified view must report a zero text column")
	}
}

func TestThumbRectanglesAreEmptyWithoutScrollbars(t *testing.T) {
	_, dv := newBoundView(t, func(dv *DiffView) {
		dv.SetDocument(Document{Hunks: []Hunk{{Lines: []Line{{Kind: Context, OldNo: 1, NewNo: 1, Text: "a"}}}}})
	})
	g := dv.layout(dv.snapshot())
	if g.hasV || g.hasH {
		t.Fatal("a fitting document must not need scrollbars")
	}
	if !g.vThumb().Empty() || !g.hThumb().Empty() {
		t.Fatal("a fitting document must have no scrollbar thumbs")
	}
}

func TestUnifiedSignPerKind(t *testing.T) {
	cases := map[Kind]string{Added: "+", Removed: "-", NoNewline: `\`, Context: " ", HunkHeader: " "}
	for kind, want := range cases {
		if got := unifiedSign(kind); got != want {
			t.Fatalf("%v sign = %q, want %q", kind, got, want)
		}
	}
}

func TestCellColorsPerKind(t *testing.T) {
	p := DarkPalette()
	cases := []struct {
		name   string
		c      cell
		bg     color.RGBA
		gutter color.RGBA
		text   color.RGBA
	}{
		{name: "placeholder", c: cell{}, bg: p.PlaceholderBG, gutter: p.PlaceholderBG, text: p.Text},
		{name: "added", c: cell{filled: true, kind: Added}, bg: p.AddedBG, gutter: p.AddedGutterBG, text: p.Text},
		{name: "removed", c: cell{filled: true, kind: Removed}, bg: p.RemovedBG, gutter: p.RemovedGutterBG, text: p.Text},
		{name: "context", c: cell{filled: true, kind: Context}, bg: p.Background, gutter: p.GutterBG, text: p.Text},
		{name: "no newline", c: cell{filled: true, kind: NoNewline}, bg: p.Background, gutter: p.GutterBG, text: p.NoNewlineText},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if cellBackground(p, tc.c) != tc.bg {
				t.Fatalf("background = %v", cellBackground(p, tc.c))
			}
			if gutterBackground(p, tc.c) != tc.gutter {
				t.Fatalf("gutter = %v", gutterBackground(p, tc.c))
			}
			if cellText(p, tc.c) != tc.text {
				t.Fatalf("text = %v", cellText(p, tc.c))
			}
		})
	}
}

type countingCapture struct {
	captured int
	released int
}

func (c *countingCapture) SetCapture(widget.Widget) { c.captured++ }

func (c *countingCapture) ReleaseCapture() { c.released++ }

type recordingContext struct {
	widget.DrawContext
}
