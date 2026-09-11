package merge

import (
	"errors"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
)

var errStore = errors.New("merge: store failed")

type memoryStore struct {
	objects map[hash.ObjectID]memoryObject
	failGet hash.ObjectID
	failPut bool
}

type memoryObject struct {
	kind object.Type
	data []byte
}

func newStore() *memoryStore {
	return &memoryStore{objects: map[hash.ObjectID]memoryObject{}}
}

func (m *memoryStore) Get(id hash.ObjectID) (object.Type, []byte, error) {
	if id == m.failGet {
		return 0, nil, errStore
	}
	obj, ok := m.objects[id]
	if !ok {
		return 0, nil, errStore
	}
	return obj.kind, obj.data, nil
}

func (m *memoryStore) Put(kind object.Type, data []byte) (hash.ObjectID, error) {
	if m.failPut {
		return hash.Zero, errStore
	}
	id := hash.SumSHA1(kind.String(), data)
	m.objects[id] = memoryObject{kind: kind, data: data}
	return id, nil
}

func (m *memoryStore) Tree(id hash.ObjectID) (*object.Tree, error) {
	kind, data, err := m.Get(id)
	if err != nil {
		return nil, err
	}
	if kind != object.TypeTree {
		return nil, errStore
	}
	return object.ParseTree(data)
}

func (m *memoryStore) blob(t *testing.T, content string) Entry {
	t.Helper()
	id, err := m.Put(object.TypeBlob, []byte(content))
	if err != nil {
		t.Fatal(err)
	}
	return Entry{Mode: object.ModeBlob, ID: id}
}

func (m *memoryStore) text(t *testing.T, entry Entry) string {
	t.Helper()
	_, data, err := m.Get(entry.ID)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func mergeOf(t *testing.T, store *memoryStore, base, ours, theirs Snapshot) TreeResult {
	t.Helper()
	result, err := Trees(base, ours, theirs, store, TreeOptions{File: Options{Labels: Labels{Ours: "ours", Theirs: "theirs"}}})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func onlyConflict(t *testing.T, result TreeResult) Conflict {
	t.Helper()
	if len(result.Conflicts) != 1 {
		t.Fatalf("conflicts = %+v, want exactly one", result.Conflicts)
	}
	return result.Conflicts[0]
}

func TestAChangeOnOneSideOfATreeIsTaken(t *testing.T) {
	s := newStore()
	a, b := s.blob(t, "a\n"), s.blob(t, "b\n")

	result := mergeOf(t, s, Snapshot{"f": a, "g": a}, Snapshot{"f": b, "g": a}, Snapshot{"f": a})

	if !result.Clean() || result.Tree["f"] != b {
		t.Fatalf("result = %+v, want our change", result)
	}
	if _, kept := result.Tree["g"]; kept {
		t.Fatal("a file their side deleted and ours left alone must go")
	}
}

func TestEditsInDifferentPlacesOfOneFileAreMergedIntoANewBlob(t *testing.T) {
	s := newStore()
	base := s.blob(t, "1\n2\n3\n4\n5\n6\n7\n")
	ours := s.blob(t, "ONE\n2\n3\n4\n5\n6\n7\n")
	theirs := s.blob(t, "1\n2\n3\n4\n5\n6\nSEVEN\n")

	result := mergeOf(t, s, Snapshot{"f": base}, Snapshot{"f": ours}, Snapshot{"f": theirs})

	if !result.Clean() {
		t.Fatalf("conflicts = %+v, want a clean merge", result.Conflicts)
	}
	if got := s.text(t, result.Tree["f"]); got != "ONE\n2\n3\n4\n5\n6\nSEVEN\n" {
		t.Fatalf("merged = %q", got)
	}
}

func TestEditsOfTheSameLineConflictAndLeaveMarkersInTheTree(t *testing.T) {
	s := newStore()
	base, ours, theirs := s.blob(t, "a\nb\nc\n"), s.blob(t, "a\nOURS\nc\n"), s.blob(t, "a\nTHEIRS\nc\n")

	result := mergeOf(t, s, Snapshot{"f": base}, Snapshot{"f": ours}, Snapshot{"f": theirs})

	c := onlyConflict(t, result)
	if c.Kind != ConflictContent || *c.Base != base || *c.Ours != ours || *c.Theirs != theirs {
		t.Fatalf("conflict = %+v, want a content conflict with all three sides", c)
	}
	if got := s.text(t, result.Tree["f"]); got != "a\n<<<<<<< ours\nOURS\n=======\nTHEIRS\n>>>>>>> theirs\nc\n" {
		t.Fatalf("tree file = %q, want the markers git writes", got)
	}
}

func TestFilesAddedDifferentlyConflictAgainstAnEmptyBase(t *testing.T) {
	s := newStore()

	result := mergeOf(t, s, Snapshot{}, Snapshot{"f": s.blob(t, "ours\n")}, Snapshot{"f": s.blob(t, "theirs\n")})

	if c := onlyConflict(t, result); c.Kind != ConflictAddAdd || c.Base != nil {
		t.Fatalf("conflict = %+v, want add/add", c)
	}
}

func TestDeletingAFileTheOtherSideChangedConflicts(t *testing.T) {
	s := newStore()
	base, changed := s.blob(t, "a\n"), s.blob(t, "changed\n")

	ourDelete := mergeOf(t, s, Snapshot{"f": base}, Snapshot{}, Snapshot{"f": changed})
	theirDelete := mergeOf(t, s, Snapshot{"f": base}, Snapshot{"f": changed}, Snapshot{})

	if c := onlyConflict(t, ourDelete); c.Kind != ConflictDeleteModify || ourDelete.Tree["f"] != changed {
		t.Fatalf("result = %+v, want delete/modify keeping their version", ourDelete)
	}
	if c := onlyConflict(t, theirDelete); c.Kind != ConflictModifyDelete || theirDelete.Tree["f"] != changed {
		t.Fatalf("result = %+v, want modify/delete keeping our version", theirDelete)
	}
}

func TestAModeChangeOnOneSideSurvivesAContentChangeOnTheOther(t *testing.T) {
	s := newStore()
	base := s.blob(t, "1\n2\n3\n")
	executable := Entry{Mode: object.ModeExecutable, ID: base.ID}
	edited := s.blob(t, "1\n2\nTHREE\n")

	result := mergeOf(t, s, Snapshot{"run": base}, Snapshot{"run": executable}, Snapshot{"run": edited})

	if !result.Clean() || result.Tree["run"].Mode != object.ModeExecutable || result.Tree["run"].ID != edited.ID {
		t.Fatalf("result = %+v, want their content with our mode", result)
	}
}

func TestTheSameContentWithModesChangedApartConflicts(t *testing.T) {
	s := newStore()
	content := s.blob(t, "x\n")

	result := mergeOf(t, s, Snapshot{},
		Snapshot{"f": {Mode: object.ModeExecutable, ID: content.ID}},
		Snapshot{"f": content})

	if c := onlyConflict(t, result); c.Kind != ConflictMode {
		t.Fatalf("conflict = %+v, want a mode conflict", c)
	}
}

func TestAFileAgainstALinkConflictsOnTheMode(t *testing.T) {
	s := newStore()
	base := s.blob(t, "1\n2\n3\n4\n5\n6\n7\n")

	result := mergeOf(t, s, Snapshot{"f": base},
		Snapshot{"f": {Mode: object.ModeExecutable, ID: s.blob(t, "ONE\n2\n3\n4\n5\n6\n7\n").ID}},
		Snapshot{"f": {Mode: object.ModeSymlink, ID: s.blob(t, "1\n2\n3\n4\n5\n6\nSEVEN\n").ID}})
	if c := onlyConflict(t, result); c.Kind != ConflictMode {
		t.Fatalf("conflict = %+v, want a mode conflict between a file and a link", c)
	}
}

func TestAMergeThatIsCleanInTextButNotInModeReportsTheMode(t *testing.T) {
	s := newStore()
	base := s.blob(t, "1\n2\n3\n4\n5\n6\n7\n")

	result := mergeOf(t, s,
		Snapshot{"f": {Mode: object.ModeBlob, ID: base.ID}},
		Snapshot{"f": {Mode: object.ModeExecutable, ID: s.blob(t, "ONE\n2\n3\n4\n5\n6\n7\n").ID}},
		Snapshot{"f": {Mode: 0o100664, ID: s.blob(t, "1\n2\n3\n4\n5\n6\nSEVEN\n").ID}})

	if c := onlyConflict(t, result); c.Kind != ConflictMode {
		t.Fatalf("conflict = %+v, want the mode reported", c)
	}
	if got := s.text(t, result.Tree["f"]); got != "ONE\n2\n3\n4\n5\n6\nSEVEN\n" {
		t.Fatalf("content = %q, want the text merged anyway", got)
	}
}

func TestBinaryFilesChangedOnBothSidesKeepOurs(t *testing.T) {
	s := newStore()
	base, ours, theirs := s.blob(t, "a\x00b"), s.blob(t, "a\x00c"), s.blob(t, "a\x00d")

	result := mergeOf(t, s, Snapshot{"bin": base}, Snapshot{"bin": ours}, Snapshot{"bin": theirs})

	if c := onlyConflict(t, result); c.Kind != ConflictBinary || result.Tree["bin"] != ours {
		t.Fatalf("result = %+v, want a binary conflict keeping ours", result)
	}
}

func TestLinksChangedOnBothSidesKeepOurs(t *testing.T) {
	s := newStore()
	link := func(target string) Entry { return Entry{Mode: object.ModeSymlink, ID: s.blob(t, target).ID} }
	ours := link("ours")

	result := mergeOf(t, s, Snapshot{"l": link("base")}, Snapshot{"l": ours}, Snapshot{"l": link("theirs")})

	if c := onlyConflict(t, result); c.Kind != ConflictSymlink || result.Tree["l"] != ours {
		t.Fatalf("result = %+v, want a link conflict keeping ours", result)
	}
}

func TestSubmodulesMovedApartConflict(t *testing.T) {
	s := newStore()
	module := func(seed string) Entry {
		return Entry{Mode: object.ModeSubmodule, ID: hash.SumSHA1("commit", []byte(seed))}
	}

	result := mergeOf(t, s, Snapshot{"m": module("base")}, Snapshot{"m": module("ours")}, Snapshot{"m": module("theirs")})

	if c := onlyConflict(t, result); c.Kind != ConflictSubmodule {
		t.Fatalf("conflict = %+v, want a submodule conflict", c)
	}
}

func TestAFileInTheWayOfADirectoryIsMovedAside(t *testing.T) {
	s := newStore()
	file, inside := s.blob(t, "file\n"), s.blob(t, "inside\n")

	ours := mergeOf(t, s, Snapshot{}, Snapshot{"d": file}, Snapshot{"d/x": inside})
	theirs := mergeOf(t, s, Snapshot{}, Snapshot{"d/x": inside}, Snapshot{"d": file})

	if c := onlyConflict(t, ours); c.Path != "d~ours" || c.Ours == nil || ours.Tree["d~ours"] != file || ours.Tree["d/x"] != inside {
		t.Fatalf("result = %+v, want our file moved to d~ours", ours)
	}
	if c := onlyConflict(t, theirs); c.Path != "d~theirs" || c.Theirs == nil || theirs.Tree["d~theirs"] != file {
		t.Fatalf("result = %+v, want their file moved to d~theirs", theirs)
	}
}

func TestAnObjectThatCannotBeReadStopsTheMerge(t *testing.T) {
	for name, pick := range map[string]func(base, ours, theirs Entry) hash.ObjectID{
		"base":   func(base, _, _ Entry) hash.ObjectID { return base.ID },
		"ours":   func(_, ours, _ Entry) hash.ObjectID { return ours.ID },
		"theirs": func(_, _, theirs Entry) hash.ObjectID { return theirs.ID },
	} {
		t.Run(name, func(t *testing.T) {
			s := newStore()
			base, ours, theirs := s.blob(t, "a\n"), s.blob(t, "b\n"), s.blob(t, "c\n")
			s.failGet = pick(base, ours, theirs)

			_, err := Trees(Snapshot{"f": base}, Snapshot{"f": ours}, Snapshot{"f": theirs}, s, TreeOptions{})

			if !errors.Is(err, errStore) {
				t.Fatalf("err = %v, want the store failure", err)
			}
		})
	}
}

func TestATreeWhereABlobShouldBeIsRefused(t *testing.T) {
	s := newStore()
	tree, err := s.Put(object.TypeTree, nil)
	if err != nil {
		t.Fatal(err)
	}
	fake := Entry{Mode: object.ModeBlob, ID: tree}

	_, err = Trees(Snapshot{"f": s.blob(t, "a\n")}, Snapshot{"f": fake}, Snapshot{"f": s.blob(t, "c\n")}, s, TreeOptions{})

	if !errors.Is(err, ErrNotABlob) {
		t.Fatalf("err = %v, want ErrNotABlob", err)
	}
}

func TestAMergedBlobThatCannotBeStoredStopsTheMerge(t *testing.T) {
	s := newStore()
	base, ours, theirs := s.blob(t, "a\n"), s.blob(t, "b\n"), s.blob(t, "c\n")
	s.failPut = true

	if _, err := Trees(Snapshot{"f": base}, Snapshot{"f": ours}, Snapshot{"f": theirs}, s, TreeOptions{}); !errors.Is(err, errStore) {
		t.Fatalf("err = %v, want the store failure", err)
	}
}

func TestASnapshotSurvivesATripThroughTrees(t *testing.T) {
	s := newStore()
	want := Snapshot{
		"a":       s.blob(t, "a\n"),
		"d/b":     s.blob(t, "b\n"),
		"d/e/c":   s.blob(t, "c\n"),
		"d.txt":   s.blob(t, "dot\n"),
		"run.sh":  {Mode: object.ModeExecutable, ID: s.blob(t, "run\n").ID},
		"x/y/z/w": s.blob(t, "deep\n"),
	}

	root, err := want.Write(s)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Read(s, root)
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != len(want) {
		t.Fatalf("snapshot = %v, want %v", got, want)
	}
	for path, entry := range want {
		if got[path] != entry {
			t.Fatalf("%s = %+v, want %+v", path, got[path], entry)
		}
	}
}

func TestAnEmptyTreeIsAnEmptySnapshot(t *testing.T) {
	got, err := Read(newStore(), hash.Zero)

	if err != nil || len(got) != 0 {
		t.Fatalf("snapshot = %v, err = %v, want nothing", got, err)
	}
}

func TestReadingATreeThatIsNotThereFails(t *testing.T) {
	s := newStore()
	root, err := Snapshot{"d/f": s.blob(t, "f\n")}.Write(s)
	if err != nil {
		t.Fatal(err)
	}
	tree, err := s.Tree(root)
	if err != nil {
		t.Fatal(err)
	}
	s.failGet = tree.Entries[0].ID

	if _, err := Read(s, root); !errors.Is(err, errStore) {
		t.Fatalf("err = %v, want the missing subtree reported", err)
	}
	s.failGet = root
	if _, err := Read(s, root); !errors.Is(err, errStore) {
		t.Fatalf("err = %v, want the missing root reported", err)
	}
}

func TestATreeThatCannotBeStoredFailsTheWrite(t *testing.T) {
	s := newStore()
	snapshot := Snapshot{"d/f": s.blob(t, "f\n")}
	s.failPut = true

	if _, err := snapshot.Write(s); !errors.Is(err, errStore) {
		t.Fatalf("err = %v, want the store failure", err)
	}
}

func TestTheSameChangeOnBothSidesOfATreeIsTakenOnce(t *testing.T) {
	s := newStore()
	base, both := s.blob(t, "a\n"), s.blob(t, "b\n")

	result := mergeOf(t, s, Snapshot{"f": base}, Snapshot{"f": both}, Snapshot{"f": both})

	if !result.Clean() || result.Tree["f"] != both {
		t.Fatalf("result = %+v, want the change once", result)
	}
}

func TestTheSameNewContentTakesTheModeOnlyOneSideChanged(t *testing.T) {
	s := newStore()
	old, both := s.blob(t, "old\n"), s.blob(t, "new\n")

	result := mergeOf(t, s,
		Snapshot{"f": old},
		Snapshot{"f": {Mode: object.ModeExecutable, ID: both.ID}},
		Snapshot{"f": both})

	if !result.Clean() || result.Tree["f"] != (Entry{Mode: object.ModeExecutable, ID: both.ID}) {
		t.Fatalf("result = %+v, want the shared content with our mode", result)
	}
}

func TestTheirModeIsTakenWhenOursKeptTheBase(t *testing.T) {
	s := newStore()
	base := s.blob(t, "1\n2\n3\n")
	edited := s.blob(t, "1\n2\nTHREE\n")

	result := mergeOf(t, s,
		Snapshot{"f": base},
		Snapshot{"f": edited},
		Snapshot{"f": {Mode: object.ModeExecutable, ID: base.ID}})

	if !result.Clean() || result.Tree["f"] != (Entry{Mode: object.ModeExecutable, ID: edited.ID}) {
		t.Fatalf("result = %+v, want our content with their mode", result)
	}
}
