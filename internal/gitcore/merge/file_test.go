package merge

import (
	"strings"
	"testing"
)

const newline = "\n"

func lines(text ...string) []byte {
	out := ""
	for _, line := range text {
		out += line + "\n"
	}
	return []byte(out)
}

func merged(t *testing.T, base, ours, theirs []byte, opts Options) (string, int) {
	t.Helper()
	result := File(base, ours, theirs, opts)
	return string(result.Content), result.Conflicts
}

func TestAFileNobodyTouchedComesBackAsItWas(t *testing.T) {
	base := lines("one", "two", "three")

	got, conflicts := merged(t, base, base, base, Options{})

	if got != string(base) || conflicts != 0 {
		t.Fatalf("content = %q, conflicts = %d, want the file unchanged", got, conflicts)
	}
}

func TestAChangeOnOneSideIsTakenAsItIs(t *testing.T) {
	base := lines("one", "two", "three")
	ours := lines("one", "two", "three")
	theirs := lines("one", "TWO", "three")

	got, conflicts := merged(t, base, ours, theirs, Options{})

	if got != string(theirs) || conflicts != 0 {
		t.Fatalf("content = %q, conflicts = %d, want their change", got, conflicts)
	}
}

func TestChangesInDifferentPlacesAreBothKept(t *testing.T) {
	base := lines("one", "two", "three", "four")
	ours := lines("ONE", "two", "three", "four")
	theirs := lines("one", "two", "three", "FOUR")

	got, conflicts := merged(t, base, ours, theirs, Options{})

	if got != string(lines("ONE", "two", "three", "FOUR")) || conflicts != 0 {
		t.Fatalf("content = %q, conflicts = %d, want both changes", got, conflicts)
	}
}

func TestTheSameChangeOnBothSidesIsTakenOnce(t *testing.T) {
	base := lines("one", "two", "three")
	side := lines("one", "TWO", "three")

	got, conflicts := merged(t, base, side, side, Options{})

	if got != string(side) || conflicts != 0 {
		t.Fatalf("content = %q, conflicts = %d, want the change once", got, conflicts)
	}
}

func TestTwoChangesInOnePlaceConflict(t *testing.T) {
	base := lines("one", "two", "three")
	ours := lines("one", "OURS", "three")
	theirs := lines("one", "THEIRS", "three")

	got, conflicts := merged(t, base, ours, theirs, Options{Labels: Labels{Ours: "HEAD", Theirs: "topic"}})

	want := "one\n<<<<<<< HEAD\nOURS\n=======\nTHEIRS\n>>>>>>> topic\nthree\n"
	if got != want || conflicts != 1 {
		t.Fatalf("content = %q, conflicts = %d, want %q with one conflict", got, conflicts, want)
	}
}

func TestTheBaseIsShownBetweenTheSidesInDiff3(t *testing.T) {
	base := lines("one", "two", "three")
	ours := lines("one", "OURS", "three")
	theirs := lines("one", "THEIRS", "three")

	got, _ := merged(t, base, ours, theirs, Options{
		Style:  StyleDiff3,
		Labels: Labels{Ours: "HEAD", Base: "base", Theirs: "topic"},
	})

	want := "one\n<<<<<<< HEAD\nOURS\n||||||| base\ntwo\n=======\nTHEIRS\n>>>>>>> topic\nthree\n"
	if got != want {
		t.Fatalf("content = %q, want %q", got, want)
	}
}

func TestLinesBothSidesAgreeOnLeaveTheConflictInZDiff3(t *testing.T) {
	base := lines("one", "two", "three")
	ours := lines("one", "shared", "OURS", "three")
	theirs := lines("one", "shared", "THEIRS", "three")

	got, conflicts := merged(t, base, ours, theirs, Options{
		Style:  StyleZDiff3,
		Labels: Labels{Ours: "HEAD", Base: "base", Theirs: "topic"},
	})

	want := "one\nshared\n<<<<<<< HEAD\nOURS\n||||||| base\ntwo\n=======\nTHEIRS\n>>>>>>> topic\nthree\n"
	if got != want || conflicts != 1 {
		t.Fatalf("content = %q, conflicts = %d, want %q", got, conflicts, want)
	}
}

func TestALineAddedByBothSidesInTheSamePlaceConflicts(t *testing.T) {
	base := lines("one", "two")
	ours := lines("one", "ours", "two")
	theirs := lines("one", "theirs", "two")

	_, conflicts := merged(t, base, ours, theirs, Options{})

	if conflicts != 1 {
		t.Fatalf("conflicts = %d, want one", conflicts)
	}
}

func TestALineDeletedOnOneSideStaysDeleted(t *testing.T) {
	base := lines("one", "two", "three")
	ours := lines("one", "three")
	theirs := lines("one", "two", "three")

	got, conflicts := merged(t, base, ours, theirs, Options{})

	if got != string(lines("one", "three")) || conflicts != 0 {
		t.Fatalf("content = %q, conflicts = %d, want the line gone", got, conflicts)
	}
}

func TestADeletionAgainstAChangeConflicts(t *testing.T) {
	base := lines("one", "two", "three")
	ours := lines("one", "three")
	theirs := lines("one", "TWO", "three")

	_, conflicts := merged(t, base, ours, theirs, Options{})

	if conflicts != 1 {
		t.Fatalf("conflicts = %d, want one", conflicts)
	}
}

func TestAFileEmptyOnBothSidesEndsEmpty(t *testing.T) {
	got, conflicts := merged(t, nil, nil, nil, Options{})

	if got != "" || conflicts != 0 {
		t.Fatalf("content = %q, conflicts = %d, want nothing", got, conflicts)
	}
}

func TestAMissingNewlineAtTheEndIsKept(t *testing.T) {
	base := []byte("one\ntwo")
	ours := []byte("one\ntwo")
	theirs := []byte("one\nTWO")

	got, conflicts := merged(t, base, ours, theirs, Options{})

	if got != "one\nTWO" || conflicts != 0 {
		t.Fatalf("content = %q, conflicts = %d, want the file without its last newline", got, conflicts)
	}
}

func TestAMissingNewlineBeforeAMarkerGetsOne(t *testing.T) {
	base := []byte("one\ntwo")
	ours := []byte("one\nOURS")
	theirs := []byte("one\nTHEIRS")

	got, conflicts := merged(t, base, ours, theirs, Options{})

	want := "one\n<<<<<<<\nOURS\n=======\nTHEIRS\n>>>>>>>\n"
	if got != want || conflicts != 1 {
		t.Fatalf("content = %q, conflicts = %d, want %q", got, conflicts, want)
	}
}

func TestConflictsWithFewLinesBetweenThemBecomeOne(t *testing.T) {
	base := lines("a", "b", "c", "d", "e")
	ours := lines("A", "b", "c", "d", "E")
	theirs := lines("A1", "b", "c", "d", "E1")

	got, conflicts := merged(t, base, ours, theirs, Options{})

	want := strings.Join([]string{"<<<<<<<", "A", "b", "c", "d", "E", "=======", "A1", "b", "c", "d", "E1", ">>>>>>>", ""}, "\n")
	if got != want || conflicts != 1 {
		t.Fatalf("content = %q, conflicts = %d, want the two conflicts joined", got, conflicts)
	}
}

func TestConflictsFarApartStayApart(t *testing.T) {
	base := lines("a", "b", "c", "d", "e", "f")
	ours := lines("A", "b", "c", "d", "e", "F")
	theirs := lines("A1", "b", "c", "d", "e", "F1")

	_, conflicts := merged(t, base, ours, theirs, Options{})

	if conflicts != 2 {
		t.Fatalf("conflicts = %d, want two", conflicts)
	}
}

func TestTheBaseBlockKeepsConflictsApart(t *testing.T) {
	base := lines("a", "b", "c", "d", "e")
	ours := lines("A", "b", "c", "d", "E")
	theirs := lines("A1", "b", "c", "d", "E1")

	for _, style := range []Style{StyleDiff3, StyleZDiff3} {
		if _, conflicts := merged(t, base, ours, theirs, Options{Style: style}); conflicts != 2 {
			t.Fatalf("style %d: conflicts = %d, want the base block to keep them apart", style, conflicts)
		}
	}
}

func TestChangesThatTouchAreMergedIntoOneConflict(t *testing.T) {
	base := lines("a", "b", "c")
	ours := lines("A", "b", "c")
	theirs := lines("a", "B", "c")

	got, conflicts := merged(t, base, ours, theirs, Options{})

	want := strings.Join([]string{"<<<<<<<", "A", "b", "=======", "a", "B", ">>>>>>>", "c", ""}, newline)
	if conflicts != 1 || got != want {
		t.Fatalf("content = %q, conflicts = %d, want %q", got, conflicts, want)
	}
}

func TestALineBothSidesEndWithLeavesTheConflict(t *testing.T) {
	base := lines("x")
	ours := lines("OURS", "shared")
	theirs := lines("THEIRS", "shared")

	got, conflicts := merged(t, base, ours, theirs, Options{})

	want := strings.Join([]string{"<<<<<<<", "OURS", "=======", "THEIRS", ">>>>>>>", "shared", ""}, newline)
	if got != want || conflicts != 1 {
		t.Fatalf("content = %q, conflicts = %d, want %q", got, conflicts, want)
	}
}

func TestOneWideChangeSwallowsTwoNarrowOnes(t *testing.T) {
	base := lines("a", "b", "c", "d")
	ours := lines("A", "b", "C", "d")
	theirs := lines("X", "Y", "Z", "d")

	got, conflicts := merged(t, base, ours, theirs, Options{})

	want := strings.Join([]string{"<<<<<<<", "A", "b", "C", "=======", "X", "Y", "Z", ">>>>>>>", "d", ""}, newline)
	if got != want || conflicts != 1 {
		t.Fatalf("content = %q, conflicts = %d, want %q", got, conflicts, want)
	}
}
