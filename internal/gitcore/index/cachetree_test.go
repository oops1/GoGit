package index

import (
	"bytes"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
)

func TestCacheTreeSurvivesEncodingUnchanged(t *testing.T) {
	raw := readFixture(t, basicV2)
	start := bytes.Index(raw, []byte(extCacheTree)) + extensionHeader
	idx := loadFixture(t, basicV2)
	if got := encodeCacheTree(idx.CacheTree); !bytes.Equal(got, raw[start:start+len(got)]) {
		t.Fatal("the cache tree does not encode back to the bytes it was read from")
	}
}

func TestCacheTreeInvalidationFollowsThePath(t *testing.T) {
	idx := loadFixture(t, basicV2)
	idx.Add(blobEntry("lib/deep/other.txt", StageMerged))
	if idx.CacheTree.Valid() {
		t.Fatal("the root stayed valid after an entry was added")
	}
	lib := idx.CacheTree.Lookup("lib")
	if lib == nil || lib.Valid() {
		t.Fatalf("lib is %+v", lib)
	}
	deep := idx.CacheTree.Lookup("lib/deep")
	if deep == nil || deep.Valid() {
		t.Fatalf("lib/deep is %+v", deep)
	}
}

func TestCacheTreeInvalidationDropsTheNamedDirectory(t *testing.T) {
	idx := loadFixture(t, basicV2)
	idx.CacheTree.invalidatePath("lib/deep")
	if idx.CacheTree.Lookup("lib/deep") != nil {
		t.Fatal("the named directory stayed in the cache tree")
	}
	if idx.CacheTree.Lookup("lib") == nil {
		t.Fatal("the parent directory was dropped as well")
	}
}

func TestCacheTreeInvalidationKeepsUnrelatedSubtrees(t *testing.T) {
	idx := loadFixture(t, basicV2)
	deep := idx.CacheTree.Lookup("lib/deep")
	idx.Remove("a.txt")
	if idx.CacheTree.Valid() {
		t.Fatal("the root stayed valid after an entry was removed")
	}
	if again := idx.CacheTree.Lookup("lib/deep"); again != deep || !again.Valid() {
		t.Fatal("an unrelated subtree lost its cached tree")
	}
}

func TestCacheTreeInvalidationIgnoresUnknownDirectories(t *testing.T) {
	idx := loadFixture(t, basicV2)
	idx.Add(blobEntry("other/deep/file.txt", StageMerged))
	if idx.CacheTree.Lookup("lib") == nil {
		t.Fatal("an unrelated subtree was dropped")
	}
	if !idx.CacheTree.Lookup("lib").Valid() {
		t.Fatal("an unrelated subtree was invalidated")
	}
}

func TestCacheTreeInvalidateMarksTheNode(t *testing.T) {
	node := &CacheTree{EntryCount: 3}
	node.Invalidate()
	if node.Valid() {
		t.Fatal("Invalidate left the node valid")
	}
	if node.Find("missing") != nil {
		t.Fatal("Find returned a subtree of an empty node")
	}
}

func TestParseCacheTreeRejectsBrokenData(t *testing.T) {
	valid := encodeCacheTree(&CacheTree{EntryCount: -1})
	cases := []struct {
		name string
		data []byte
		want error
	}{
		{name: "empty", data: nil, want: ErrTruncated},
		{name: "path without a terminator", data: []byte("lib"), want: ErrTruncated},
		{name: "counters without a newline", data: []byte("\x004 1"), want: ErrTruncated},
		{name: "counters without a space", data: []byte("\x0041\n"), want: ErrMalformed},
		{name: "entry count is not a number", data: []byte("\x00x 1\n"), want: ErrMalformed},
		{name: "subtree count is not a number", data: []byte("\x004 x\n"), want: ErrMalformed},
		{name: "negative subtree count", data: []byte("\x004 -1\n"), want: ErrMalformed},
		{name: "object id is missing", data: []byte("\x004 0\n"), want: ErrTruncated},
		{name: "named root", data: []byte("lib\x00-1 0\n"), want: ErrMalformed},
		{name: "trailing bytes", data: append(slices.Clone(valid), 'x'), want: ErrMalformed},
		{name: "missing subtree", data: []byte("\x00-1 1\n"), want: ErrTruncated},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := parseCacheTree(testCase.data)
			if !errors.Is(err, testCase.want) {
				t.Fatalf("parseCacheTree returned %v, want %v", err, testCase.want)
			}
		})
	}
}

func TestParseCacheTreeReadsNestedSubtrees(t *testing.T) {
	source := &CacheTree{EntryCount: 3, ID: idOfByte(1), Subtrees: []*CacheTree{
		{Path: "lib", EntryCount: 2, ID: idOfByte(2), Subtrees: []*CacheTree{
			{Path: "deep", EntryCount: 1, ID: idOfByte(3)},
		}},
		{Path: "other", EntryCount: -1},
	}}
	parsed, err := parseCacheTree(encodeCacheTree(source))
	if err != nil {
		t.Fatalf("parseCacheTree returned error %v", err)
	}
	if parsed.EntryCount != 3 || len(parsed.Subtrees) != 2 {
		t.Fatalf("the root came back as %+v", parsed)
	}
	if parsed.Lookup("lib/deep").ID != idOfByte(3) {
		t.Fatal("the nested subtree lost its object id")
	}
	if parsed.Lookup("other").Valid() {
		t.Fatal("an invalid subtree came back as valid")
	}
	if !bytes.Equal(encodeCacheTree(parsed), encodeCacheTree(source)) {
		t.Fatal("the cache tree does not survive a round trip")
	}
}

func TestSortSubtreesOrdersByLengthThenBytes(t *testing.T) {
	node := &CacheTree{Subtrees: []*CacheTree{{Path: "bbb"}, {Path: "aa"}, {Path: "ab"}, {Path: "c"}}}
	node.sortSubtrees()
	var got []string
	for _, sub := range node.Subtrees {
		got = append(got, sub.Path)
	}
	if !slices.Equal(got, []string{"c", "aa", "ab", "bbb"}) {
		t.Fatalf("sortSubtrees produced %v", got)
	}
}

func TestWriteTreeStoresEveryDirectory(t *testing.T) {
	idx := loadFixture(t, basicV2)
	idx.CacheTree = nil
	objects := newMemoryObjects()
	id, err := idx.WriteTree(objects)
	if err != nil {
		t.Fatalf("WriteTree returned error %v", err)
	}
	if id != mustParseID(t, "991af36dcb875c514ff64265b04dcd2c9fd480b0") {
		t.Fatalf("WriteTree returned %s", id)
	}
	if len(objects.stored) != 3 {
		t.Fatalf("WriteTree stored %d trees, want 3", len(objects.stored))
	}
	if !idx.CacheTree.Valid() || idx.CacheTree.EntryCount != 4 {
		t.Fatalf("the cache tree root is %+v", idx.CacheTree)
	}
	if idx.CacheTree.Lookup("lib/deep") == nil {
		t.Fatal("the cache tree lost the nested directory")
	}
	if idx.extensionAt(extCacheTree) < 0 {
		t.Fatal("WriteTree did not add the cache tree extension")
	}
}

func TestWriteTreeMatchesTheCachedRoot(t *testing.T) {
	idx := loadFixture(t, basicV2)
	cached := idx.CacheTree.ID
	objects := newMemoryObjects()
	id, err := idx.WriteTree(objects)
	if err != nil {
		t.Fatalf("WriteTree returned error %v", err)
	}
	if id != cached {
		t.Fatalf("WriteTree returned %s, the cache tree holds %s", id, cached)
	}
	if len(objects.stored) != 0 {
		t.Fatal("WriteTree wrote objects although the cache tree was valid")
	}
}

func TestWriteTreeReusesValidSubtrees(t *testing.T) {
	idx := loadFixture(t, basicV2)
	idx.Add(blobEntry("a.txt", StageMerged))
	objects := newMemoryObjects()
	if _, err := idx.WriteTree(objects); err != nil {
		t.Fatalf("WriteTree returned error %v", err)
	}
	if len(objects.stored) != 1 {
		t.Fatalf("WriteTree stored %d trees, want only the root", len(objects.stored))
	}
}

func TestWriteTreeWritesTheEmptyTreeForAnEmptyIndex(t *testing.T) {
	objects := newMemoryObjects()
	id, err := New(Version2).WriteTree(objects)
	if err != nil {
		t.Fatalf("WriteTree returned error %v", err)
	}
	if id != mustParseID(t, "4b825dc642cb6eb9a060e54bf8d69288fbee4904") {
		t.Fatalf("WriteTree returned %s", id)
	}
}

func TestWriteTreeRefusesUnmergedEntries(t *testing.T) {
	_, err := loadFixture(t, conflictV2).WriteTree(newMemoryObjects())
	if !errors.Is(err, ErrUnmerged) {
		t.Fatalf("WriteTree returned %v, want %v", err, ErrUnmerged)
	}
}

func TestWriteTreeReportsAFailingObjectStore(t *testing.T) {
	idx := loadFixture(t, basicV2)
	idx.CacheTree = nil
	objects := newMemoryObjects()
	objects.fail = errInjected
	_, err := idx.WriteTree(objects)
	if !errors.Is(err, errInjected) {
		t.Fatalf("WriteTree returned %v, want %v", err, errInjected)
	}
}

func TestWriteTreeRejectsACacheTreeThatOutgrowsTheIndex(t *testing.T) {
	idx := New(Version2)
	idx.Add(blobEntry("a", StageMerged))
	idx.CacheTree = &CacheTree{EntryCount: 9, ID: idOfByte(4)}
	_, err := idx.WriteTree(newMemoryObjects())
	if !errors.Is(err, ErrMalformed) {
		t.Fatalf("WriteTree returned %v, want %v", err, ErrMalformed)
	}
}

func TestWriteTreeRejectsACacheTreeThatCoversTooFewEntries(t *testing.T) {
	idx := New(Version2)
	idx.Add(blobEntry("a", StageMerged))
	idx.Add(blobEntry("b", StageMerged))
	idx.CacheTree = &CacheTree{EntryCount: 1, ID: idOfByte(4)}
	_, err := idx.WriteTree(newMemoryObjects())
	if !errors.Is(err, ErrMalformed) {
		t.Fatalf("WriteTree returned %v, want %v", err, ErrMalformed)
	}
}

func TestWriteTreeRejectsAnEmptyCachedSubtree(t *testing.T) {
	idx := New(Version2)
	idx.Add(blobEntry("lib/a", StageMerged))
	idx.CacheTree = &CacheTree{EntryCount: -1, Subtrees: []*CacheTree{{Path: "lib", EntryCount: 0, ID: idOfByte(5)}}}
	_, err := idx.WriteTree(newMemoryObjects())
	if !errors.Is(err, ErrMalformed) {
		t.Fatalf("WriteTree returned %v, want %v", err, ErrMalformed)
	}
}

func TestWriteTreeRejectsAFileAndADirectoryWithTheSameName(t *testing.T) {
	idx := New(Version2)
	idx.Add(blobEntry("a", StageMerged))
	idx.Add(blobEntry("a/b", StageMerged))
	_, err := idx.WriteTree(newMemoryObjects())
	if !errors.Is(err, ErrMalformed) {
		t.Fatalf("WriteTree returned %v, want %v", err, ErrMalformed)
	}
}

func TestWriteTreeStoresModesOfEveryEntryKind(t *testing.T) {
	idx := New(Version2)
	idx.Add(Entry{Path: "exec", Mode: object.ModeExecutable, ID: idOfByte(1)})
	idx.Add(Entry{Path: "link", Mode: object.ModeSymlink, ID: idOfByte(2)})
	idx.Add(Entry{Path: "module", Mode: object.ModeSubmodule, ID: idOfByte(3)})
	objects := newMemoryObjects()
	id, err := idx.WriteTree(objects)
	if err != nil {
		t.Fatalf("WriteTree returned error %v", err)
	}
	tree, err := object.ParseTree(objects.stored[id])
	if err != nil {
		t.Fatalf("ParseTree returned error %v", err)
	}
	for _, entry := range tree.Entries {
		stored, ok := idx.Get(entry.Name, StageMerged)
		if !ok || stored.Mode != entry.Mode {
			t.Fatalf("the tree entry %q carries mode %s", entry.Name, entry.Mode)
		}
	}
}

func TestWriteTreeAndWriteAgreeOnTheStoredCacheTree(t *testing.T) {
	idx := loadFixture(t, basicV2)
	idx.Add(blobEntry("lib/deep/extra.txt", StageMerged))
	id, err := idx.WriteTree(newMemoryObjects())
	if err != nil {
		t.Fatalf("WriteTree returned error %v", err)
	}
	again := reread(t, encodeIndex(t, idx, Version2))
	if again.CacheTree == nil || again.CacheTree.ID != id {
		t.Fatalf("the written cache tree root is %+v", again.CacheTree)
	}
	if again.CacheTree.Lookup("lib/deep") == nil {
		t.Fatal("the rebuilt subtree did not reach the file")
	}
}

func TestWriteTreeCreatesTheExtensionOnAnIndexWithoutOne(t *testing.T) {
	idx := New(Version2)
	idx.Add(Entry{Path: "a", Mode: object.ModeBlob, ID: idOfByte(1)})
	if _, err := idx.WriteTree(newMemoryObjects()); err != nil {
		t.Fatalf("WriteTree returned error %v", err)
	}
	data := encodeIndex(t, idx, Version2)
	if !bytes.Contains(data, []byte(extCacheTree)) {
		t.Fatal("the written index carries no cache tree")
	}
}

func TestReadCStringAndReadLineRejectMissingTerminators(t *testing.T) {
	if _, _, err := readCString([]byte("abc"), 0); !errors.Is(err, ErrTruncated) {
		t.Fatalf("readCString returned %v", err)
	}
	if _, _, err := readLine([]byte("abc"), 0); !errors.Is(err, ErrTruncated) {
		t.Fatalf("readLine returned %v", err)
	}
	text, next, err := readCString([]byte("abc\x00rest"), 0)
	if err != nil || text != "abc" || next != 4 {
		t.Fatalf("readCString returned (%q, %d, %v)", text, next, err)
	}
	line, next, err := readLine([]byte("abc\nrest"), 0)
	if err != nil || line != "abc" || next != 4 {
		t.Fatalf("readLine returned (%q, %d, %v)", line, next, err)
	}
}

func TestEncodeCacheTreeKeepsDeepChainsWithoutRecursion(t *testing.T) {
	root := &CacheTree{EntryCount: -1}
	node := root
	for range 4096 {
		child := &CacheTree{Path: "d", EntryCount: -1}
		node.Subtrees = append(node.Subtrees, child)
		node = child
	}
	data := encodeCacheTree(root)
	parsed, err := parseCacheTree(data)
	if err != nil {
		t.Fatalf("parseCacheTree returned error %v", err)
	}
	if !bytes.Equal(encodeCacheTree(parsed), data) {
		t.Fatal("a deep cache tree does not survive a round trip")
	}
	if parsed.Lookup(strings.TrimSuffix(strings.Repeat("d/", 4096), "/")) == nil {
		t.Fatal("the deepest node is missing")
	}
}

func TestWriteTreeAcceptsAnObjectStoreThatImplementsTheInterface(t *testing.T) {
	var store Writer = newMemoryObjects()
	id, err := store.Put(object.TypeTree, nil)
	if err != nil {
		t.Fatalf("Put returned error %v", err)
	}
	if id == hash.Zero {
		t.Fatal("Put returned an empty object id")
	}
}
