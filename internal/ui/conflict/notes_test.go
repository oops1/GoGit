package conflict

import (
	"image"
	"image/color"
	"testing"

	"github.com/oops1/headless-gui/v3/engine"
	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/i18n"
)

func TestEachPanelIsExplainedBetweenItsHeaderAndItsCode(t *testing.T) {
	v := shown(t)

	want := [3]string{i18n.T("Dialog.Conflict.Note.Ours"), i18n.T("Dialog.Conflict.Note.Base"), i18n.T("Dialog.Conflict.Note.Theirs")}
	if got := v.notes.Texts(); got != want {
		t.Fatalf("notes = %q, want %q", got, want)
	}
	v.Merge().SetBounds(image.Rect(100, 50, 1100, 650))
	if got, want := v.notes.Bounds(), image.Rect(114, 102, 1068, 114); got != want {
		t.Fatalf("strip = %v, want %v under the headers and above the code", got, want)
	}
}

func TestTheNotesSitInTheSameCellAsTheMergeView(t *testing.T) {
	v := newTestView(t)

	if v.notes.GetGridRow() != v.Merge().GetGridRow() || v.notes.GetGridColumn() != v.Merge().GetGridColumn() {
		t.Fatalf("notes cell = %d,%d, merge cell = %d,%d", v.notes.GetGridRow(), v.notes.GetGridColumn(), v.Merge().GetGridRow(), v.Merge().GetGridColumn())
	}
}

func TestAMergeViewWithoutRoomHasNoNotes(t *testing.T) {
	notes := newPaneNotes(widget.NewMergeView("", ""), "a", "b", "c")

	if !notes.Bounds().Empty() {
		t.Fatalf("strip = %v, want nothing before the merge view is placed", notes.Bounds())
	}
	eng := engine.New(200, 100, 30)
	t.Cleanup(eng.Stop)
	eng.SetRoot(notes)
	_ = eng.RenderOnce()
}

func TestTheNotesFollowThePanesWithAndWithoutTheBase(t *testing.T) {
	strip := image.Rect(0, 0, 320, 12)

	if got, want := paneSpans(strip, true), [3][2]int{{0, 100}, {110, 210}, {220, 320}}; got != want {
		t.Fatalf("three panes = %v, want %v", got, want)
	}
	if got, want := paneSpans(strip, false), [3][2]int{{0, 155}, {}, {165, 320}}; got != want {
		t.Fatalf("two panes = %v, want %v", got, want)
	}
}

func TestTheNotesDrawOnlyWhereThereIsAPaneAndAText(t *testing.T) {
	merge := widget.NewMergeView("", "")
	notes := newPaneNotes(merge, "ours", "", "theirs")
	eng := engine.New(1000, 600, 30)
	t.Cleanup(eng.Stop)
	eng.SetRoot(notes)
	merge.SetBounds(image.Rect(0, 0, 1000, 600))

	_ = eng.RenderOnce()
	merge.SetShowBase(false)
	_ = eng.RenderOnce()
}

func TestTheNotesTakeTheHintColourOnTheGroundOfTheMergeView(t *testing.T) {
	notes := newPaneNotes(widget.NewMergeView("", ""), "a", "b", "c")
	dark := widget.Win11DarkTheme()

	notes.Restyle(dark)

	notes.mu.Lock()
	defer notes.mu.Unlock()
	if notes.color != dark.SecondaryText || notes.bg != mergeBackground(dark) {
		t.Fatalf("colour = %v, ground = %v", notes.color, notes.bg)
	}
}

func TestTheGroundUnderTheNotesMatchesTheMergeView(t *testing.T) {
	white := color.RGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF}
	shadedWhite := color.RGBA{R: 244, G: 244, B: 244, A: 0xFF}
	window := color.RGBA{R: 0xEE, G: 0xEE, B: 0xEE, A: 0xFF}
	panel := color.RGBA{R: 0xDD, G: 0xDD, B: 0xDD, A: 0xFF}

	for _, tt := range []struct {
		name  string
		theme widget.Theme
		want  color.RGBA
	}{
		{"the window colour", widget.Theme{InputBG: white, WindowBG: window}, window},
		{"no window colour shades the card", widget.Theme{InputBG: white}, shadedWhite},
		{"a window as light as the card is shaded", widget.Theme{InputBG: white, WindowBG: white}, shadedWhite},
		{"the panel stands in for a missing field", widget.Theme{PanelBG: panel, WindowBG: window}, window},
		{"nothing at all falls back to white", widget.Theme{}, shadedWhite},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := mergeBackground(&tt.theme); got != tt.want {
				t.Fatalf("ground = %v, want %v", got, tt.want)
			}
		})
	}
}
