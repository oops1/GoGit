package merge

import (
	"errors"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
)

func tenLines(seed string) string {
	var lines []string
	for i := range 10 {
		lines = append(lines, seed+" line "+string(rune('0'+i))+" long enough to be recognised as the same file")
	}
	return strings.Join(lines, "\n") + "\n"
}

func replaceLine(content string, line int, replacement string) string {
	lines := strings.Split(strings.TrimSuffix(content, "\n"), "\n")
	lines[line] = replacement
	return strings.Join(lines, "\n") + "\n"
}

func renamedMerge(t *testing.T, s *memoryStore, base, ours, theirs Snapshot, ourRenames, theirRenames Renames) TreeResult {
	t.Helper()
	result, err := Trees(base, ours, theirs, s, TreeOptions{
		File:         Options{Labels: Labels{Base: "base", Ours: "ours", Theirs: "theirs"}},
		OurRenames:   ourRenames,
		TheirRenames: theirRenames,
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestDetectRenamesFindsAFileMovedToAnotherPath(t *testing.T) {
	s := newStore()
	file := s.blob(t, tenLines("a"))
	base, err := Snapshot{"a": file, "k": s.blob(t, "k\n")}.Write(s)
	if err != nil {
		t.Fatal(err)
	}
	side, err := Snapshot{"dir/b": file, "k": s.blob(t, "k\n")}.Write(s)
	if err != nil {
		t.Fatal(err)
	}

	renames, err := DetectRenames(t.Context(), s, base, side)

	if err != nil || !maps.Equal(renames, Renames{"a": "dir/b"}) {
		t.Fatalf("renames = %v, %v", renames, err)
	}
}

func TestDetectingRenamesInATreeThatIsNotThereFails(t *testing.T) {
	s := newStore()
	base, side := hash.SumSHA1("tree", []byte("base")), hash.SumSHA1("tree", []byte("side"))

	if _, err := DetectRenames(t.Context(), s, base, side); !errors.Is(err, errStore) {
		t.Fatalf("err = %v, want the store failure", err)
	}
}

func TestARenameTheTreesCannotBackIsRefused(t *testing.T) {
	s := newStore()
	file := s.blob(t, "a\n")
	base, moved := Snapshot{"a": file}, Snapshot{"b": file}
	for name, opts := range map[string]TreeOptions{
		"unknown source":   {OurRenames: Renames{"x": "b"}},
		"missing target":   {TheirRenames: Renames{"a": "x"}},
		"rename to itself": {OurRenames: Renames{"a": "a"}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Trees(base, moved, moved, s, opts); !errors.Is(err, ErrUnknownRename) {
				t.Fatalf("err = %v, want ErrUnknownRename", err)
			}
		})
	}
}

func TestAnEditFollowsTheFileTheOtherSideRenamed(t *testing.T) {
	s := newStore()
	original := tenLines("a")
	base := s.blob(t, original)
	edited := s.blob(t, replaceLine(original, 9, "THEIRS"))

	result := renamedMerge(t, s, Snapshot{"a": base}, Snapshot{"b": base}, Snapshot{"a": edited}, Renames{"a": "b"}, nil)

	if !result.Clean() || len(result.Tree) != 1 || result.Tree["b"] != edited {
		t.Fatalf("result = %+v, want their edit under our new name", result)
	}
}

func TestConflictMarkersNameThePathsEachSideKnowsTheFileBy(t *testing.T) {
	s := newStore()
	original := tenLines("a")
	base := s.blob(t, original)
	ours := s.blob(t, replaceLine(original, 2, "OURS"))
	theirs := s.blob(t, replaceLine(original, 2, "THEIRS"))

	result := renamedMerge(t, s, Snapshot{"a": base}, Snapshot{"a": ours}, Snapshot{"dir/b": theirs}, nil, Renames{"a": "dir/b"})

	c := onlyConflict(t, result)
	text := s.text(t, result.Tree["dir/b"])
	if c.Path != "dir/b" || c.Kind != ConflictContent || !strings.Contains(text, "<<<<<<< ours:a\n") || !strings.Contains(text, ">>>>>>> theirs:dir/b\n") {
		t.Fatalf("conflict = %+v, text:\n%s", c, text)
	}
}

func TestARenameOntoAPathTheOtherSideAddedLeavesBothToTheContentMerge(t *testing.T) {
	s := newStore()
	file := s.blob(t, tenLines("a"))
	added := s.blob(t, tenLines("b"))

	result := renamedMerge(t, s, Snapshot{"a": file}, Snapshot{"b": file}, Snapshot{"a": file, "b": added}, Renames{"a": "b"}, nil)

	c := onlyConflict(t, result)
	if c.Path != "b" || c.Kind != ConflictAddAdd || result.Tree["a"] != (Entry{}) {
		t.Fatalf("result = %+v", result)
	}
}

func TestTheSameRenameOnBothSidesMergesTheEdits(t *testing.T) {
	s := newStore()
	original := tenLines("a")
	base := s.blob(t, original)
	ours := s.blob(t, replaceLine(original, 0, "OURS"))
	theirs := s.blob(t, replaceLine(original, 9, "THEIRS"))

	result := renamedMerge(t, s, Snapshot{"a": base}, Snapshot{"b": ours}, Snapshot{"b": theirs}, Renames{"a": "b"}, Renames{"a": "b"})

	want := replaceLine(replaceLine(original, 0, "OURS"), 9, "THEIRS")
	if !result.Clean() || len(result.Tree) != 1 || s.text(t, result.Tree["b"]) != want {
		t.Fatalf("result = %+v", result)
	}
}

func TestRenamesApartKeepTheFileUnderBothNames(t *testing.T) {
	s := newStore()
	file := s.blob(t, tenLines("a"))

	result := renamedMerge(t, s, Snapshot{"a": file}, Snapshot{"b": file}, Snapshot{"c": file}, Renames{"a": "b"}, Renames{"a": "c"})

	want := []Conflict{
		{Path: "a", Kind: ConflictRenameRename, Base: &file},
		{Path: "b", Kind: ConflictRenameRename, Ours: &file},
		{Path: "c", Kind: ConflictRenameRename, Theirs: &file},
	}
	if len(result.Tree) != 2 || result.Tree["b"] != file || result.Tree["c"] != file || !sameConflicts(result.Conflicts, want) {
		t.Fatalf("result = %+v", result)
	}
}

func TestRenamesApartWithClashingEditsUseLongerMarkers(t *testing.T) {
	s := newStore()
	original := tenLines("a")
	base := s.blob(t, original)
	ours := s.blob(t, replaceLine(original, 0, "OURS"))
	theirs := s.blob(t, replaceLine(original, 0, "THEIRS"))

	result := renamedMerge(t, s, Snapshot{"a": base}, Snapshot{"b": ours}, Snapshot{"c": theirs}, Renames{"a": "b"}, Renames{"a": "c"})

	text := s.text(t, result.Tree["b"])
	if !strings.HasPrefix(text, "<<<<<<<< ours:b\nOURS\n========\nTHEIRS\n>>>>>>>> theirs:c\n") || result.Tree["c"] != result.Tree["b"] {
		t.Fatalf("text:\n%s", text)
	}
}

func TestRenamesApartStopWhenTheContentCannotBeRead(t *testing.T) {
	s := newStore()
	original := tenLines("a")
	base := s.blob(t, original)
	ours := s.blob(t, replaceLine(original, 0, "OURS"))
	theirs := s.blob(t, replaceLine(original, 9, "THEIRS"))
	s.failGet = base.ID

	_, err := Trees(Snapshot{"a": base}, Snapshot{"b": ours}, Snapshot{"c": theirs}, s, TreeOptions{OurRenames: Renames{"a": "b"}, TheirRenames: Renames{"a": "c"}})

	if !errors.Is(err, errStore) {
		t.Fatalf("err = %v, want the store failure", err)
	}
}

func TestARenameOfAFileTheOtherSideDeletedConflictsAtTheNewPath(t *testing.T) {
	s := newStore()
	file := s.blob(t, tenLines("a"))
	keep := s.blob(t, "k\n")
	for name, c := range map[string]struct {
		ours, theirs             Snapshot
		ourRenames, theirRenames Renames
		wantOurs, wantTheirs     *Entry
	}{
		"we renamed":   {ours: Snapshot{"b": file, "k": keep}, theirs: Snapshot{"k": keep}, ourRenames: Renames{"a": "b"}, wantOurs: &file},
		"they renamed": {ours: Snapshot{"k": keep}, theirs: Snapshot{"b": file, "k": keep}, theirRenames: Renames{"a": "b"}, wantTheirs: &file},
	} {
		t.Run(name, func(t *testing.T) {
			result := renamedMerge(t, s, Snapshot{"a": file, "k": keep}, c.ours, c.theirs, c.ourRenames, c.theirRenames)

			want := []Conflict{{Path: "b", Kind: ConflictRenameDelete, Base: &file, Ours: c.wantOurs, Theirs: c.wantTheirs}}
			if len(result.Tree) != 2 || result.Tree["b"] != file || !sameConflicts(result.Conflicts, want) {
				t.Fatalf("result = %+v", result)
			}
		})
	}
}

func sameConflicts(got, want []Conflict) bool {
	return slices.EqualFunc(got, want, func(g, w Conflict) bool {
		return g.Path == w.Path && g.Kind == w.Kind && same(g.Base, w.Base) && same(g.Ours, w.Ours) && same(g.Theirs, w.Theirs)
	})
}
