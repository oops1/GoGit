package merge

import (
	"maps"
	"slices"
	"testing"
)

func directoryMerge(t *testing.T, s *memoryStore, mode DirectoryRenames, base, ours, theirs Snapshot, ourRenames, theirRenames Renames) TreeResult {
	t.Helper()
	result, err := Trees(base, ours, theirs, s, TreeOptions{
		File:             Options{Labels: Labels{Ours: "ours", Theirs: "theirs"}},
		OurRenames:       ourRenames,
		TheirRenames:     theirRenames,
		DirectoryRenames: mode,
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func paths(tree Snapshot) []string { return slices.Sorted(maps.Keys(tree)) }

type movedLibrary struct {
	s                  *memoryStore
	a, b, added        Entry
	base, ours, theirs Snapshot
	ourRenames         Renames
}

func newMovedLibrary(t *testing.T) movedLibrary {
	s := newStore()
	m := movedLibrary{s: s, a: s.blob(t, tenLines("a")), b: s.blob(t, tenLines("b")), added: s.blob(t, "new\n")}
	m.base = Snapshot{"lib/a": m.a, "lib/b": m.b}
	m.ours = Snapshot{"src/a": m.a, "src/b": m.b}
	m.theirs = Snapshot{"lib/a": m.a, "lib/b": m.b, "lib/new": m.added}
	m.ourRenames = Renames{"lib/a": "src/a", "lib/b": "src/b"}
	return m
}

func TestAFileAddedInADirectoryTheOtherSideRenamedFollowsIt(t *testing.T) {
	for mode, wantConflict := range map[DirectoryRenames]bool{DirectoryRenamesConflict: true, DirectoryRenamesApply: false} {
		m := newMovedLibrary(t)

		result := directoryMerge(t, m.s, mode, m.base, m.ours, m.theirs, m.ourRenames, nil)

		if !slices.Equal(paths(result.Tree), []string{"src/a", "src/b", "src/new"}) {
			t.Fatalf("mode %d: tree = %v", mode, paths(result.Tree))
		}
		want := []Conflict{{Path: "src/new", Kind: ConflictFileLocation, Theirs: &m.added}}
		if wantConflict && !sameConflicts(result.Conflicts, want) || !wantConflict && !result.Clean() {
			t.Fatalf("mode %d: conflicts = %+v", mode, result.Conflicts)
		}
	}
}

func TestDirectoryRenamesCanBeSwitchedOff(t *testing.T) {
	m := newMovedLibrary(t)

	result := directoryMerge(t, m.s, DirectoryRenamesOff, m.base, m.ours, m.theirs, m.ourRenames, nil)

	if !result.Clean() || !slices.Equal(paths(result.Tree), []string{"lib/new", "src/a", "src/b"}) {
		t.Fatalf("result = %+v", result)
	}
}

func TestADirectorySplitWithoutAMajorityIsNotRenamed(t *testing.T) {
	m := newMovedLibrary(t)
	ours := Snapshot{"x/a": m.a, "y/b": m.b}

	result := directoryMerge(t, m.s, DirectoryRenamesApply, m.base, ours, m.theirs, Renames{"lib/a": "x/a", "lib/b": "y/b"}, nil)

	if !slices.Equal(paths(result.Tree), []string{"lib/new", "x/a", "y/b"}) {
		t.Fatalf("tree = %v", paths(result.Tree))
	}
}

func TestADirectoryTheAddingSideStillHoldsIsRenamedOnlyByTheOtherSide(t *testing.T) {
	m := newMovedLibrary(t)
	theirs := Snapshot{"dst/a": m.a, "dst/b": m.b, "lib/new": m.added}

	result := directoryMerge(t, m.s, DirectoryRenamesApply, m.base, m.ours, theirs, m.ourRenames, Renames{"lib/a": "dst/a", "lib/b": "dst/b"})

	if _, moved := result.Tree["src/new"]; !moved {
		t.Fatalf("tree = %v", paths(result.Tree))
	}
}

func TestFilesThatWouldLandOnOnePathStayWhereTheyWere(t *testing.T) {
	m := newMovedLibrary(t)
	c := m.s.blob(t, tenLines("c"))
	base := Snapshot{"lib/a": m.a, "lib2/c": c}
	ours := Snapshot{"src/a": m.a, "src/c": c}
	theirs := Snapshot{"lib/a": m.a, "lib2/c": c, "lib/new": m.added, "lib2/new": m.added}

	result := directoryMerge(t, m.s, DirectoryRenamesApply, base, ours, theirs, Renames{"lib/a": "src/a", "lib2/c": "src/c"}, nil)

	if !slices.Equal(paths(result.Tree), []string{"lib/new", "lib2/new", "src/a", "src/c"}) {
		t.Fatalf("tree = %v", paths(result.Tree))
	}
}

func TestAPathAlreadyThereKeepsAFileFromMovingOntoIt(t *testing.T) {
	m := newMovedLibrary(t)
	other := m.s.blob(t, "other\n")
	onTheirSide := m.theirs
	onTheirSide["src/new/inner"] = other
	everywhere := Snapshot{"src/new": other}
	maps.Copy(everywhere, m.base)
	oursEverywhere := Snapshot{"src/new": other}
	maps.Copy(oursEverywhere, m.ours)
	theirsEverywhere := Snapshot{"src/new": other}
	maps.Copy(theirsEverywhere, m.theirs)
	delete(theirsEverywhere, "src/new/inner")

	directory := directoryMerge(t, m.s, DirectoryRenamesApply, m.base, m.ours, onTheirSide, m.ourRenames, nil)
	unchanged := directoryMerge(t, m.s, DirectoryRenamesApply, everywhere, oursEverywhere, theirsEverywhere, m.ourRenames, nil)

	if _, kept := directory.Tree["lib/new"]; !kept {
		t.Fatalf("a directory in the way: tree = %v", paths(directory.Tree))
	}
	if _, kept := unchanged.Tree["lib/new"]; !kept {
		t.Fatalf("an unchanged file in the way: tree = %v", paths(unchanged.Tree))
	}
}

func TestAFileLandingWhereTheirSideAlreadyAddedOneStays(t *testing.T) {
	m := newMovedLibrary(t)
	theirs := Snapshot{"src/new": m.s.blob(t, "theirs\n")}
	maps.Copy(theirs, m.theirs)

	result := directoryMerge(t, m.s, DirectoryRenamesApply, m.base, m.ours, theirs, m.ourRenames, nil)

	if _, kept := result.Tree["lib/new"]; !kept {
		t.Fatalf("tree = %v", paths(result.Tree))
	}
}

func TestADirectoryTheOtherSideRenamedAwayIsNotATarget(t *testing.T) {
	s := newStore()
	a, b, added := s.blob(t, tenLines("a")), s.blob(t, tenLines("b")), s.blob(t, "new\n")
	base := Snapshot{"lib/a": a, "src/b": b}
	ours := Snapshot{"src/a": a, "src/b": b}
	theirs := Snapshot{"lib/a": a, "other/b": b, "lib/new": added}

	result := directoryMerge(t, s, DirectoryRenamesApply, base, ours, theirs, Renames{"lib/a": "src/a"}, Renames{"src/b": "other/b"})

	if _, kept := result.Tree["lib/new"]; !kept {
		t.Fatalf("tree = %v", paths(result.Tree))
	}
}

func TestAFileTheOtherSideRenamedIntoAMovedDirectoryFollowsIt(t *testing.T) {
	s := newStore()
	a, x := s.blob(t, tenLines("a")), tenLines("x")
	base := Snapshot{"lib/a": a, "old/x": s.blob(t, x)}
	oursX := s.blob(t, replaceLine(x, 0, "OURS"))
	ours := Snapshot{"src/a": a, "old/x": oursX}
	theirX := s.blob(t, replaceLine(x, 9, "THEIRS"))
	theirs := Snapshot{"lib/a": a, "lib/x": theirX}

	result := directoryMerge(t, s, DirectoryRenamesConflict, base, ours, theirs, Renames{"lib/a": "src/a"}, Renames{"old/x": "lib/x"})

	if !slices.Equal(paths(result.Tree), []string{"src/a", "src/x"}) {
		t.Fatalf("tree = %v", paths(result.Tree))
	}
	c := onlyConflict(t, result)
	if c.Path != "src/x" || c.Kind != ConflictFileLocation || c.Base == nil || !sameEntry(c.Ours, oursX) || !sameEntry(c.Theirs, theirX) {
		t.Fatalf("conflict = %+v", c)
	}
	if got := s.text(t, result.Tree["src/x"]); got != replaceLine(replaceLine(x, 0, "OURS"), 9, "THEIRS") {
		t.Fatalf("merged = %q", got)
	}
}

func TestDirectoryRenameCountsClimbWhileTheNamesMatch(t *testing.T) {
	base := Snapshot{"top/lib/a": {}, "top/lib/b": {}, "top/t": {}, "flat/c": {}, "flat/f": {}, "same/d": {}, "a/b/c/x": {}, "a/y": {}, "quiet/q": {}}
	side := Snapshot{"up/lib/a": {}, "up/lib/b": {}, "up/t": {}, "c": {}, "f": {}, "same/e": {}, "z/b/c/x": {}, "z/y": {}, "loud/q": {}}
	adder := Snapshot{"top/new": {}, "flat/new": {}, "a/new": {}, "quiet/deep/new": {}}
	renames := Renames{
		"top/lib/a": "up/lib/a", "top/lib/b": "up/lib/b", "top/t": "up/t",
		"flat/c": "c", "flat/f": "f", "same/d": "same/e",
		"a/b/c/x": "z/b/c/x", "a/y": "z/y", "quiet/q": "loud/q",
	}

	got := directoryRenamesOf(renames, base, side, adder)

	want := map[string]string{"top/lib": "up/lib", "top": "up", "flat": "", "a/b/c": "z/b/c", "a": "z"}
	if !maps.Equal(got, want) {
		t.Fatalf("directory renames = %v, want %v", got, want)
	}
}

func TestAFileAddedInADirectoryMovedToTheTopFollowsIt(t *testing.T) {
	s := newStore()
	a, added := s.blob(t, tenLines("a")), s.blob(t, "new\n")

	result := directoryMerge(t, s, DirectoryRenamesApply, Snapshot{"lib/a": a}, Snapshot{"a": a}, Snapshot{"lib/a": a, "lib/new": added}, Renames{"lib/a": "a"}, nil)

	if !result.Clean() || !slices.Equal(paths(result.Tree), []string{"a", "new"}) {
		t.Fatalf("result = %+v", result)
	}
}
