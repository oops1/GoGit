package patch

import (
	"errors"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/diff"
)

func hunksOf(old, new string) []diff.Hunk {
	return diff.Blobs([]byte(old), []byte(new), diff.Defaults())
}

func applied(t *testing.T, base string, hunks []diff.Hunk) string {
	t.Helper()
	out, err := diff.Apply([]byte(base), hunks)
	if err != nil {
		t.Fatalf("Apply returned error %v", err)
	}
	return string(out)
}

func lineIndex(t *testing.T, hunk diff.Hunk, kind diff.Kind, text string) int {
	t.Helper()
	for index, line := range hunk.Lines {
		if line.Kind == kind && line.Text == text {
			return index
		}
	}
	t.Fatalf("no %v line %q in %+v", kind, text, hunk.Lines)
	return -1
}

const tenLines = "1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n11\n12\n13\n14\n15\n16\n17\n18\n19\n20\n"

func TestPickingEverythingRebuildsTheNewSide(t *testing.T) {
	old := tenLines
	new := "1\nONE\n2\n3\n4\n5\n6\n7\n8\n9\n10\n11\n12\n13\n14\n15\n16\n17\n19\n20\nTWENTY\n"

	selected, err := Select(hunksOf(old, new), All)

	if err != nil {
		t.Fatalf("Select returned error %v", err)
	}
	if got := applied(t, old, selected); got != new {
		t.Fatalf("result = %q, want %q", got, new)
	}
}

func TestPickingOneHunkLeavesTheOthersOut(t *testing.T) {
	old := tenLines
	new := "1\nONE\n2\n3\n4\n5\n6\n7\n8\n9\n10\n11\n12\n13\n14\n15\n16\n17\n18\n19\n20\nTWENTY\n"
	hunks := hunksOf(old, new)
	if len(hunks) != 2 {
		t.Fatalf("hunks = %+v, want two apart", hunks)
	}

	selected, err := Select(hunks, Hunk(1))

	if err != nil {
		t.Fatalf("Select returned error %v", err)
	}
	if want := tenLines + "TWENTY\n"; applied(t, old, selected) != want {
		t.Fatalf("result = %q, want %q", applied(t, old, selected), want)
	}
	if len(selected) != 1 || selected[0].NewStart != selected[0].OldStart {
		t.Fatalf("selected = %+v, want the second hunk alone at its own place", selected)
	}
}

func TestALaterHunkMovesByWhatTheEarlierOneAdded(t *testing.T) {
	old := tenLines
	new := "1\nONE\nUNO\n2\n3\n4\n5\n6\n7\n8\n9\n10\n11\n12\n13\n14\n15\n16\n17\n18\n19\n20\nTWENTY\n"

	selected, err := Select(hunksOf(old, new), All)

	if err != nil {
		t.Fatalf("Select returned error %v", err)
	}
	if selected[1].NewStart != selected[1].OldStart+2 {
		t.Fatalf("second hunk = %+v, want it two lines further on the new side", selected[1])
	}
}

func TestAnAddedLineLeftOutIsDropped(t *testing.T) {
	old := "a\nb\n"
	new := "a\nFIRST\nSECOND\nb\n"
	hunks := hunksOf(old, new)

	selected, err := Select(hunks, Lines(0, lineIndex(t, hunks[0], diff.KindAdd, "SECOND")))

	if err != nil {
		t.Fatalf("Select returned error %v", err)
	}
	if got := applied(t, old, selected); got != "a\nSECOND\nb\n" {
		t.Fatalf("result = %q", got)
	}
}

func TestADeletedLineLeftOutStaysInPlace(t *testing.T) {
	old := "a\nFIRST\nSECOND\nb\n"
	new := "a\nb\n"
	hunks := hunksOf(old, new)

	selected, err := Select(hunks, Lines(0, lineIndex(t, hunks[0], diff.KindDel, "FIRST")))

	if err != nil {
		t.Fatalf("Select returned error %v", err)
	}
	if got := applied(t, old, selected); got != "a\nSECOND\nb\n" {
		t.Fatalf("result = %q", got)
	}
	if selected[0].OldLines != selected[0].NewLines+1 {
		t.Fatalf("hunk = %+v, want one line fewer on the new side", selected[0])
	}
}

func TestPickingOnlyContextChangesNothing(t *testing.T) {
	old := "a\nb\nc\n"
	new := "a\nB\nc\n"
	hunks := hunksOf(old, new)

	selected, err := Select(hunks, Lines(0, lineIndex(t, hunks[0], diff.KindContext, "a")))

	if err != nil || selected != nil {
		t.Fatalf("selected = %+v, err = %v, want nothing", selected, err)
	}
}

func TestAMissingFinalNewlineCannotBeSplitFromItsLine(t *testing.T) {
	old := "a"
	new := "a\nb"
	hunks := hunksOf(old, new)

	_, err := Select(hunks, Lines(0, lineIndex(t, hunks[0], diff.KindAdd, "b")))

	if !errors.Is(err, ErrUnsplittable) {
		t.Fatalf("err = %v, want ErrUnsplittable", err)
	}
}

func TestAMissingFinalNewlineTakenWholeStillApplies(t *testing.T) {
	old := "a"
	new := "a\nb"

	selected, err := Select(hunksOf(old, new), All)

	if err != nil {
		t.Fatalf("Select returned error %v", err)
	}
	if got := applied(t, old, selected); got != new {
		t.Fatalf("result = %q, want %q", got, new)
	}
}

func TestADeletedLastLineWithoutNewlineMustBeTheLastOldLine(t *testing.T) {
	lines := []diff.Line{
		{Kind: diff.KindDel, Text: "a", NoNewline: true},
		{Kind: diff.KindContext, Text: "b"},
	}

	if endsCleanly(lines) {
		t.Fatal("an old line without a newline followed by another old line was accepted")
	}
}

func TestReversingSwapsTheSidesAndTheirPlaces(t *testing.T) {
	old := "a\nb\nc\n"
	new := "a\nB\nc\nd\n"
	hunks := hunksOf(old, new)

	back := Reverse(hunks)

	if got := applied(t, new, back); got != old {
		t.Fatalf("reversed result = %q, want %q", got, old)
	}
	if back[0].OldStart != hunks[0].NewStart || back[0].NewLines != hunks[0].OldLines {
		t.Fatalf("reversed = %+v, from %+v", back[0], hunks[0])
	}
}

func TestDiscardingPickedLinesWorksOnTheReversedHunks(t *testing.T) {
	index := "a\nb\nc\n"
	working := "a\nADDED\nb\nc\nMORE\n"
	hunks := hunksOf(index, working)
	pick := func(hunk, line int) bool {
		return hunks[hunk].Lines[line].Kind == diff.KindAdd && hunks[hunk].Lines[line].Text == "ADDED"
	}

	selected, err := Select(Reverse(hunks), pick)

	if err != nil {
		t.Fatalf("Select returned error %v", err)
	}
	if got := applied(t, working, selected); got != "a\nb\nc\nMORE\n" {
		t.Fatalf("working copy after discard = %q", got)
	}
}
