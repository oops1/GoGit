package merge

import (
	"slices"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
)

func submoduleAt(seed string) Entry {
	return Entry{Mode: object.ModeSubmodule, ID: hash.SumSHA1("commit", []byte(seed))}
}

func conflictAt(t *testing.T, result TreeResult, path string) Conflict {
	t.Helper()
	at := slices.IndexFunc(result.Conflicts, func(c Conflict) bool { return c.Path == path })
	if at < 0 {
		t.Fatalf("no conflict at %s in %+v", path, result.Conflicts)
	}
	c := result.Conflicts[at]
	if c.Kind != ConflictDistinctTypes {
		t.Fatalf("conflict at %s = %+v, want distinct types", path, c)
	}
	return c
}

func sameEntry(a *Entry, b Entry) bool { return a != nil && *a == b }

func TestAFileAgainstASubmoduleMovesTheFileAside(t *testing.T) {
	s := newStore()
	file, module := s.blob(t, "file\n"), submoduleAt("module")

	result := mergeOf(t, s, Snapshot{}, Snapshot{"p": file}, Snapshot{"p": module})

	if len(result.Conflicts) != 2 || result.Tree["p~ours"] != file || result.Tree["p"] != module {
		t.Fatalf("result = %+v", result)
	}
	if c := conflictAt(t, result, "p~ours"); !sameEntry(c.Ours, file) || c.Base != nil || c.Theirs != nil {
		t.Fatalf("p~ours = %+v", c)
	}
	if c := conflictAt(t, result, "p"); !sameEntry(c.Theirs, module) || c.Base != nil || c.Ours != nil {
		t.Fatalf("p = %+v", c)
	}
}

func TestASubmoduleAgainstAFileMovesTheirFileAside(t *testing.T) {
	s := newStore()
	file, module := s.blob(t, "file\n"), submoduleAt("module")

	result := mergeOf(t, s, Snapshot{}, Snapshot{"p": module}, Snapshot{"p": file})

	if result.Tree["p"] != module || result.Tree["p~theirs"] != file {
		t.Fatalf("result = %+v", result)
	}
	conflictAt(t, result, "p")
	conflictAt(t, result, "p~theirs")
}

func TestAnEditedFileAgainstASubmoduleKeepsTheBaseWithTheFile(t *testing.T) {
	s := newStore()
	base, edited, module := s.blob(t, "base\n"), s.blob(t, "edited\n"), submoduleAt("module")

	result := mergeOf(t, s, Snapshot{"q": base}, Snapshot{"q": edited}, Snapshot{"q": module})

	if c := conflictAt(t, result, "q~ours"); !sameEntry(c.Base, base) || !sameEntry(c.Ours, edited) {
		t.Fatalf("q~ours = %+v", c)
	}
	if c := conflictAt(t, result, "q"); c.Base != nil || !sameEntry(c.Theirs, module) {
		t.Fatalf("q = %+v", c)
	}
}

func TestASubmoduleAgainstALinkMovesBothAside(t *testing.T) {
	s := newStore()
	module, link := submoduleAt("module"), Entry{Mode: object.ModeSymlink, ID: s.blob(t, "target").ID}

	result := mergeOf(t, s, Snapshot{}, Snapshot{"s": module}, Snapshot{"s": link})

	if _, kept := result.Tree["s"]; kept || result.Tree["s~ours"] != module || result.Tree["s~theirs"] != link {
		t.Fatalf("result = %+v", result)
	}
}

func TestTheAsideNameSkipsFilesAndDirectoriesThatExist(t *testing.T) {
	s := newStore()
	file, module, taken := s.blob(t, "file\n"), submoduleAt("module"), s.blob(t, "taken\n")
	base := Snapshot{"p~ours": taken, "p~ours_0/x": taken}

	result := mergeOf(t, s, base, Snapshot{"p~ours": taken, "p~ours_0/x": taken, "p": file}, Snapshot{"p~ours": taken, "p~ours_0/x": taken, "p": module})

	if result.Tree["p~ours_1"] != file || result.Tree["p~ours"] != taken {
		t.Fatalf("result = %+v", result)
	}
}

func TestATypeChangeOnOneSideOnlyIsTakenCleanly(t *testing.T) {
	s := newStore()
	file, module := s.blob(t, "file\n"), submoduleAt("module")

	result := mergeOf(t, s, Snapshot{"p": module}, Snapshot{"p": module}, Snapshot{"p": file})

	if !result.Clean() || result.Tree["p"] != file {
		t.Fatalf("result = %+v", result)
	}
}
