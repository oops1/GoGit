package merge

import (
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/object"
)

func virtualMerge(t *testing.T, s *memoryStore, depth int, base, ours, theirs Snapshot) TreeResult {
	t.Helper()
	result, err := Trees(base, ours, theirs, s, TreeOptions{Depth: depth, File: Options{Labels: Labels{Ours: "ours", Theirs: "theirs"}}})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestTheVirtualBaseKeepsTheBaseVersionOfConflictsThatAreNotText(t *testing.T) {
	s := newStore()
	link := func(target string) Entry { return Entry{Mode: object.ModeSymlink, ID: s.blob(t, target).ID} }
	base := s.blob(t, "base\n")
	for name, c := range map[string]struct {
		base, ours, theirs Snapshot
		kind               ConflictKind
		clean              bool
	}{
		"modify against delete": {base: Snapshot{"p": base}, ours: Snapshot{"p": s.blob(t, "ours\n")}, theirs: Snapshot{}, kind: ConflictModifyDelete},
		"delete against modify": {base: Snapshot{"p": base}, ours: Snapshot{}, theirs: Snapshot{"p": s.blob(t, "theirs\n")}, kind: ConflictDeleteModify},
		"binary":                {base: Snapshot{"p": s.blob(t, "base\x00")}, ours: Snapshot{"p": s.blob(t, "ours\x00")}, theirs: Snapshot{"p": s.blob(t, "theirs\x00")}, clean: true},
		"symlink":               {base: Snapshot{"p": link("base")}, ours: Snapshot{"p": link("ours")}, theirs: Snapshot{"p": link("theirs")}, kind: ConflictSymlink},
		"submodule":             {base: Snapshot{"p": submoduleAt("base")}, ours: Snapshot{"p": submoduleAt("ours")}, theirs: Snapshot{"p": submoduleAt("theirs")}, kind: ConflictSubmodule},
		"distinct types":        {base: Snapshot{"p": base}, ours: Snapshot{"p": link("ours")}, theirs: Snapshot{"p": submoduleAt("theirs")}, kind: ConflictDistinctTypes},
	} {
		t.Run(name, func(t *testing.T) {
			outer := virtualMerge(t, s, 0, c.base, c.ours, c.theirs)
			inner := virtualMerge(t, s, 1, c.base, c.ours, c.theirs)

			if outer.Tree["p"] == c.base["p"] || outer.Clean() {
				t.Fatalf("the outer merge kept the base or merged cleanly: %+v", outer)
			}
			if inner.Tree["p"] != c.base["p"] {
				t.Fatalf("inner merge = %+v, want the base version", inner)
			}
			if c.clean != inner.Clean() || !c.clean && inner.Conflicts[0].Kind != c.kind {
				t.Fatalf("inner conflicts = %+v, want clean %v or kind %d", inner.Conflicts, c.clean, c.kind)
			}
		})
	}
}

func TestTheVirtualBaseLeavesOutALinkOrTypeNobodyHadInTheBase(t *testing.T) {
	s := newStore()
	link := func(target string) Entry { return Entry{Mode: object.ModeSymlink, ID: s.blob(t, target).ID} }

	links := virtualMerge(t, s, 1, Snapshot{}, Snapshot{"p": link("ours")}, Snapshot{"p": link("theirs")})
	types := virtualMerge(t, s, 1, Snapshot{}, Snapshot{"p": s.blob(t, "file\n")}, Snapshot{"p": submoduleAt("theirs")})

	if _, kept := links.Tree["p"]; kept || len(links.Conflicts) != 1 {
		t.Fatalf("links = %+v, want the path left out", links)
	}
	if _, kept := types.Tree["p"]; kept || len(types.Conflicts) != 1 {
		t.Fatalf("types = %+v, want the path left out", types)
	}
}

func TestBinaryFilesAddedApartLeaveAnEmptyFileInTheVirtualBase(t *testing.T) {
	s := newStore()

	result := virtualMerge(t, s, 1, Snapshot{}, Snapshot{"p": s.blob(t, "ours\x00")}, Snapshot{"p": s.blob(t, "theirs\x00")})

	if !result.Clean() || s.text(t, result.Tree["p"]) != "" {
		t.Fatalf("result = %+v, want a clean empty file", result)
	}
}

func TestBinaryFilesWithModesChangedApartStillConflictInTheVirtualBase(t *testing.T) {
	s := newStore()
	base := s.blob(t, "base\x00")

	result := virtualMerge(t, s, 1,
		Snapshot{"p": {Mode: 0o100664, ID: base.ID}},
		Snapshot{"p": {Mode: object.ModeExecutable, ID: s.blob(t, "ours\x00").ID}},
		Snapshot{"p": {Mode: object.ModeBlob, ID: s.blob(t, "theirs\x00").ID}})

	if c := onlyConflict(t, result); c.Kind != ConflictMode || result.Tree["p"] != (Entry{Mode: object.ModeExecutable, ID: base.ID}) {
		t.Fatalf("result = %+v, want the base content with our mode", result)
	}
}

func TestVirtualBaseMarkersGrowTwoSignsWithEveryLevel(t *testing.T) {
	s := newStore()
	base, ours, theirs := s.blob(t, "a\nb\nc\n"), s.blob(t, "a\nO\nc\n"), s.blob(t, "a\nT\nc\n")

	result := virtualMerge(t, s, 2, Snapshot{"p": base}, Snapshot{"p": ours}, Snapshot{"p": theirs})

	if text := s.text(t, result.Tree["p"]); !strings.HasPrefix(text, "a\n<<<<<<<<<<< ours\nO\n===========\n") {
		t.Fatalf("text = %q, want eleven-sign markers", text)
	}
}

func TestABaseOfAnotherKindMergesTheFilesTwoWay(t *testing.T) {
	s := newStore()
	link := Entry{Mode: object.ModeSymlink, ID: s.blob(t, "a\nb\nours\n").ID}

	result := virtualMerge(t, s, 0, Snapshot{"p": link}, Snapshot{"p": s.blob(t, "a\nb\nours\n")}, Snapshot{"p": s.blob(t, "a\nb\ntheirs\n")})

	want := "a\nb\n<<<<<<< ours\nours\n=======\ntheirs\n>>>>>>> theirs\n"
	if c := onlyConflict(t, result); c.Kind != ConflictContent || s.text(t, result.Tree["p"]) != want {
		t.Fatalf("result = %+v, text %q", result, s.text(t, result.Tree["p"]))
	}
}

func TestARenameOfAFileTheOtherSideDeletedKeepsTheBaseInTheVirtualBase(t *testing.T) {
	s := newStore()
	original := tenLines("a")
	file, keep := s.blob(t, original), s.blob(t, "k\n")
	renamed := s.blob(t, replaceLine(original, 0, "OURS"))

	result, err := Trees(Snapshot{"a": file, "k": keep}, Snapshot{"b": renamed, "k": keep}, Snapshot{"k": keep}, s, TreeOptions{Depth: 1, OurRenames: Renames{"a": "b"}})

	if err != nil || len(result.Tree) != 2 || result.Tree["b"] != file {
		t.Fatalf("result = %+v, %v, want the base content at the new path", result, err)
	}
}
