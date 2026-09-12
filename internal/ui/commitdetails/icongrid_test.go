package commitdetails

import (
	"image"
	"testing"

	"github.com/oops1/headless-gui/v3/engine"
	"github.com/oops1/headless-gui/v3/widget/datagrid"
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

	if got := v.changes.queued(); got != 0 {
		t.Fatalf("icons = %d", got)
	}
}
