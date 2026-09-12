package rerere

import (
	"strings"
	"testing"
)

func markers(sign byte, size int) string {
	return strings.Repeat(string(sign), size)
}

func TestNormalizeSortsTheSidesAndDropsTheLabels(t *testing.T) {
	file := "head\n" +
		markers('<', 7) + " ours\nzebra\n" +
		markers('=', 7) + "\napple\n" +
		markers('>', 7) + " theirs\ntail\n"

	conflict, ok := Normalize([]byte(file), 0)

	if !ok {
		t.Fatal("the file was not seen as conflicted")
	}
	want := "head\n" + markers('<', 7) + "\napple\n" + markers('=', 7) + "\nzebra\n" + markers('>', 7) + "\ntail\n"
	if string(conflict.Preimage) != want {
		t.Fatalf("preimage = %q, want %q", conflict.Preimage, want)
	}
	if len(conflict.ID) != 40 {
		t.Fatalf("id = %q", conflict.ID)
	}
}

func TestNormalizeGivesTheSameIDWhateverTheOrderOfTheSides(t *testing.T) {
	one := markers('<', 7) + " a\nalpha\n" + markers('=', 7) + "\nbeta\n" + markers('>', 7) + " b\n"
	two := markers('<', 7) + " b\nbeta\n" + markers('=', 7) + "\nalpha\n" + markers('>', 7) + " a\n"

	first, ok := Normalize([]byte(one), 0)
	second, again := Normalize([]byte(two), 0)

	if !ok || !again || first.ID != second.ID {
		t.Fatalf("ids = %q and %q", first.ID, second.ID)
	}
}

func TestNormalizeIgnoresTheCommonAncestorSection(t *testing.T) {
	withBase := markers('<', 7) + " ours\nalpha\n" + markers('|', 7) + " base\nbase line\n" +
		markers('=', 7) + "\nbeta\n" + markers('>', 7) + " theirs\n"
	without := markers('<', 7) + " ours\nalpha\n" + markers('=', 7) + "\nbeta\n" + markers('>', 7) + " theirs\n"

	diff3, ok := Normalize([]byte(withBase), 0)
	plain, again := Normalize([]byte(without), 0)

	if !ok || !again || diff3.ID != plain.ID || string(diff3.Preimage) != string(plain.Preimage) {
		t.Fatalf("diff3 = %+v, plain = %+v", diff3, plain)
	}
}

func TestNormalizeKeepsNestedConflictsInsideTheirSide(t *testing.T) {
	nested := markers('<', 7) + " outer\n" +
		markers('<', 7) + " inner\nalpha\n" + markers('=', 7) + "\nbeta\n" + markers('>', 7) + " inner\n" +
		markers('=', 7) + "\ngamma\n" + markers('>', 7) + " outer\n"

	conflict, ok := Normalize([]byte(nested), 0)

	if !ok {
		t.Fatal("the nested conflict was not recognised")
	}
	if strings.Count(string(conflict.Preimage), markers('<', 7)) != 2 {
		t.Fatalf("preimage = %q", conflict.Preimage)
	}
}

func TestNormalizeRefusesFilesWithoutAConflict(t *testing.T) {
	for _, file := range []string{
		"plain\nlines\n",
		"",
		markers('<', 7) + " ours\nalpha\n",
		markers('<', 7) + " ours\nalpha\n" + markers('=', 7) + "\nbeta\n",
		markers('<', 7) + " ours\n" + markers('>', 7) + " theirs\n",
		markers('=', 7) + "\nalpha\n",
		markers('<', 7) + " ours\n" + markers('=', 7) + "\n" + markers('|', 7) + "\n" + markers('>', 7) + " theirs\n",
		markers('<', 7) + " ours\n" + markers('|', 7) + "\n" + markers('|', 7) + "\n" + markers('=', 7) + "\n" + markers('>', 7) + " theirs\n",
		markers('<', 7) + " ours\n" + markers('=', 7) + "\n" + markers('=', 7) + "\n" + markers('>', 7) + " theirs\n",
		markers('<', 7) + " ours\nalpha\n" + markers('=', 7) + "\nbeta\n" + markers('>', 7) + " theirs\n" + markers('<', 7) + " broken\n",
		markers('<', 7) + " outer\n" + markers('<', 7) + " inner\nalpha\n" + markers('=', 7) + "\n",
	} {
		if conflict, ok := Normalize([]byte(file), 7); ok {
			t.Errorf("%q was taken for a conflict: %+v", file, conflict)
		}
	}
}

func TestNormalizeNeedsTheMarkerOfTheRightWidth(t *testing.T) {
	file := markers('<', 7) + " ours\nalpha\n" + markers('=', 7) + "\nbeta\n" + markers('>', 7) + " theirs\n"

	if _, ok := Normalize([]byte(file), 11); ok {
		t.Fatal("narrow markers were taken for a conflict")
	}
}

func TestNormalizeAcceptsWideMarkers(t *testing.T) {
	file := markers('<', 11) + " ours\nalpha\n" + markers('=', 11) + "\nbeta\n" + markers('>', 11) + " theirs\n"

	conflict, ok := Normalize([]byte(file), 11)

	if !ok || !strings.HasPrefix(string(conflict.Preimage), markers('<', 11)+"\n") {
		t.Fatalf("preimage = %q", conflict.Preimage)
	}
}

func TestNormalizeHandlesAFileThatDoesNotEndWithANewline(t *testing.T) {
	file := markers('<', 7) + " ours\nalpha\n" + markers('=', 7) + "\nbeta\n" + markers('>', 7) + " theirs\ntail"

	conflict, ok := Normalize([]byte(file), 0)

	if !ok || !strings.HasSuffix(string(conflict.Preimage), "tail") {
		t.Fatalf("preimage = %q", conflict.Preimage)
	}
}

func TestNormalizeAcceptsAnyWhitespaceAfterTheMiddleMarkers(t *testing.T) {
	for _, space := range []string{" ", "\t", "\r", "\v", "\f"} {
		file := markers('<', 7) + " ours\n" + markers('|', 7) + space + "\n" +
			markers('=', 7) + space + "\nbeta\n" + markers('>', 7) + " theirs\n"
		if _, ok := Normalize([]byte(file), 0); !ok {
			t.Errorf("%q after the markers was not accepted", space)
		}
	}
}

func TestNormalizeKeepsALineThatOnlyLooksLikeAMarker(t *testing.T) {
	file := markers('<', 7) + " ours\n" + markers('=', 7) + "text\n" + markers('=', 7) + "\nbeta\n" + markers('>', 7) + " theirs\n"

	conflict, ok := Normalize([]byte(file), 0)

	if !ok || !strings.Contains(string(conflict.Preimage), markers('=', 7)+"text") {
		t.Fatalf("preimage = %q", conflict.Preimage)
	}
}

func TestResolvedTellsWhetherTheMarkersAreGone(t *testing.T) {
	conflicted := markers('<', 7) + " ours\nalpha\n" + markers('=', 7) + "\nbeta\n" + markers('>', 7) + " theirs\n"

	if Resolved([]byte(conflicted), 0) || !Resolved([]byte("alpha\n"), 0) {
		t.Fatal("Resolved disagrees with Normalize")
	}
}
