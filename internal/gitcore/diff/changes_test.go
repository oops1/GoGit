package diff

import (
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestChangesListTheChangedLineRanges(t *testing.T) {
	got := Changes([]byte("a\nb\nc\n"), []byte("a\nB\nc\nd\n"), Defaults())

	want := []Change{
		{OldIndex: 1, OldCount: 1, NewIndex: 1, NewCount: 1},
		{OldIndex: 3, OldCount: 0, NewIndex: 3, NewCount: 1},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("Changes returned %+v instead of %+v", got, want)
	}
}

func TestOnlyPatchesTrimTheCommonTailWithoutContext(t *testing.T) {
	pair := commonTailPair()
	store := newMemoryStore()
	oldTree := buildTree(store, treeFiles{"f": blobSpec(pair.old)})
	newTree := buildTree(store, treeFiles{"f": blobSpec(pair.new)})
	opts := Defaults()
	opts.Context = 0

	files, err := Trees(t.Context(), store, oldTree, newTree, opts)

	if err != nil || len(files) != 1 {
		t.Fatalf("Trees returned %+v, %v", files, err)
	}
	trimmed := patchHunks([]byte(pair.old), []byte(pair.new), opts)
	if !reflect.DeepEqual(files[0].Hunks, trimmed) || trimmed[1].NewStart != 8 {
		t.Fatalf("tree hunks %+v differ from patch hunks %+v", files[0].Hunks, trimmed)
	}
	if whole := Blobs([]byte(pair.old), []byte(pair.new), opts); reflect.DeepEqual(whole, trimmed) {
		t.Fatalf("Blobs trimmed the common tail: %+v", whole)
	}
}

func TestChangesOfEqualDataAreEmpty(t *testing.T) {
	if got := Changes([]byte("a\nb\n"), []byte("a\nb\n"), Defaults()); len(got) != 0 {
		t.Fatalf("Changes returned %+v", got)
	}
}

func TestTrimCommonTailCutsWholeBlocksBackToALineEnd(t *testing.T) {
	common := strings.Repeat("common line\n", 300)
	a, b := []byte("old head\n"+common), []byte("new\n"+common)

	gotA, gotB := trimCommonTail(a, b)

	cut := len(a) - len(gotA)
	if cut == 0 || cut%tailBlock == 0 || len(b)-len(gotB) != cut {
		t.Fatalf("trimmed %d and %d bytes", cut, len(b)-len(gotB))
	}
	if gotA[len(gotA)-1] != '\n' || !strings.HasPrefix(string(gotA), "old head\n") || !strings.HasPrefix(string(gotB), "new\n") {
		t.Fatalf("trimmed data ends badly: %q / %q", gotA[len(gotA)-12:], gotB[len(gotB)-12:])
	}
}

func TestTrimCommonTailKeepsATailWithoutNewlines(t *testing.T) {
	common := strings.Repeat("x", 3*tailBlock)
	a, b := []byte("a"+common), []byte("b"+common)

	gotA, gotB := trimCommonTail(a, b)

	if len(gotA) != len(a) || len(gotB) != len(b) {
		t.Fatalf("trimmed to %d and %d bytes", len(gotA), len(gotB))
	}
}

func TestTrimCommonTailLeavesShortDataAlone(t *testing.T) {
	gotA, gotB := trimCommonTail([]byte("a\nsame\n"), []byte("b\nsame\n"))

	if string(gotA) != "a\nsame\n" || string(gotB) != "b\nsame\n" {
		t.Fatalf("trimmed to %q and %q", gotA, gotB)
	}
}
