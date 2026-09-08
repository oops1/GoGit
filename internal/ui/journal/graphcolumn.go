package journal

import (
	"image"
	"image/color"

	"github.com/oops1/headless-gui/v3/widget/datagrid"

	"github.com/oops1/gogit/internal/ui/journal/graph"
)

const (
	graphColumnIndex = 0
	graphLaneWidth   = 14
	graphLeftPadding = 10
	graphDotRadius   = 4
	graphLineWidth   = 2
	graphCurveSteps  = 8
	graphOverflowGap = 4
	graphMinWidth    = 28
	graphMaxWidth    = 240
)

func (v *View) newGraphColumn() datagrid.Column {
	return datagrid.NewTemplateColumn("", v.drawGraphCell)
}

func graphColumnWidth(lanes int) int {
	width := graphLeftPadding + lanes*graphLaneWidth
	return min(max(width, graphMinWidth), graphMaxWidth)
}

func laneColor(index int) color.RGBA {
	return badgePalette[index%len(badgePalette)]
}

func (v *View) drawGraphCell(cdc datagrid.CellDrawContext) {
	row, ok := cdc.Item.(Row)
	if !ok {
		return
	}
	g := row.Graph
	top := cdc.Rect.Min.Y
	bottom := cdc.Rect.Max.Y
	middle := top + cdc.Rect.Dy()/2

	for _, segment := range g.Through {
		drawVertical(cdc, laneX(cdc.Rect, segment.Lane), top, bottom, laneColor(segment.Color))
	}
	own := laneX(cdc.Rect, g.Lane)
	if g.FromAbove {
		drawVertical(cdc, own, top, middle, laneColor(g.Color))
	}
	for _, segment := range g.Out {
		drawEdge(cdc, own, middle, laneX(cdc.Rect, segment.Lane), bottom, laneColor(segment.Color))
	}
	drawDot(cdc, own, middle, g)
	if g.Overflow {
		drawOverflow(cdc, g)
	}
}

func laneX(rect image.Rectangle, lane int) int {
	return rect.Min.X + graphLeftPadding + lane*graphLaneWidth
}

func drawVertical(cdc datagrid.CellDrawContext, x, top, bottom int, col color.RGBA) {
	cdc.DrawCtx.FillRect(x-graphLineWidth/2, top, graphLineWidth, bottom-top, col)
}

func drawEdge(cdc datagrid.CellDrawContext, fromX, fromY, toX, toY int, col color.RGBA) {
	if fromX == toX {
		drawVertical(cdc, fromX, fromY, toY, col)
		return
	}
	cdc.DrawCtx.StrokePolylineAA(curve(fromX, fromY, toX, toY), float64(graphLineWidth), false, col)
}

func curve(fromX, fromY, toX, toY int) []image.Point {
	lean := float64(toY-fromY) / 2
	points := make([]image.Point, 0, graphCurveSteps+1)
	for step := 0; step <= graphCurveSteps; step++ {
		t := float64(step) / graphCurveSteps
		points = append(points, image.Pt(
			bend(float64(fromX), float64(fromX), float64(toX), float64(toX), t),
			bend(float64(fromY), float64(fromY)+lean, float64(toY)-lean, float64(toY), t),
		))
	}
	return points
}

func bend(from, lead, trail, to, t float64) int {
	rest := 1 - t
	return int(rest*rest*rest*from + 3*rest*rest*t*lead + 3*rest*t*t*trail + t*t*t*to + 0.5)
}

func drawDot(cdc datagrid.CellDrawContext, x, y int, g graph.Row) {
	radius := graphDotRadius
	col := laneColor(g.Color)
	if g.Merge {
		cdc.DrawCtx.StrokeEllipseAA(x, y, radius, radius, float64(graphLineWidth), col)
		return
	}
	cdc.DrawCtx.FillEllipseAA(x, y, radius, radius, col)
}

func drawOverflow(cdc datagrid.CellDrawContext, g graph.Row) {
	x := laneX(cdc.Rect, g.Lanes-1) + graphOverflowGap
	y := cdc.Rect.Min.Y + cdc.Rect.Dy()/2
	for dot := range 3 {
		cdc.DrawCtx.FillEllipseAA(x+dot*3, y, 1, 1, laneColor(g.Color))
	}
}
