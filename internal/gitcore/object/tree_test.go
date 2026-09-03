package object_test

import (
	"bytes"
	"errors"
	"slices"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
)

func TestParseTreeReadsEveryModeGitCanStore(t *testing.T) {
	tree := namedFixture(t, "tree_root").tree(t)
	want := []struct {
		mode object.Mode
		name string
	}{
		{object.ModeBlob, "a.b"},
		{object.ModeTree, "a"},
		{object.ModeExecutable, "exe"},
		{object.ModeBlob, "file"},
		{object.ModeSymlink, "link"},
		{object.ModeSubmodule, "mod"},
		{object.ModeTree, "sub"},
	}
	if len(tree.Entries) != len(want) {
		t.Fatalf("parsed %d entries, want %d", len(tree.Entries), len(want))
	}
	for index, entry := range tree.Entries {
		if entry.Mode != want[index].mode || entry.Name != want[index].name {
			t.Fatalf("entry %d = %s %q, want %s %q",
				index, entry.Mode, entry.Name, want[index].mode, want[index].name)
		}
		if entry.ID.IsZero() {
			t.Fatalf("entry %q has a zero object id", entry.Name)
		}
	}
}

func TestParseTreeAcceptsAnEmptyTree(t *testing.T) {
	f := namedFixture(t, "tree_empty")
	tree := f.tree(t)
	if len(tree.Entries) != 0 {
		t.Fatalf("empty tree parsed %d entries", len(tree.Entries))
	}
	if tree.ID() != f.id || len(tree.Encode()) != 0 {
		t.Fatalf("empty tree encodes to %q with id %s", tree.Encode(), tree.ID())
	}
}

func TestParseTreeKeepsNonAsciiAndSpacedNames(t *testing.T) {
	tree := namedFixture(t, "tree_unicode").tree(t)
	names := make([]string, 0, len(tree.Entries))
	for _, entry := range tree.Entries {
		names = append(names, entry.Name)
	}
	if !slices.Equal(names, []string{"z z z.txt", "файл.txt"}) {
		t.Fatalf("names = %q", names)
	}
}

func TestTreeFindLocatesEntriesByName(t *testing.T) {
	tree := namedFixture(t, "tree_root").tree(t)
	entry, ok := tree.Find("link")
	if !ok || entry.Mode != object.ModeSymlink {
		t.Fatalf("Find(link) = %v, %v", entry, ok)
	}
	if _, ok := tree.Find("absent"); ok {
		t.Fatal("Find reported a missing entry as present")
	}
}

func TestSortComparesDirectoriesWithATrailingSlash(t *testing.T) {
	blob := object.TreeEntry{Mode: object.ModeBlob, Name: "a.b"}
	dir := object.TreeEntry{Mode: object.ModeTree, Name: "a"}
	gitlink := object.TreeEntry{Mode: object.ModeSubmodule, Name: "a-mod"}
	tree := &object.Tree{Entries: []object.TreeEntry{dir, gitlink, blob}}
	if tree.IsSorted() {
		t.Fatal("IsSorted() = true for a shuffled tree")
	}
	tree.Sort()
	if !tree.IsSorted() {
		t.Fatal("IsSorted() = false right after Sort()")
	}
	got := []string{tree.Entries[0].Name, tree.Entries[1].Name, tree.Entries[2].Name}
	if !slices.Equal(got, []string{"a-mod", "a.b", "a"}) {
		t.Fatalf("sorted order = %q", got)
	}
	if object.CompareEntries(dir, dir) != 0 {
		t.Fatal("CompareEntries is not reflexive")
	}
}

func TestSortReproducesTheOrderGitWrote(t *testing.T) {
	f := namedFixture(t, "tree_root")
	tree := f.tree(t)
	if !tree.IsSorted() {
		t.Fatal("tree written by git is not sorted by our rule")
	}
	shuffled := &object.Tree{Entries: slices.Clone(tree.Entries)}
	slices.Reverse(shuffled.Entries)
	shuffled.Sort()
	if !bytes.Equal(shuffled.Encode(), f.raw(t)) {
		t.Fatal("sorting a shuffled tree did not restore the bytes git wrote")
	}
}

func TestParseTreeSeqStopsWhenTheCallerBreaks(t *testing.T) {
	raw := namedFixture(t, "tree_root").raw(t)
	seen := 0
	for entry, err := range object.ParseTreeSeq(raw) {
		if err != nil {
			t.Fatalf("iteration error: %v", err)
		}
		seen++
		if entry.Name == "exe" {
			break
		}
	}
	if seen != 3 {
		t.Fatalf("consumed %d entries before breaking, want 3", seen)
	}
}

func TestParseTreeRejectsTruncatedEntries(t *testing.T) {
	id := make([]byte, hash.Size)
	cases := map[string][]byte{
		"no space after the mode": []byte("100644"),
		"unknown mode":            append([]byte("140000 name\x00"), id...),
		"mode is not octal":       append([]byte("1x0644 name\x00"), id...),
		"no name terminator":      []byte("100644 name"),
		"empty name":              append([]byte("100644 \x00"), id...),
		"short object id":         append([]byte("100644 name\x00"), id[:hash.Size-1]...),
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := object.ParseTree(data); err == nil {
				t.Fatal("ParseTree accepted a truncated entry")
			}
		})
	}
}

func TestParseTreeReportsInvalidModeSentinel(t *testing.T) {
	data := append([]byte("140000 name\x00"), make([]byte, hash.Size)...)
	if _, err := object.ParseTree(data); !errors.Is(err, object.ErrInvalidMode) {
		t.Fatalf("err = %v, want ErrInvalidMode", err)
	}
}

func TestParseTreeReportsMalformedSentinel(t *testing.T) {
	if _, err := object.ParseTree([]byte("100644 name")); !errors.Is(err, object.ErrMalformed) {
		t.Fatalf("err = %v, want ErrMalformed", err)
	}
}

func TestModeClassifiesTreeEntries(t *testing.T) {
	cases := []struct {
		mode       object.Mode
		text       string
		tree       bool
		regular    bool
		symlink    bool
		submodule  bool
		objectType object.Type
	}{
		{object.ModeTree, "40000", true, false, false, false, object.TypeTree},
		{object.ModeBlob, "100644", false, true, false, false, object.TypeBlob},
		{object.ModeExecutable, "100755", false, true, false, false, object.TypeBlob},
		{object.ModeSymlink, "120000", false, false, true, false, object.TypeBlob},
		{object.ModeSubmodule, "160000", false, false, false, true, object.TypeCommit},
	}
	for _, c := range cases {
		t.Run(c.text, func(t *testing.T) {
			if c.mode.String() != c.text {
				t.Fatalf("String() = %q, want %q", c.mode.String(), c.text)
			}
			if c.mode.IsTree() != c.tree || c.mode.IsRegular() != c.regular ||
				c.mode.IsSymlink() != c.symlink || c.mode.IsSubmodule() != c.submodule {
				t.Fatalf("classification of %s is wrong", c.text)
			}
			if c.mode.ObjectType() != c.objectType {
				t.Fatalf("ObjectType() = %s, want %s", c.mode.ObjectType(), c.objectType)
			}
		})
	}
}

func TestParseModeAcceptsLegacyGroupWritableBlobs(t *testing.T) {
	mode, err := object.ParseMode("100664")
	if err != nil || mode != object.Mode(0o100664) || !mode.IsRegular() {
		t.Fatalf("ParseMode(100664) = %v, %v", mode, err)
	}
}

func TestParseModeRejectsUnusableValues(t *testing.T) {
	for _, text := range []string{"", "0", "9", "80000", "20000", "140000", "-100644", "1000000000000"} {
		if _, err := object.ParseMode(text); !errors.Is(err, object.ErrInvalidMode) {
			t.Fatalf("ParseMode(%q) err = %v, want ErrInvalidMode", text, err)
		}
	}
}
