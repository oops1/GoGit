package diffview

import (
	"image"
	"testing"

	"github.com/oops1/headless-gui/v3/widget"
)

func headerLineDocument() Document {
	return Document{Hunks: []Hunk{{Lines: []Line{
		{Kind: HunkHeader, Text: "@@ inline header @@"},
		{Kind: Context, OldNo: 1, NewNo: 1, Text: "context"},
	}}}}
}

func TestHunkHeaderLineBecomesAHeaderRowInBothModes(t *testing.T) {
	for _, mode := range []Mode{SideBySide, Unified} {
		set := buildRows(headerLineDocument(), mode)
		if len(set.rows) != 2 {
			t.Fatalf("%v rows = %d", mode, len(set.rows))
		}
		if !set.rows[0].header || set.rows[0].text != "@@ inline header @@" {
			t.Fatalf("%v first row = %+v", mode, set.rows[0])
		}
		if set.rows[0].lineFor(false) != -1 {
			t.Fatalf("%v header row must report no line", mode)
		}
	}
}

func TestNoNewlineMarkerFollowsTheAnnotatedSide(t *testing.T) {
	cases := []struct {
		name       string
		previous   Line
		wantLeft   bool
		wantRight  bool
		annotation Line
	}{
		{name: "after a removal", previous: Line{Kind: Removed, OldNo: 1, Text: "old"}, wantLeft: true},
		{name: "after an addition", previous: Line{Kind: Added, NewNo: 1, Text: "new"}, wantRight: true},
		{name: "after context", previous: Line{Kind: Context, OldNo: 1, NewNo: 1, Text: "same"},
			wantLeft: true, wantRight: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			doc := Document{Hunks: []Hunk{{Lines: []Line{c.previous, {Kind: NoNewline, Text: "No newline"}}}}}
			set := buildRows(doc, SideBySide)
			marker := set.rows[len(set.rows)-1]
			if marker.left.filled != c.wantLeft || marker.right.filled != c.wantRight {
				t.Fatalf("marker = left %v, right %v", marker.left.filled, marker.right.filled)
			}
		})
	}
}

func TestNoNewlineMarkerWithoutAPreviousLineSpansBothSides(t *testing.T) {
	doc := Document{Hunks: []Hunk{{Lines: []Line{
		{Kind: NoNewline, Text: "No newline"},
		{Kind: NoNewline, Text: "No newline"},
	}}}}
	set := buildRows(doc, SideBySide)
	if !set.rows[0].left.filled || !set.rows[0].right.filled {
		t.Fatalf("marker = %+v", set.rows[0])
	}
}

func TestLineForFallsBackToTheOppositeSide(t *testing.T) {
	doc := Document{Hunks: []Hunk{{Lines: []Line{
		{Kind: Removed, OldNo: 1, Text: "gone"},
		{Kind: Context, OldNo: 2, NewNo: 1, Text: "same"},
		{Kind: Added, NewNo: 2, Text: "fresh"},
	}}}}
	set := buildRows(doc, SideBySide)
	if got := set.rows[0].lineFor(true); got != 0 {
		t.Fatalf("right side of a removal-only row = %d", got)
	}
	if got := set.rows[2].lineFor(false); got != 2 {
		t.Fatalf("left side of an addition-only row = %d", got)
	}
}

func TestDigitsOfGrowsWithTheNumber(t *testing.T) {
	cases := map[int]int{0: 1, 9: 1, 10: 2, 999: 3, 1000: 4}
	for value, want := range cases {
		if got := digitsOf(value); got != want {
			t.Fatalf("digitsOf(%d) = %d, want %d", value, got, want)
		}
	}
}

func TestWidestRowCountsTheUnifiedSign(t *testing.T) {
	doc := Document{Hunks: []Hunk{{Header: "@@", Lines: []Line{{Kind: Added, NewNo: 1, Text: "abcd"}}}}}
	if got := buildRows(doc, Unified).maxRunes; got != 5 {
		t.Fatalf("unified width = %d, want the text plus its sign", got)
	}
	if got := buildRows(doc, SideBySide).maxRunes; got != 4 {
		t.Fatalf("side by side width = %d", got)
	}
}

func TestRowAtRejectsPointsAboveTheContent(t *testing.T) {
	dv := New()
	dv.SetDocument(longDocument(10))
	dv.SetBounds(image.Rect(0, 100, 200, 300))
	g := dv.layout(dv.snapshot())
	if got := g.rowAt(0); got != -1 {
		t.Fatalf("row above the content = %d", got)
	}
}

func TestClickOutsideEveryRegionIsIgnored(t *testing.T) {
	dv := New()
	dv.SetDocument(longDocument(60))
	dv.SetBounds(image.Rect(100, 100, 300, 200))
	if dv.OnMouseButton(widget.MouseEvent{X: 10, Y: 10, Button: widget.MouseLeft, Pressed: true}) {
		t.Fatal("a click outside the widget must not be consumed")
	}
}

func TestEmptySpansAreNotPainted(t *testing.T) {
	doc := Document{Hunks: []Hunk{{Header: "@@ -1 +1 @@", Lines: []Line{
		{Kind: Removed, OldNo: 1, Text: "abc", Spans: []Span{{Start: 2, End: 2}}},
		{Kind: Added, NewNo: 1, Text: "abd", Spans: []Span{{Start: 2, End: 3}}},
	}}}}
	frame := renderView(t, 300, 80, func(dv *DiffView) { dv.SetDocument(doc) })
	if frame == nil {
		t.Fatal("no frame")
	}
}

func TestBareEmptyContextLineIsKept(t *testing.T) {
	doc, err := ParseUnified([]byte("@@ -1,2 +1,2 @@\n a\n\n"))
	if err != nil {
		t.Fatal(err)
	}
	lines := doc.Hunks[0].Lines
	if len(lines) != 2 || lines[1].Kind != Context || lines[1].Text != "" {
		t.Fatalf("lines = %+v", lines)
	}
}
