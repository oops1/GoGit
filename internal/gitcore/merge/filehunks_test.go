package merge

import (
	"slices"
	"strings"
	"testing"
)

func TestSharedLinesInsideAConflictSplitIt(t *testing.T) {
	base := lines("a", "b", "c")
	ours := lines("a", "O1", "s1", "s2", "s3", "s4", "O2", "c")
	theirs := lines("a", "T1", "s1", "s2", "s3", "s4", "T2", "c")

	got, conflicts := merged(t, base, ours, theirs, Options{})

	want := strings.Join([]string{"a", "<<<<<<<", "O1", "=======", "T1", ">>>>>>>", "s1", "s2", "s3", "s4", "<<<<<<<", "O2", "=======", "T2", ">>>>>>>", "c", ""}, newline)
	if got != want || conflicts != 2 {
		t.Fatalf("content = %q, conflicts = %d, want %q", got, conflicts, want)
	}
}

func TestAChangeOnOneSideKeepsNeighbouringConflictsApart(t *testing.T) {
	base := lines("a", "b", "c", "d", "e")
	ours := lines("A", "b", "C", "d", "E")
	theirs := lines("A2", "b", "c", "d", "E2")

	got, conflicts := merged(t, base, ours, theirs, Options{})

	want := strings.Join([]string{"<<<<<<<", "A", "=======", "A2", ">>>>>>>", "b", "C", "d", "<<<<<<<", "E", "=======", "E2", ">>>>>>>", ""}, newline)
	if got != want || conflicts != 2 {
		t.Fatalf("content = %q, conflicts = %d, want %q", got, conflicts, want)
	}
}

func TestLinesWithoutLettersJoinConflictsOnlyAtTheAlnumLevel(t *testing.T) {
	base := lines("a", "{", "}", "", ")", "g")
	ours := lines("OURS", "{", "}", "", ")", "OURS")
	theirs := lines("THEIRS", "{", "}", "", ")", "THEIRS")

	_, zealous := merged(t, base, ours, theirs, Options{})
	_, alnum := merged(t, base, ours, theirs, Options{Level: LevelZealousAlnum})
	_, lettered := merged(t, base, lines("OURS", "{", "x", "", ")", "OURS"), lines("THEIRS", "{", "x", "", ")", "THEIRS"), Options{Level: LevelZealousAlnum})

	if zealous != 2 || alnum != 1 || lettered != 2 {
		t.Fatalf("conflicts: zealous %d, alnum %d, with letters %d; want 2, 1, 2", zealous, alnum, lettered)
	}
}

func TestSidesThatDifferOnlyInHowTheyGotThereMergeCleanly(t *testing.T) {
	base := lines("a", "b", "c", "d")
	ours := lines("X", "b", "Y", "d")
	theirs := lines("X", "Y", "d")

	got, conflicts := merged(t, lines("a", "b", "c", "d"), lines("a", "B", "C", "d"), lines("a", "B", "C", "d"), Options{})
	if got != string(lines("a", "B", "C", "d")) || conflicts != 0 {
		t.Fatalf("content = %q, conflicts = %d", got, conflicts)
	}

	got, conflicts = merged(t, base, ours, theirs, Options{})

	if conflicts != 1 || !strings.Contains(got, "<<<<<<<\nb\n=======\n>>>>>>>\n") {
		t.Fatalf("content = %q, conflicts = %d", got, conflicts)
	}
}

func TestIdenticalLinesReachedByDifferentEditsAreNotAConflict(t *testing.T) {
	base := lines("a", "b", "c", "d")
	ours := lines("a", "N", "M", "d")
	theirs := lines("a", "N", "M", "d")
	oursPrefixed := lines("P", "N", "M", "d")
	theirsReplaced := lines("P", "N", "M", "d")

	got, conflicts := merged(t, base, oursPrefixed, theirsReplaced, Options{})
	if got != string(oursPrefixed) || conflicts != 0 {
		t.Fatalf("content = %q, conflicts = %d", got, conflicts)
	}
	s := sides{base: splitLines(base), ours: splitLines(ours), theirs: splitLines(theirs)}
	refined := s.refine([]node{{mode: nodeConflict, i0: 1, chg0: 2, i1: 1, chg1: 2, i2: 1, chg2: 2}}, Options{}.Diff)
	if len(refined) != 1 || refined[0].mode != nodeIdentical {
		t.Fatalf("refined = %+v, want the identical sides recognised", refined)
	}
	if got := string(s.render(refined, Options{}).Content); got != string(ours) {
		t.Fatalf("render = %q, want the shared lines once", got)
	}
	blocks := s.chunks(refined)
	if count(blocks) != 0 || chunkText(blocks[0].Merged) != string(ours) {
		t.Fatalf("blocks = %+v", blocks)
	}
}

func TestAUnionMergeKeepsBothSidesOfEveryConflict(t *testing.T) {
	base := []byte("a\nb\nc")
	ours := []byte("a\nOURS")
	theirs := []byte("a\nTHEIRS")

	got, conflicts := merged(t, base, ours, theirs, Options{Union: true})

	if got != "a\nOURS\nTHEIRS" || conflicts != 0 {
		t.Fatalf("content = %q, conflicts = %d", got, conflicts)
	}
}

func TestMarkersFollowWindowsLineEndings(t *testing.T) {
	base := []byte("a\r\nb\r\nc\r\n")
	ours := []byte("a\r\nOURS\r\nc\r\n")
	theirs := []byte("a\r\nTHEIRS\r\nc\r\n")

	got, _ := merged(t, base, ours, theirs, Options{Style: StyleDiff3, Labels: Labels{Ours: "o", Base: "b", Theirs: "t"}})

	want := "a\r\n<<<<<<< o\r\nOURS\r\n||||||| b\r\nb\r\n=======\r\nTHEIRS\r\n>>>>>>> t\r\nc\r\n"
	if got != want {
		t.Fatalf("content = %q, want %q", got, want)
	}
}

func TestTheLineEndingIsTakenFromTheLineBeforeAnUnterminatedLastLine(t *testing.T) {
	for _, c := range []struct {
		lines []string
		at    int
		want  int
	}{
		{nil, 0, -1},
		{[]string{"x"}, 0, -1},
		{[]string{"x\r\n", "y"}, 1, 1},
		{[]string{"x\n", "y"}, 1, 0},
		{[]string{"x\r\n"}, 0, 1},
		{[]string{"x\n", "y\n"}, 0, 0},
	} {
		if got := eolIsCRLF(c.lines, c.at); got != c.want {
			t.Errorf("eolIsCRLF(%q, %d) = %d, want %d", c.lines, c.at, got, c.want)
		}
	}
}

func TestBlocksAddUpToEachSide(t *testing.T) {
	cases := [][3][]byte{
		{lines("a", "b", "c"), lines("a", "O1", "s1", "s2", "s3", "s4", "O2", "c"), lines("a", "T1", "s1", "s2", "s3", "s4", "T2", "c")},
		{lines("a", "b", "c", "d", "e"), lines("A", "b", "c", "d", "E"), lines("A1", "b", "c", "d", "E1")},
		{lines("x"), lines("head", "OURS", "tail"), lines("head", "THEIRS", "tail")},
		{lines("a", "b"), lines("a", "b", "c"), lines("z", "a", "b")},
	}
	for _, style := range []Style{StyleMerge, StyleDiff3, StyleZDiff3} {
		for _, c := range cases {
			var base, ours, theirs []string
			for _, block := range Chunks(c[0], c[1], c[2], Options{Style: style}) {
				base = append(base, block.Base...)
				ours = append(ours, block.Ours...)
				theirs = append(theirs, block.Theirs...)
			}
			if !slices.Equal(base, splitLines(c[0])) || !slices.Equal(ours, splitLines(c[1])) || !slices.Equal(theirs, splitLines(c[2])) {
				t.Fatalf("style %d: blocks of %q do not add up: %q %q %q", style, c, base, ours, theirs)
			}
		}
	}
}

func TestZDiff3MovesLinesBothSidesShareOutOfTheConflict(t *testing.T) {
	base := lines("x")
	ours := lines("head", "OURS", "tail")
	theirs := lines("head", "THEIRS", "tail")

	got, _ := merged(t, base, ours, theirs, Options{Style: StyleZDiff3})

	want := strings.Join([]string{"head", "<<<<<<<", "OURS", "|||||||", "x", "=======", "THEIRS", ">>>>>>>", "tail", ""}, newline)
	if got != want {
		t.Fatalf("content = %q, want %q", got, want)
	}
}
