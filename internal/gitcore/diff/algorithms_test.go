package diff

import (
	"fmt"
	"strings"
	"testing"
)

type wordSource struct {
	state uint64
}

func (s *wordSource) next(limit int) int {
	s.state = s.state*6364136223846793005 + 1442695040888963407
	return int((s.state >> 33) % uint64(limit))
}

func vocabularyLines(seed uint64, count, vocabulary int) string {
	source := &wordSource{state: seed}
	var out strings.Builder
	for range count {
		fmt.Fprintf(&out, "word %d\n", source.next(vocabulary))
	}
	return out.String()
}

func snakedLines(seed uint64, blocks, snake, noise, vocabulary int) string {
	source := &wordSource{state: seed}
	var out strings.Builder
	for block := range blocks {
		for at := range snake {
			fmt.Fprintf(&out, "block %d line %d\n", block, at)
		}
		for range noise {
			fmt.Fprintf(&out, "noise %d\n", source.next(vocabulary))
		}
	}
	return out.String()
}

func anchoredLines(seed uint64, blocks, noise, anchor, vocabulary int) string {
	source := &wordSource{state: seed}
	var out strings.Builder
	for block := range blocks {
		for range noise {
			fmt.Fprintf(&out, "noise %d\n", source.next(vocabulary))
		}
		for at := range anchor {
			fmt.Fprintf(&out, "anchor %d %d\n", block, at)
		}
	}
	return out.String()
}

func reversedLines(text string) string {
	lines := splitLines([]byte(text))
	var out strings.Builder
	for at := len(lines) - 1; at >= 0; at-- {
		record, newline := lineText(lines[at])
		out.WriteString(record)
		if newline || at != len(lines)-1 {
			out.WriteString("\n")
		}
	}
	return out.String()
}

func repeatedLine(record string, count int) string {
	return strings.Repeat(record+"\n", count)
}

func hunkedLineCount(hunks []Hunk) (added, deleted int) {
	for _, hunk := range hunks {
		for _, line := range hunk.Lines {
			switch line.Kind {
			case KindAdd:
				added++
			case KindDel:
				deleted++
			case KindContext:
			}
		}
	}
	return added, deleted
}

func TestBothAlgorithmsProduceApplicableHunksOnHardInputs(t *testing.T) {
	cases := []struct {
		name string
		old  string
		new  string
	}{
		{
			name: "a repeated vocabulary with no long runs",
			old:  vocabularyLines(1, 4000, 40),
			new:  vocabularyLines(2, 4000, 40),
		},
		{
			name: "a reversed file",
			old:  repeatLines("line ", 2000),
			new:  reversedLines(repeatLines("line ", 2000)),
		},
		{
			name: "long runs between reshuffled noise",
			old:  snakedLines(3, 200, 40, 20, 30),
			new:  snakedLines(4, 200, 40, 20, 30),
		},
		{
			name: "a line repeated past the histogram chain limit",
			old:  "head\n" + repeatedLine("same", histChainLimit+20) + "tail\n",
			new:  "other head\n" + repeatedLine("same", histChainLimit+20) + "other tail\n",
		},
		{
			name: "many equal lines around a single change",
			old:  repeatedLine("same", 3000) + "old\n" + repeatedLine("same", 3000),
			new:  repeatedLine("same", 3000) + "new\n" + repeatedLine("same", 3000),
		},
		{
			name: "long anchors inside a very large rewrite",
			old:  anchoredLines(11, 500, 15, 60, 40),
			new:  anchoredLines(12, 500, 15, 60, 40),
		},
		{
			name: "long runs leading each block of a large rewrite",
			old:  snakedLines(21, 500, 60, 15, 40),
			new:  snakedLines(22, 500, 60, 15, 40),
		},
		{
			name: "wide blocks moved across the file",
			old:  repeatLines("alpha ", 800) + repeatLines("beta ", 800),
			new:  repeatLines("beta ", 800) + repeatLines("alpha ", 800),
		},
	}
	for _, c := range cases {
		for _, algorithm := range []Algorithm{AlgorithmMyers, AlgorithmHistogram} {
			t.Run(fmt.Sprintf("%s/%d", c.name, algorithm), func(t *testing.T) {
				opts := Defaults()
				opts.Algorithm = algorithm
				hunks := Blobs([]byte(c.old), []byte(c.new), opts)
				got, err := Apply([]byte(c.old), hunks)
				if err != nil {
					t.Fatalf("Apply returned error %v", err)
				}
				if string(got) != c.new {
					t.Error("applying the hunks did not rebuild the new side")
				}
				added, deleted := hunkedLineCount(hunks)
				if c.old != c.new && added+deleted == 0 {
					t.Error("different inputs produced no changed lines")
				}
			})
		}
	}
}

func TestDiagonalDistanceIsSymmetric(t *testing.T) {
	cases := []struct {
		d    int
		mid  int
		want int
	}{{5, 5, 0}, {7, 5, 2}, {3, 5, 2}, {-4, 1, 5}}
	for _, c := range cases {
		if got := diagonalDistance(c.d, c.mid); got != c.want {
			t.Errorf("diagonalDistance(%d, %d) returned %d instead of %d", c.d, c.mid, got, c.want)
		}
	}
}

func TestSnakeRoomChecksEveryBoxEdge(t *testing.T) {
	const snake = 4
	cases := []struct {
		name string
		i1   int
		i2   int
		want bool
	}{
		{"well inside the box", 10, 10, true},
		{"too close to the old start", 3, 10, false},
		{"at the old limit", 20, 10, false},
		{"too close to the new start", 10, 3, false},
		{"at the new limit", 10, 20, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := snakeRoomBefore(0, 20, 0, 20, c.i1, c.i2, snake); got != c.want {
				t.Errorf("snakeRoomBefore(%d, %d) returned %v instead of %v", c.i1, c.i2, got, c.want)
			}
		})
	}
	after := []struct {
		name string
		i1   int
		i2   int
		want bool
	}{
		{"well inside the box", 10, 10, true},
		{"at the old start", 0, 10, false},
		{"too close to the old limit", 18, 10, false},
		{"at the new start", 10, 0, false},
		{"too close to the new limit", 10, 18, false},
	}
	for _, c := range after {
		t.Run("after/"+c.name, func(t *testing.T) {
			if got := snakeRoomAfter(0, 20, 0, 20, c.i1, c.i2, snake); got != c.want {
				t.Errorf("snakeRoomAfter(%d, %d) returned %v instead of %v", c.i1, c.i2, got, c.want)
			}
		})
	}
}

func TestEqualRunsCompareTheRequestedWindow(t *testing.T) {
	ha1 := []int{1, 2, 3, 4, 5, 6}
	ha2 := []int{9, 2, 3, 4, 5, 9}
	if !equalBefore(ha1, ha2, 5, 5, 3) {
		t.Error("equalBefore missed a matching window")
	}
	if equalBefore(ha1, ha2, 5, 5, 5) {
		t.Error("equalBefore accepted a window with a mismatch")
	}
	if !equalAfter(ha1, ha2, 1, 1, 4) {
		t.Error("equalAfter missed a matching window")
	}
	if equalAfter(ha1, ha2, 1, 1, 5) {
		t.Error("equalAfter accepted a window with a mismatch")
	}
}

func TestHistogramFallsBackToMyersOnOverusedLines(t *testing.T) {
	old := "head\n" + repeatedLine("same", histChainLimit+5) + "tail\n"
	updated := "changed head\n" + repeatedLine("same", histChainLimit+5) + "changed tail\n"
	opts := Defaults()
	opts.Algorithm = AlgorithmHistogram
	hunks := Blobs([]byte(old), []byte(updated), opts)
	added, deleted := hunkedLineCount(hunks)
	if added != 2 || deleted != 2 {
		t.Errorf("the fallback produced %d insertions and %d deletions instead of two each", added, deleted)
	}
}

func TestBlobsIsStableAcrossRepeatedRuns(t *testing.T) {
	old := vocabularyLines(7, 600, 25)
	updated := vocabularyLines(8, 600, 25)
	first := Blobs([]byte(old), []byte(updated), Defaults())
	second := Blobs([]byte(old), []byte(updated), Defaults())
	if len(first) != len(second) {
		t.Fatalf("two runs produced %d and %d hunks", len(first), len(second))
	}
	for at := range first {
		if first[at].OldStart != second[at].OldStart || len(first[at].Lines) != len(second[at].Lines) {
			t.Fatalf("hunk %d differs between two runs", at)
		}
	}
}

func TestContextOptionsChangeTheHunkShape(t *testing.T) {
	old := repeatLines("line ", 40)
	updated := strings.Replace(old, "line 20\n", "changed 20\n", 1)
	cases := []struct {
		context int
		want    int
	}{{0, 2}, {1, 4}, {3, 8}, {10, 22}}
	for _, c := range cases {
		opts := Defaults()
		opts.Context = c.context
		hunks := Blobs([]byte(old), []byte(updated), opts)
		if len(hunks) != 1 {
			t.Fatalf("context %d produced %d hunks", c.context, len(hunks))
		}
		if got := len(hunks[0].Lines); got != c.want {
			t.Errorf("context %d produced %d lines instead of %d", c.context, got, c.want)
		}
	}
}

func TestInterHunkContextJoinsNeighbouringHunks(t *testing.T) {
	old := repeatLines("line ", 30)
	updated := strings.Replace(old, "line 10\n", "changed 10\n", 1)
	updated = strings.Replace(updated, "line 18\n", "changed 18\n", 1)
	opts := Defaults()
	opts.Context = 1
	if got := len(Blobs([]byte(old), []byte(updated), opts)); got != 2 {
		t.Fatalf("a small context produced %d hunks instead of two", got)
	}
	opts.InterHunkContext = 10
	if got := len(Blobs([]byte(old), []byte(updated), opts)); got != 1 {
		t.Errorf("a wide inter-hunk context produced %d hunks instead of one", got)
	}
}
