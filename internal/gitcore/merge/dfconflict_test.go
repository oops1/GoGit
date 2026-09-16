package merge

import "testing"

func TestAConflictedFileInTheWayOfADirectoryTakesItsStagesAside(t *testing.T) {
	s := newStore()
	base, changed, inside := s.blob(t, "d\n"), s.blob(t, "changed\n"), s.blob(t, "x\n")

	ours := mergeOf(t, s, Snapshot{"d": base}, Snapshot{"d": changed}, Snapshot{"d/x": inside})
	theirs := mergeOf(t, s, Snapshot{"d": base}, Snapshot{"d/x": inside}, Snapshot{"d": changed})

	want := []Conflict{{Path: "d~ours", Kind: ConflictModifyDelete, Base: &base, Ours: &changed}}
	if !sameConflicts(ours.Conflicts, want) || ours.Tree["d~ours"] != changed || ours.Tree["d/x"] != inside {
		t.Fatalf("ours = %+v", ours)
	}
	want = []Conflict{{Path: "d~theirs", Kind: ConflictDeleteModify, Base: &base, Theirs: &changed}}
	if !sameConflicts(theirs.Conflicts, want) || theirs.Tree["d~theirs"] != changed {
		t.Fatalf("theirs = %+v", theirs)
	}
}
