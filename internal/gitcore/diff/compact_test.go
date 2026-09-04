package diff

import (
	"fmt"
	"strings"
	"testing"
)

func sourceOf(lines ...string) *source {
	return &source{recs: lines, rchg: make([]bool, len(lines)+2)}
}

func TestMeasureSplitLooksAroundTheSplitPoint(t *testing.T) {
	s := sourceOf("a\n", "\n", "    b\n", "\n", "c\n")
	cases := []struct {
		name  string
		split int
		want  splitMeasurement
	}{
		{"at the start of the file", 0, splitMeasurement{indent: 0, preIndent: -1, postIndent: 4, postBlank: 1}},
		{"on a blank line", 1, splitMeasurement{indent: -1, preIndent: 0, postIndent: 4}},
		{"on an indented line", 2, splitMeasurement{indent: 4, preIndent: 0, preBlank: 1, postIndent: 0, postBlank: 1}},
		{"past the end of the file", 5, splitMeasurement{endOfFile: true, indent: -1, preIndent: 0, postIndent: -1}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := measureSplit(s, c.split); got != c.want {
				t.Errorf("measureSplit at %d returned %+v instead of %+v", c.split, got, c.want)
			}
		})
	}
}

func TestMeasureSplitStopsCountingBlanksAtTheLimit(t *testing.T) {
	lines := []string{"a\n"}
	for range maxBlanks + 5 {
		lines = append(lines, "\n")
	}
	lines = append(lines, "b\n")
	for range maxBlanks + 5 {
		lines = append(lines, "\n")
	}
	lines = append(lines, "c\n")
	s := sourceOf(lines...)
	got := measureSplit(s, maxBlanks+6)
	if got.preBlank != maxBlanks || got.preIndent != 0 {
		t.Errorf("the leading blanks were counted as %d with indent %d", got.preBlank, got.preIndent)
	}
	if got.postBlank != maxBlanks || got.postIndent != 0 {
		t.Errorf("the trailing blanks were counted as %d with indent %d", got.postBlank, got.postIndent)
	}
}

func TestScoreAddSplitPenalisesEachSplitShape(t *testing.T) {
	cases := []struct {
		name        string
		measurement splitMeasurement
		want        splitScore
	}{
		{
			name:        "the start of the file",
			measurement: splitMeasurement{indent: 0, preIndent: -1, postIndent: 0},
			want:        splitScore{effectiveIndent: 0, penalty: startOfFilePenalty},
		},
		{
			name:        "the end of the file",
			measurement: splitMeasurement{endOfFile: true, indent: -1, preIndent: 0, postIndent: -1},
			want:        splitScore{effectiveIndent: -1, penalty: endOfFilePenalty + totalBlankWeight + postBlankWeight},
		},
		{
			name:        "an unchanged indent",
			measurement: splitMeasurement{indent: 4, preIndent: 4, postIndent: 4},
			want:        splitScore{effectiveIndent: 4},
		},
		{
			name:        "a deeper indent without blanks",
			measurement: splitMeasurement{indent: 8, preIndent: 4, postIndent: 8},
			want:        splitScore{effectiveIndent: 8, penalty: relativeIndentPenalty},
		},
		{
			name:        "a deeper indent next to blanks",
			measurement: splitMeasurement{indent: 8, preIndent: 4, postIndent: 8, preBlank: 2},
			want: splitScore{
				effectiveIndent: 8,
				penalty:         relativeIndentWithBlankPenalty + 2*totalBlankWeight,
			},
		},
		{
			name:        "an outdent followed by deeper lines",
			measurement: splitMeasurement{indent: 4, preIndent: 8, postIndent: 8},
			want:        splitScore{effectiveIndent: 4, penalty: relativeOutdentPenalty},
		},
		{
			name:        "an outdent followed by deeper lines next to blanks",
			measurement: splitMeasurement{indent: 4, preIndent: 8, postIndent: 8, preBlank: 1},
			want: splitScore{
				effectiveIndent: 4,
				penalty:         relativeOutdentWithBlankPenalty + totalBlankWeight,
			},
		},
		{
			name:        "a dedent",
			measurement: splitMeasurement{indent: 4, preIndent: 8, postIndent: 0},
			want:        splitScore{effectiveIndent: 4, penalty: relativeDedentPenalty},
		},
		{
			name:        "a dedent next to blanks",
			measurement: splitMeasurement{indent: 4, preIndent: 8, postIndent: 0, preBlank: 3},
			want: splitScore{
				effectiveIndent: 4,
				penalty:         relativeDedentWithBlankPenalty + 3*totalBlankWeight,
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var got splitScore
			scoreAddSplit(c.measurement, &got)
			if got != c.want {
				t.Errorf("scoreAddSplit returned %+v instead of %+v", got, c.want)
			}
		})
	}
}

func TestScoreCmpWeighsIndentAboveThePenalty(t *testing.T) {
	cases := []struct {
		name string
		a    splitScore
		b    splitScore
		want int
	}{
		{"an equal score", splitScore{}, splitScore{}, 0},
		{"a deeper indent loses", splitScore{effectiveIndent: 4}, splitScore{}, indentWeight},
		{"a shallower indent wins", splitScore{}, splitScore{effectiveIndent: 4}, -indentWeight},
		{"the penalty decides a tie", splitScore{penalty: 5}, splitScore{penalty: 2}, 3},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := scoreCmp(c.a, c.b); got != c.want {
				t.Errorf("scoreCmp returned %d instead of %d", got, c.want)
			}
		})
	}
}

func TestGroupPreviousStopsAtTheStartOfTheFile(t *testing.T) {
	s := sourceOf("a\n", "b\n", "c\n")
	g := group{}
	if groupPrevious(s, &g) {
		t.Error("groupPrevious moved past the start of the file")
	}
	g = group{start: 2, end: 3}
	if !groupPrevious(s, &g) {
		t.Error("groupPrevious refused to move back inside the file")
	}
	if g.start != 1 || g.end != 1 {
		t.Errorf("groupPrevious moved to %d..%d instead of 1..1", g.start, g.end)
	}
}

func TestGroupSlidingRefusesUnmatchedNeighbours(t *testing.T) {
	e := prepareEnv(splitLines([]byte("a\nb\nc\n")), splitLines([]byte("a\nB\nc\n")), Defaults())
	e.myers()
	g := groupInit(e.a)
	groupNext(e.a, &g)
	if groupSlideDown(e.a, &g) {
		t.Error("a group with unmatched neighbours slid down")
	}
	if groupSlideUp(e.a, &g) {
		t.Error("a group with unmatched neighbours slid up")
	}
}

func hunkShape(hunks []Hunk) string {
	var out strings.Builder
	for _, hunk := range hunks {
		fmt.Fprintf(&out, "@%d,%d+%d,%d", hunk.OldStart, hunk.OldLines, hunk.NewStart, hunk.NewLines)
		for _, line := range hunk.Lines {
			out.WriteString(line.Kind.prefix())
			out.WriteString(line.Text)
			out.WriteString("|")
		}
	}
	return out.String()
}

func TestIndentHeuristicMovesSomeHunksOfTheCorpus(t *testing.T) {
	moved := 0
	for _, pair := range corpus() {
		withHeuristic := Blobs([]byte(pair.old), []byte(pair.new), Defaults())
		without := Blobs([]byte(pair.old), []byte(pair.new), withOptions(func(o *Options) { o.IndentHeuristic = false }))
		if hunkShape(withHeuristic) != hunkShape(without) {
			moved++
		}
	}
	if moved == 0 {
		t.Error("the indent heuristic changed no hunk of the corpus")
	}
}
