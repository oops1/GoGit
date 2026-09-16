package merge

import (
	"slices"
	"strings"
	"testing"
)

func attributedMerge(t *testing.T, s *memoryStore, depth int, attrs PathAttributes, base, ours, theirs string) TreeResult {
	t.Helper()
	result, err := Trees(Snapshot{"p": s.blob(t, base)}, Snapshot{"p": s.blob(t, ours)}, Snapshot{"p": s.blob(t, theirs)}, s, TreeOptions{
		Depth: depth,
		File:  Options{Labels: Labels{Ours: "ours", Theirs: "theirs"}},
		Attributes: func(path string, virtual bool) PathAttributes {
			if path != "p" || virtual != (depth > 0) {
				t.Errorf("attributes asked for %q, virtual %v", path, virtual)
			}
			return attrs
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestAUnionDriverKeepsBothSidesWithoutAConflict(t *testing.T) {
	s := newStore()

	result := attributedMerge(t, s, 0, PathAttributes{Driver: DriverUnion}, "a\nb\nc\n", "a\nO\nc\n", "a\nT\nc\n")

	if !result.Clean() || s.text(t, result.Tree["p"]) != "a\nO\nT\nc\n" {
		t.Fatalf("result = %+v, text %q", result, s.text(t, result.Tree["p"]))
	}
}

func TestABinaryDriverTreatsTextAsBinary(t *testing.T) {
	s := newStore()

	result := attributedMerge(t, s, 0, PathAttributes{Driver: DriverBinary}, "a\nb\nc\n", "a\nO\nc\n", "a\nb\nC\n")

	if c := onlyConflict(t, result); c.Kind != ConflictBinary || s.text(t, result.Tree["p"]) != "a\nO\nc\n" || len(result.Warnings) != 0 {
		t.Fatalf("result = %+v, want a binary conflict keeping ours", result)
	}
}

func TestAnExternalDriverIsReportedAndLeftAsABinaryConflict(t *testing.T) {
	s := newStore()

	result := attributedMerge(t, s, 0, PathAttributes{Driver: DriverExternal, Name: "custom"}, "a\n", "O\n", "T\n")

	if c := onlyConflict(t, result); c.Kind != ConflictBinary {
		t.Fatalf("conflict = %+v, want binary", c)
	}
	if want := []Warning{{Kind: WarningExternalDriver, Path: "p", Driver: "custom"}}; !slices.Equal(result.Warnings, want) {
		t.Fatalf("warnings = %+v, want %+v", result.Warnings, want)
	}
}

func TestTheMarkerSizeAttributeGrowsWithTheVirtualDepth(t *testing.T) {
	s := newStore()
	for depth, want := range map[int]string{0: strings.Repeat("<", 12) + " ours\n", 1: strings.Repeat("<", 14) + " ours\n"} {
		result := attributedMerge(t, s, depth, PathAttributes{MarkerSize: 12}, "a\nb\nc\n", "a\nO\nc\n", "a\nT\nc\n")

		if text := s.text(t, result.Tree["p"]); !strings.Contains(text, "\n"+want) {
			t.Errorf("depth %d: text %q, want %q", depth, text, want)
		}
	}
}
