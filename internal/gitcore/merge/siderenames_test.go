package merge

import (
	"errors"
	"maps"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
)

type countingObjects struct {
	*memoryStore
	reads map[hash.ObjectID]int
}

func (c *countingObjects) Get(id hash.ObjectID) (object.Type, []byte, error) {
	c.reads[id]++
	return c.memoryStore.Get(id)
}

func writeSnapshot(t *testing.T, s *memoryStore, snapshot Snapshot) hash.ObjectID {
	t.Helper()
	id, err := snapshot.Write(s)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func sideRenames(t *testing.T, s Objects, base, ours, theirs hash.ObjectID, opts RenameOptions) SideRenames {
	t.Helper()
	detected, err := DetectSideRenames(t.Context(), s, base, ours, theirs, opts)
	if err != nil {
		t.Fatal(err)
	}
	return detected
}

func TestDetectSideRenamesReadsEachObjectOnce(t *testing.T) {
	s := newStore()
	moved := tenLines("moved")
	base := writeSnapshot(t, s, Snapshot{"a": s.blob(t, moved), "k": s.blob(t, "k\n")})
	ours := writeSnapshot(t, s, Snapshot{"ours/a": s.blob(t, moved+"ours\n"), "k": s.blob(t, "k\n")})
	theirs := writeSnapshot(t, s, Snapshot{"theirs/a": s.blob(t, moved+"theirs\n"), "k": s.blob(t, "k\n")})
	counting := &countingObjects{memoryStore: s, reads: map[hash.ObjectID]int{}}

	detected := sideRenames(t, counting, base, ours, theirs, RenameOptions{Limit: DefaultRenameLimit})

	if !maps.Equal(detected.Ours, Renames{"a": "ours/a"}) || !maps.Equal(detected.Theirs, Renames{"a": "theirs/a"}) || detected.NeededLimit != 0 {
		t.Fatalf("renames = %+v", detected)
	}
	for id, reads := range counting.reads {
		if reads != 1 {
			t.Errorf("object %s was read %d times", id, reads)
		}
	}
}

func TestDetectSideRenamesStopsAtAnUnreadableSide(t *testing.T) {
	s := newStore()
	file := s.blob(t, tenLines("a"))
	base := writeSnapshot(t, s, Snapshot{"a": file})
	moved := writeSnapshot(t, s, Snapshot{"b": s.blob(t, tenLines("a")+"moved\n")})
	missing := hash.ObjectID{1}
	for name, trees := range map[string][3]hash.ObjectID{
		"base":   {missing, base, base},
		"ours":   {base, missing, base},
		"theirs": {base, base, missing},
	} {
		if _, err := DetectSideRenames(t.Context(), s, trees[0], trees[1], trees[2], RenameOptions{}); !errors.Is(err, errStore) {
			t.Errorf("an unreadable %s returned %v", name, err)
		}
	}
	s.failGet = file.ID
	if _, err := DetectSideRenames(t.Context(), s, base, moved, moved, RenameOptions{}); !errors.Is(err, errStore) {
		t.Errorf("an unreadable source blob returned %v", err)
	}
}

func TestDetectRenamesLeavesEmptyFilesUnpaired(t *testing.T) {
	s := newStore()
	empty := s.blob(t, "")
	base := writeSnapshot(t, s, Snapshot{"old/__init__.py": empty, "k": s.blob(t, tenLines("k"))})
	side := writeSnapshot(t, s, Snapshot{"new/__init__.py": empty, "k": s.blob(t, tenLines("k"))})

	renames, err := DetectRenames(t.Context(), s, base, side)

	if err != nil || len(renames) != 0 {
		t.Fatalf("renames = %v, %v; want none for an empty file", renames, err)
	}
}

func TestSideRenamesSkipSourcesTheOtherSideLeftAlone(t *testing.T) {
	s := newStore()
	a, keep := tenLines("a"), s.blob(t, "k\n")
	base := writeSnapshot(t, s, Snapshot{"a": s.blob(t, a), "k": keep})
	ours := writeSnapshot(t, s, Snapshot{"b": s.blob(t, a+"ours\n"), "k": keep})
	untouched := writeSnapshot(t, s, Snapshot{"a": s.blob(t, a), "k": s.blob(t, "theirs\n")})
	edited := writeSnapshot(t, s, Snapshot{"a": s.blob(t, replaceLine(a, 0, "THEIRS")), "k": keep})

	if detected := sideRenames(t, s, base, ours, untouched, RenameOptions{}); len(detected.Ours) != 0 {
		t.Fatalf("renames against an untouched source = %+v", detected)
	}
	if detected := sideRenames(t, s, base, ours, edited, RenameOptions{}); !maps.Equal(detected.Ours, Renames{"a": "b"}) {
		t.Fatalf("renames against an edited source = %+v", detected)
	}
}

func TestSideRenamesLookForMovedDirectoriesOnlyWhenAsked(t *testing.T) {
	s := newStore()
	a, b := tenLines("a"), tenLines("b")
	base := writeSnapshot(t, s, Snapshot{"lib/a": s.blob(t, a), "lib/b": s.blob(t, b), "top": s.blob(t, "top\n")})
	ours := writeSnapshot(t, s, Snapshot{"src/a": s.blob(t, a+"ours\n"), "src/b": s.blob(t, b+"ours\n"), "top": s.blob(t, "top\n")})
	theirs := writeSnapshot(t, s, Snapshot{"lib/a": s.blob(t, a), "lib/b": s.blob(t, b), "lib/new": s.blob(t, "new\n"), "top": s.blob(t, "top\n")})

	off := sideRenames(t, s, base, ours, theirs, RenameOptions{})
	on := sideRenames(t, s, base, ours, theirs, RenameOptions{DirectoryRenames: true})

	if len(off.Ours) != 0 || !maps.Equal(on.Ours, Renames{"lib/a": "src/a", "lib/b": "src/b"}) {
		t.Fatalf("off = %+v, on = %+v", off, on)
	}
}

func TestSideRenamesReportTheLimitTheyNeeded(t *testing.T) {
	s := newStore()
	base, ours, theirs := Snapshot{}, Snapshot{}, Snapshot{}
	for _, name := range []string{"a", "b", "c"} {
		text := tenLines(name)
		base[name] = s.blob(t, text)
		ours["moved/"+name+"2"] = s.blob(t, text+"ours\n")
		theirs[name] = s.blob(t, strings.Replace(text, "line 0", "LINE 0", 1))
	}

	detected := sideRenames(t, s, writeSnapshot(t, s, base), writeSnapshot(t, s, ours), writeSnapshot(t, s, theirs), RenameOptions{Limit: 2})

	if len(detected.Ours) != 0 || detected.NeededLimit != 3 {
		t.Fatalf("detected = %+v, want no renames and a needed limit of 3", detected)
	}
}
