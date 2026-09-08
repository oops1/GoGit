package journal

import (
	"image"
	"image/color"
	"testing"

	"github.com/oops1/headless-gui/v3/widget/datagrid"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/ui/journal/graph"
)

type drawnLine struct {
	x1, y1, x2, y2 int
	color          color.RGBA
}

type drawnDot struct {
	x, y, radius int
	filled       bool
	color        color.RGBA
}

type recordingGraphCtx struct {
	recordingDrawCtx
	lines     []drawnLine
	polylines [][]image.Point
	dots      []drawnDot
}

func (c *recordingGraphCtx) FillRect(x, y, w, h int, col color.RGBA) {
	c.lines = append(c.lines, drawnLine{x + w/2, y, x + w/2, y + h, col})
}

func (c *recordingGraphCtx) DrawLineAA(x1, y1, x2, y2 int, thickness float64, col color.RGBA) {
	c.lines = append(c.lines, drawnLine{x1, y1, x2, y2, col})
}

func (c *recordingGraphCtx) StrokePolylineAA(pts []image.Point, thickness float64, closed bool, col color.RGBA) {
	c.polylines = append(c.polylines, pts)
}

func (c *recordingGraphCtx) FillEllipseAA(cx, cy, rx, ry int, col color.RGBA) {
	c.dots = append(c.dots, drawnDot{cx, cy, rx, true, col})
}

func (c *recordingGraphCtx) StrokeEllipseAA(cx, cy, rx, ry int, thickness float64, col color.RGBA) {
	c.dots = append(c.dots, drawnDot{cx, cy, rx, false, col})
}

func graphCell(row Row, dc datagrid.DrawContextBridge) datagrid.CellDrawContext {
	return datagrid.CellDrawContext{
		Rect:      image.Rect(0, 0, 60, 20),
		Item:      row,
		DrawCtx:   dc,
		TextColor: color.RGBA{A: 0xFF},
		FontSize:  9,
	}
}

func TestTheGraphCellDrawsNothingForAnythingButARow(t *testing.T) {
	dc := &recordingGraphCtx{}

	NewView().drawGraphCell(datagrid.CellDrawContext{Rect: image.Rect(0, 0, 60, 20), Item: "not a row", DrawCtx: dc})

	if len(dc.lines) != 0 || len(dc.dots) != 0 {
		t.Fatal("a cell that holds no commit must stay empty")
	}
}

func TestACommitIsDrawnAsAFilledDotOnItsLine(t *testing.T) {
	dc := &recordingGraphCtx{}
	row := Row{Graph: graph.Row{Lane: 0, Lanes: 1, FromAbove: true, Out: []graph.Segment{{Lane: 0}}}}

	NewView().drawGraphCell(graphCell(row, dc))

	if len(dc.dots) != 1 || !dc.dots[0].filled {
		t.Fatalf("dots = %+v, want one filled dot", dc.dots)
	}
	if len(dc.lines) != 2 {
		t.Fatalf("lines = %+v, want the line above and below the dot", dc.lines)
	}
	if dc.lines[0].y1 != 0 || dc.lines[0].y2 != 10 {
		t.Fatalf("line above = %+v, want it from the top edge to the dot", dc.lines[0])
	}
	if dc.lines[1].y1 != 10 || dc.lines[1].y2 != 20 {
		t.Fatalf("line below = %+v, want it from the dot to the bottom edge", dc.lines[1])
	}
}

func TestAMergeIsDrawnAsARing(t *testing.T) {
	dc := &recordingGraphCtx{}
	row := Row{Graph: graph.Row{Lane: 0, Lanes: 2, Merge: true, Out: []graph.Segment{{Lane: 0}, {Lane: 1, Color: 1}}}}

	NewView().drawGraphCell(graphCell(row, dc))

	if len(dc.dots) != 1 || dc.dots[0].filled {
		t.Fatalf("dots = %+v, want one ring", dc.dots)
	}
}

func TestALineThatChangesLaneIsDrawnAsACurve(t *testing.T) {
	dc := &recordingGraphCtx{}
	row := Row{Graph: graph.Row{Lane: 1, Lanes: 2, Color: 1, Out: []graph.Segment{{Lane: 0}}}}

	NewView().drawGraphCell(graphCell(row, dc))

	if len(dc.polylines) != 1 {
		t.Fatalf("polylines = %d, want the curve into the other lane", len(dc.polylines))
	}
	curve := dc.polylines[0]
	if curve[0].X != laneX(image.Rect(0, 0, 60, 20), 1) || curve[len(curve)-1].X != laneX(image.Rect(0, 0, 60, 20), 0) {
		t.Fatalf("curve = %v, want it to run from the dot to the other lane", curve)
	}
	if curve[0].Y != 10 || curve[len(curve)-1].Y != 20 {
		t.Fatalf("curve = %v, want it to run from the middle to the bottom edge", curve)
	}
	if len(dc.lines) != 0 {
		t.Fatalf("lines = %+v, want the curve alone", dc.lines)
	}
}

func TestALaneThatPassesTheRowIsDrawnStraightThrough(t *testing.T) {
	dc := &recordingGraphCtx{}
	row := Row{Graph: graph.Row{Lane: 0, Lanes: 2, Through: []graph.Segment{{Lane: 1, Color: 2}}}}

	NewView().drawGraphCell(graphCell(row, dc))

	if len(dc.lines) != 1 {
		t.Fatalf("lines = %+v, want the passing lane alone", dc.lines)
	}
	line := dc.lines[0]
	if line.y1 != 0 || line.y2 != 20 || line.x1 != line.x2 {
		t.Fatalf("line = %+v, want it straight through the row", line)
	}
	if line.color != laneColor(2) {
		t.Fatalf("colour = %v, want the colour of the lane", line.color)
	}
}

func TestLanesBeyondTheLimitAreMarkedWithDots(t *testing.T) {
	dc := &recordingGraphCtx{}
	row := Row{Graph: graph.Row{Lane: 0, Lanes: 3, Overflow: true}}

	NewView().drawGraphCell(graphCell(row, dc))

	if len(dc.dots) != 4 {
		t.Fatalf("dots = %+v, want the commit and three marks", dc.dots)
	}
}

func TestTheGraphColumnGrowsWithTheLanesAndStops(t *testing.T) {
	if graphColumnWidth(0) != graphMinWidth {
		t.Fatalf("width = %d, want the smallest column", graphColumnWidth(0))
	}
	if graphColumnWidth(3) <= graphColumnWidth(2) {
		t.Fatal("a wider graph must take a wider column")
	}
	if graphColumnWidth(100) != graphMaxWidth {
		t.Fatalf("width = %d, want the column to stop growing", graphColumnWidth(100))
	}
}

func TestTheGraphColumnIsATemplateColumnWithoutAHeader(t *testing.T) {
	col := NewView().newGraphColumn()

	if _, ok := col.(*datagrid.DataGridTemplateColumn); !ok {
		t.Fatalf("column type = %T, want a template column", col)
	}
	if col.Header() != "" {
		t.Fatalf("header = %q, want none", col.Header())
	}
}

func TestTheColumnWidensAsTheHistoryBranches(t *testing.T) {
	v, grid := bound(t)
	grid.Grid.SetColumns(journalColumns())
	v.installGraphColumn()
	narrow := grid.Grid.Columns()[graphColumnIndex].Width().Value

	v.Append([]Row{
		{ID: idFor(1), Parents: []hash.ObjectID{idFor(2), idFor(3)}},
		{ID: idFor(2), Parents: []hash.ObjectID{idFor(4)}},
		{ID: idFor(3), Parents: []hash.ObjectID{idFor(4)}},
	})

	if grid.Grid.Columns()[graphColumnIndex].Width().Value == narrow {
		t.Fatal("a branching history must widen the graph column")
	}

	v.Reset()

	if grid.Grid.Columns()[graphColumnIndex].Width().Value != narrow {
		t.Fatal("an empty journal must give the width back")
	}
}

func TestACommitThatIsOnlyLocalIsDrawnInItsOwnColour(t *testing.T) {
	dc := &recordingGraphCtx{}
	row := Row{Unpushed: true, Graph: graph.Row{Lane: 0, Lanes: 1, FromAbove: true, Out: []graph.Segment{{Lane: 0}}}}

	NewView().drawGraphCell(graphCell(row, dc))

	if dc.dots[0].color != unpushedColor {
		t.Fatalf("dot colour = %v, want the local-only colour", dc.dots[0].color)
	}
	for _, line := range dc.lines {
		if line.color != unpushedColor {
			t.Fatalf("line colour = %v, want the local-only colour", line.color)
		}
	}
}

func TestALaneThatMovesIsDrawnAsACurveAcrossTheRow(t *testing.T) {
	dc := &recordingGraphCtx{}
	row := Row{Graph: graph.Row{
		Lane:  0,
		Lanes: 2,
		Moves: []graph.Move{{From: 1, To: 0, Color: 1}},
	}}

	NewView().drawGraphCell(graphCell(row, dc))

	if len(dc.polylines) != 1 {
		t.Fatalf("polylines = %d, want the moved lane drawn as a curve", len(dc.polylines))
	}
	curve := dc.polylines[0]
	if curve[0].Y != 0 || curve[len(curve)-1].Y != 20 {
		t.Fatalf("curve = %v, want it to cross the whole row", curve)
	}
}
