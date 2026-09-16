package merge

import (
	"errors"
	"strings"
	"testing"
)

func TestTwoFilesRenamedIntoOnePathKeepTheOtherSidesEdits(t *testing.T) {
	s := newStore()
	a, b := tenLines("a"), tenLines("b")
	base := Snapshot{"a": s.blob(t, a), "b": s.blob(t, b)}
	ours := Snapshot{"c": s.blob(t, a), "b": s.blob(t, replaceLine(b, 1, "OURS B"))}
	theirs := Snapshot{"a": s.blob(t, replaceLine(a, 8, "THEIRS A")), "c": s.blob(t, b)}

	result := renamedMerge(t, s, base, ours, theirs, Renames{"a": "c"}, Renames{"b": "c"})

	c := onlyConflict(t, result)
	if c.Path != "c" || c.Kind != ConflictAddAdd || c.Base != nil || len(result.Tree) != 1 {
		t.Fatalf("result = %+v", result)
	}
	if got := s.text(t, *c.Ours); got != replaceLine(a, 8, "THEIRS A") {
		t.Fatalf("stage 2 = %q, want a with their edit", got)
	}
	if got := s.text(t, *c.Theirs); got != replaceLine(b, 1, "OURS B") {
		t.Fatalf("stage 3 = %q, want b with our edit", got)
	}
}

func TestClashingEditsOfARenamedSourceNestLongerMarkers(t *testing.T) {
	s := newStore()
	a, b := tenLines("a"), tenLines("b")
	base := Snapshot{"a": s.blob(t, a), "b": s.blob(t, b)}
	ours := Snapshot{"c": s.blob(t, replaceLine(a, 0, "OURS")), "b": s.blob(t, b)}
	theirs := Snapshot{"a": s.blob(t, replaceLine(a, 0, "THEIRS")), "c": s.blob(t, b)}

	result := renamedMerge(t, s, base, ours, theirs, Renames{"a": "c"}, Renames{"b": "c"})

	c := onlyConflict(t, result)
	if got := s.text(t, *c.Ours); !strings.HasPrefix(got, "<<<<<<<< ours:c\nOURS\n========\nTHEIRS\n>>>>>>>> theirs:a\n") {
		t.Fatalf("stage 2 = %q", got)
	}
}

func TestASourceTheOtherSideDeletedIsTakenAsRenamed(t *testing.T) {
	s := newStore()
	a, b := tenLines("a"), tenLines("b")
	base := Snapshot{"a": s.blob(t, a), "b": s.blob(t, b)}

	oursDeleted := renamedMerge(t, s, base, Snapshot{"c": s.blob(t, a)}, Snapshot{"a": s.blob(t, a), "c": s.blob(t, b)}, Renames{"a": "c"}, Renames{"b": "c"})
	theirsDeleted := renamedMerge(t, s, base, Snapshot{"c": s.blob(t, a), "b": s.blob(t, b)}, Snapshot{"c": s.blob(t, b)}, Renames{"a": "c"}, Renames{"b": "c"})

	for name, result := range map[string]TreeResult{"ours": oursDeleted, "theirs": theirsDeleted} {
		c := onlyConflict(t, result)
		if s.text(t, *c.Ours) != a || s.text(t, *c.Theirs) != b || len(result.Tree) != 1 {
			t.Fatalf("%s deleted: result = %+v", name, result)
		}
	}
}

func TestAnUnreadableSourceStopsATwoIntoOneMerge(t *testing.T) {
	a, b := tenLines("a"), tenLines("b")
	for _, failing := range []string{"a", "b"} {
		s := newStore()
		base := Snapshot{"a": s.blob(t, a), "b": s.blob(t, b)}
		ours := Snapshot{"c": s.blob(t, replaceLine(a, 0, "OURS")), "b": s.blob(t, replaceLine(b, 0, "OURS"))}
		theirs := Snapshot{"a": s.blob(t, replaceLine(a, 9, "THEIRS")), "c": s.blob(t, replaceLine(b, 9, "THEIRS"))}
		s.failGet = base[failing].ID

		_, err := Trees(base, ours, theirs, s, TreeOptions{OurRenames: Renames{"a": "c"}, TheirRenames: Renames{"b": "c"}})

		if !errors.Is(err, errStore) {
			t.Fatalf("unreadable %s: err = %v", failing, err)
		}
	}
}
