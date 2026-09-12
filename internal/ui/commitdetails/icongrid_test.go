package commitdetails

import (
	"image"
	"image/color"
	"testing"

	"github.com/oops1/headless-gui/v3/engine"
	"github.com/oops1/headless-gui/v3/widget"
	"github.com/oops1/headless-gui/v3/widget/datagrid"

	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/style"
)

func renderChanges(t *testing.T, v *View) {
	t.Helper()
	eng := engine.New(500, 300, 30)
	t.Cleanup(eng.Stop)
	v.changes.SetBounds(image.Rect(0, 0, 500, 300))
	eng.SetRoot(v.changes)
	_ = eng.RenderOnce()
}

func TestEveryChangedFileGetsTheIconOfItsState(t *testing.T) {
	v := newTestView(t)
	v.Show(details())

	renderChanges(t, v)

	if got := v.changes.queued(); got != 2 {
		t.Fatalf("icons = %d, want one per changed file", got)
	}
}

func TestAStateWithoutAnIconLeavesJustThePath(t *testing.T) {
	v := newTestView(t)
	model := details()
	model.Changes = []Change{{Status: "no such state", Path: "f"}}
	v.Show(model)

	renderChanges(t, v)

	if got := v.changes.queued(); got != 0 {
		t.Fatalf("icons = %d, want none for an unknown state", got)
	}
}

func TestACellThatIsNotAChangeIsLeftAlone(t *testing.T) {
	v := newTestView(t)

	v.changes.drawPathCell(datagrid.CellDrawContext{Item: "not a change"})
	v.changes.drawLinesCell(datagrid.CellDrawContext{Item: "not a change"})

	if got := v.changes.queued(); got != 0 {
		t.Fatalf("icons = %d", got)
	}
}

func TestAddedLinesAreGreenDeletedRedAndZeroGrey(t *testing.T) {
	newTestView(t)
	grey := color.RGBA{R: 128, G: 128, B: 128, A: 255}
	green := color.RGBA{G: 200, A: 255}
	red := color.RGBA{R: 200, A: 255}

	parts := lineParts(3, 0, grey, green, red)

	if len(parts) != 2 {
		t.Fatalf("parts = %+v", parts)
	}
	if parts[0].text != i18n.Tf("Details.Lines.Added", 3) || parts[0].color != green {
		t.Fatalf("added = %+v", parts[0])
	}
	if parts[1].text != i18n.Tf("Details.Lines.Deleted", 0) || parts[1].color != grey {
		t.Fatalf("deleted = %+v", parts[1])
	}
	if parts := lineParts(0, 2, grey, green, red); parts[0].color != grey || parts[1].color != red {
		t.Fatalf("parts = %+v", parts)
	}
}

func TestAFileWithNoLineChangesShowsNoCounts(t *testing.T) {
	if parts := lineParts(0, 0, color.RGBA{}, color.RGBA{}, color.RGBA{}); parts != nil {
		t.Fatalf("parts = %+v", parts)
	}
}

func TestTheCountsTakeTheirColoursFromTheTheme(t *testing.T) {
	v := newTestView(t)
	dark := widget.Win11DarkTheme()

	v.Restyle(dark)

	v.changes.mu.Lock()
	defer v.changes.mu.Unlock()
	if v.changes.added != style.Of(dark).Added() || v.changes.deleted != style.Of(dark).Deleted() {
		t.Fatalf("colours = %v %v", v.changes.added, v.changes.deleted)
	}
}

func renderFiles(t *testing.T, v *View) {
	t.Helper()
	eng := engine.New(500, 300, 30)
	t.Cleanup(eng.Stop)
	v.files.SetBounds(image.Rect(0, 0, 500, 300))
	eng.SetRoot(v.files)
	_ = eng.RenderOnce()
}

func TestTheFilesListDrawsFoldersGreyerThanNames(t *testing.T) {
	v := newTestView(t)
	v.Show(details())

	renderFiles(t, v)

	if got := v.files.mutedColor(); got != widget.CurrentTheme().SecondaryText {
		t.Fatalf("folder colour = %v, want the secondary text colour", got)
	}
}

func TestAFileCellThatIsNotAFileIsLeftAlone(t *testing.T) {
	v := newTestView(t)

	v.files.drawFileCell(datagrid.CellDrawContext{Item: "not a file"})

	if got := v.files.queued(); got != 0 {
		t.Fatalf("icons = %d", got)
	}
}

func TestTheFolderColourFollowsTheTheme(t *testing.T) {
	v := newTestView(t)
	dark := widget.Win11DarkTheme()

	v.Restyle(dark)

	if v.files.mutedColor() != dark.SecondaryText || v.changes.mutedColor() != dark.SecondaryText {
		t.Fatalf("folder colours = %v %v", v.files.mutedColor(), v.changes.mutedColor())
	}
}
