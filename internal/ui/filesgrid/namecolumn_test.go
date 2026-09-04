package filesgrid

import (
	"testing"

	"github.com/oops1/headless-gui/v3/engine"
	"github.com/oops1/headless-gui/v3/widget/datagrid"

	"github.com/oops1/gogit/internal/ui/changes"
)

func TestDrawQueuesAnIconForARowWithAKnownStatus(t *testing.T) {
	_, g := newBoundRowGrid(t, []changes.Row{{Name: "main.go", Status: changes.RowModified, State: "Modified"}})
	if len(g.pendingIcons) != 1 {
		t.Fatalf("pendingIcons = %d, want 1", len(g.pendingIcons))
	}
}

func TestDrawSkipsTheIconForARowWithNoKnownStatus(t *testing.T) {
	_, g := newBoundRowGrid(t, []changes.Row{{Name: "marker"}})
	if len(g.pendingIcons) != 0 {
		t.Fatalf("pendingIcons = %d, want 0", len(g.pendingIcons))
	}
}

func TestDrawClearsStalePendingIconsOnTheNextFrameWithoutRows(t *testing.T) {
	eng, g := newBoundRowGrid(t, []changes.Row{{Name: "main.go", Status: changes.RowModified}})
	if len(g.pendingIcons) != 1 {
		t.Fatalf("pendingIcons = %d, want 1", len(g.pendingIcons))
	}
	oc := g.Data().Grid.ItemsSource()
	oc.Clear()
	eng.RenderOnce()
	if len(g.pendingIcons) != 0 {
		t.Fatalf("pendingIcons = %d, want 0 after clearing the rows", len(g.pendingIcons))
	}
}

type namedItem struct{ Name string }

func TestDrawFallsBackToReflectionForNonRowItems(t *testing.T) {
	eng := engine.New(gridWidth, gridHeight, 30)
	t.Cleanup(eng.Stop)
	g := New()
	oc := datagrid.NewObservableCollection()
	oc.Add(namedItem{Name: "plain.txt"})
	g.SetItemsSource(oc)
	eng.SetRoot(g)
	eng.RenderOnce()
	if len(g.pendingIcons) != 0 {
		t.Fatalf("pendingIcons = %d, want 0 for a non-Row item", len(g.pendingIcons))
	}
}
