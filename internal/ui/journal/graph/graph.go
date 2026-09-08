package graph

import (
	"github.com/oops1/gogit/internal/gitcore/hash"
)

const (
	Colors   = 10
	MaxLanes = 16
)

type Commit struct {
	ID      hash.ObjectID
	Parents []hash.ObjectID
}

type Segment struct {
	Lane  int
	Color int
}

type Row struct {
	Lane      int
	Color     int
	Merge     bool
	Lanes     int
	Through   []Segment
	Out       []Segment
	FromAbove bool
	Overflow  bool
}

type lane struct {
	expect hash.ObjectID
	color  int
}

type Layout struct {
	lanes []lane
	limit int
	next  int
}

func New() *Layout {
	return &Layout{limit: MaxLanes}
}

func (l *Layout) SetLimit(lanes int) {
	if lanes > 0 {
		l.limit = lanes
	}
}

func (l *Layout) Reset() {
	l.lanes = nil
	l.next = 0
}

func (l *Layout) Add(c Commit) Row {
	at, waiting := l.laneExpecting(c.ID)
	before := l.width()
	own, color := l.claim(at, waiting)

	row := Row{Lane: own, Color: color, Merge: len(c.Parents) > 1, FromAbove: waiting}
	row.Through = l.passing(own)
	l.lanes[own] = lane{color: color}
	row.Out = l.placeParents(c.Parents, own, color)
	l.trim()

	row.Lanes = widest(before, l.width(), own+1, row.Through, row.Out)
	return l.clamp(row)
}

func (l *Layout) claim(at int, waiting bool) (int, int) {
	if waiting {
		return at, l.lanes[at].color
	}
	own := l.free()
	color := l.newColor()
	l.lanes[own] = lane{color: color}
	return own, color
}

func (l *Layout) passing(own int) []Segment {
	var out []Segment
	for i, ln := range l.lanes {
		if i == own || ln.expect.IsZero() {
			continue
		}
		out = append(out, Segment{Lane: i, Color: ln.color})
	}
	return out
}

func (l *Layout) placeParents(parents []hash.ObjectID, own, color int) []Segment {
	if len(parents) == 0 {
		l.lanes[own] = lane{}
		return nil
	}
	out := make([]Segment, 0, len(parents))
	for i, parent := range parents {
		out = append(out, l.placeParent(parent, own, color, i == 0))
	}
	return out
}

func (l *Layout) placeParent(parent hash.ObjectID, own, color int, first bool) Segment {
	if at, taken := l.laneExpecting(parent); taken {
		if first && own < at {
			l.lanes[own] = lane{expect: parent, color: color}
			l.lanes[at] = lane{}
			return Segment{Lane: own, Color: color}
		}
		if first {
			l.lanes[own] = lane{}
		}
		return Segment{Lane: at, Color: l.lanes[at].color}
	}
	if first {
		l.lanes[own] = lane{expect: parent, color: color}
		return Segment{Lane: own, Color: color}
	}
	at := l.free()
	fresh := l.newColor()
	l.lanes[at] = lane{expect: parent, color: fresh}
	return Segment{Lane: at, Color: fresh}
}

func (l *Layout) laneExpecting(id hash.ObjectID) (int, bool) {
	for i, ln := range l.lanes {
		if ln.expect == id {
			return i, true
		}
	}
	return 0, false
}

func (l *Layout) free() int {
	for i, ln := range l.lanes {
		if ln.expect.IsZero() {
			return i
		}
	}
	l.lanes = append(l.lanes, lane{})
	return len(l.lanes) - 1
}

func (l *Layout) newColor() int {
	color := l.next % Colors
	l.next++
	return color
}

func (l *Layout) width() int {
	last := -1
	for i, ln := range l.lanes {
		if !ln.expect.IsZero() {
			last = i
		}
	}
	return last + 1
}

func (l *Layout) trim() {
	for len(l.lanes) > 0 && l.lanes[len(l.lanes)-1].expect.IsZero() {
		l.lanes = l.lanes[:len(l.lanes)-1]
	}
}

func (l *Layout) clamp(row Row) Row {
	edge := l.limit - 1
	row.Overflow = row.Lanes > l.limit
	if row.Overflow {
		row.Lanes = l.limit
	}
	row.Lane = min(row.Lane, edge)
	row.Through = clampSegments(row.Through, edge)
	row.Out = clampSegments(row.Out, edge)
	return row
}

func clampSegments(segments []Segment, edge int) []Segment {
	for i := range segments {
		segments[i].Lane = min(segments[i].Lane, edge)
	}
	return segments
}

func widest(before, after, own int, groups ...[]Segment) int {
	widest := max(before, after, own)
	for _, group := range groups {
		for _, segment := range group {
			widest = max(widest, segment.Lane+1)
		}
	}
	return widest
}
