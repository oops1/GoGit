package index

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
)

func entryKeys(idx *Index) []string {
	var out []string
	for entry := range idx.Entries() {
		out = append(out, fmt.Sprintf("%s:%d:%s:%s", entry.Path, entry.Stage, entry.Mode, entry.ID))
	}
	return out
}

func indexOf(entries ...Entry) *Index {
	idx := New(Version2)
	for _, entry := range entries {
		idx.Add(entry)
	}
	return idx
}

func TestReplaceMatchesRemovingThenAddingEachPath(t *testing.T) {
	executable := blobEntry("b", StageMerged)
	executable.Mode = object.ModeExecutable
	second := blobEntry("a", StageMerged)
	second.ID = idOfByte(9)
	tests := []struct {
		name    string
		start   []Entry
		paths   []string
		entries []Entry
	}{
		{name: "empty index", paths: []string{"x"}, entries: []Entry{blobEntry("b", StageMerged), blobEntry("a", StageMerged)}},
		{name: "nothing to do", start: []Entry{blobEntry("a", StageMerged)}},
		{name: "remove only", start: []Entry{blobEntry("a", StageMerged), blobEntry("b", StageMerged)}, paths: []string{"a", "missing"}},
		{name: "replace in place", start: []Entry{blobEntry("a", StageMerged), blobEntry("b", StageMerged)}, paths: []string{"b"}, entries: []Entry{executable}},
		{name: "replace without removal", start: []Entry{blobEntry("a", StageMerged)}, entries: []Entry{second}},
		{
			name:    "merged entry becomes a conflict",
			start:   []Entry{blobEntry("a", StageMerged), blobEntry("c", StageMerged)},
			paths:   []string{"a"},
			entries: []Entry{blobEntry("a", StageTheirs), blobEntry("a", StageAncestor), blobEntry("a", StageOurs)},
		},
		{
			name:    "conflict resolves into a merged entry",
			start:   []Entry{blobEntry("a", StageAncestor), blobEntry("a", StageOurs), blobEntry("b", StageMerged)},
			paths:   []string{"a"},
			entries: []Entry{second},
		},
		{
			name:    "additions before, between and after existing entries",
			start:   []Entry{blobEntry("b", StageMerged), blobEntry("d", StageMerged)},
			paths:   []string{"d"},
			entries: []Entry{blobEntry("e", StageMerged), blobEntry("a", StageMerged), blobEntry("c", StageMerged)},
		},
		{
			name:    "the last duplicate wins",
			start:   []Entry{blobEntry("a", StageMerged)},
			paths:   []string{"a"},
			entries: []Entry{blobEntry("a", StageMerged), second},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			want := indexOf(tc.start...)
			for _, path := range tc.paths {
				want.Remove(path)
			}
			for _, entry := range tc.entries {
				want.Add(entry)
			}
			got := indexOf(tc.start...)
			got.Replace(tc.paths, tc.entries)
			if !slices.Equal(entryKeys(got), entryKeys(want)) {
				t.Fatalf("Replace left %v, removing and adding leaves %v", entryKeys(got), entryKeys(want))
			}
		})
	}
}

func TestReplaceStoresCopiesOfTheEntries(t *testing.T) {
	idx := New(Version2)
	entries := []Entry{blobEntry("a", StageMerged)}
	idx.Replace(nil, entries)
	entries[0].Path = "changed"
	if got := paths(idx); !slices.Equal(got, []string{"a"}) {
		t.Fatalf("paths = %v after the caller changed its slice", got)
	}
}

func TestReplaceInvalidatesTheCacheTreeOfRemovedAndAddedPaths(t *testing.T) {
	idx := indexOf(blobEntry("keep/x", StageMerged), blobEntry("gone/y", StageMerged), blobEntry("new/z", StageMerged))
	if _, err := idx.WriteTree(newMemoryObjects()); err != nil {
		t.Fatalf("WriteTree returned error %v", err)
	}
	idx.Replace([]string{"gone/y"}, []Entry{blobEntry("new/w", StageMerged)})
	for name, wantValid := range map[string]bool{"keep": true, "gone": false, "new": false} {
		if sub := idx.CacheTree.Find(name); sub.Valid() != wantValid {
			t.Errorf("cache tree %q valid = %v, want %v", name, sub.Valid(), wantValid)
		}
	}
	if idx.CacheTree.Valid() {
		t.Fatalf("the root of the cache tree stays valid")
	}
}

func BenchmarkReplaceOfAThousandPaths(b *testing.B) {
	paths, entries := benchmarkChanges()
	base := benchmarkIndex(b)
	for b.Loop() {
		idx := &Index{Version: Version2, entries: slices.Clone(base.entries)}
		idx.Replace(paths, entries)
	}
}

func BenchmarkRemoveAndAddOfAThousandPaths(b *testing.B) {
	paths, entries := benchmarkChanges()
	base := benchmarkIndex(b)
	for b.Loop() {
		idx := &Index{Version: Version2, entries: slices.Clone(base.entries)}
		for at, path := range paths {
			idx.Remove(path)
			idx.Add(entries[at])
		}
	}
}

func benchmarkChanges() ([]string, []Entry) {
	var paths []string
	var entries []Entry
	for at := 0; at < benchmarkEntries; at += benchmarkEntries / 1000 {
		path := fmt.Sprintf("src/module%03d/package%02d/file%04d.go", at%128, at%64, at)
		paths = append(paths, path)
		entries = append(entries, Entry{Path: path, Mode: object.ModeBlob, ID: idOfByte(byte(at + 1))})
	}
	return paths, entries
}

type storedObject struct {
	kind object.Type
	data []byte
}

type objectReader map[hash.ObjectID]storedObject

var errMissingObject = errors.New("missing object")

func (r objectReader) Get(id hash.ObjectID) (object.Type, []byte, error) {
	stored, ok := r[id]
	if !ok {
		return 0, nil, errMissingObject
	}
	return stored.kind, stored.data, nil
}

func TestContentBlobReadsTheMergedEntryOrOurSideOfAConflict(t *testing.T) {
	merged, ours, theirs, tree := idOfByte(1), idOfByte(2), idOfByte(3), idOfByte(4)
	objects := objectReader{
		merged: {kind: object.TypeBlob, data: []byte("merged")},
		ours:   {kind: object.TypeBlob, data: []byte("ours")},
		theirs: {kind: object.TypeBlob, data: []byte("theirs")},
		tree:   {kind: object.TypeTree, data: []byte("tree")},
	}
	idx := indexOf(
		Entry{Path: "clean", Mode: object.ModeBlob, ID: merged},
		Entry{Path: "conflict", Mode: object.ModeBlob, ID: theirs, Stage: StageTheirs},
		Entry{Path: "conflict", Mode: object.ModeBlob, ID: ours, Stage: StageOurs},
		Entry{Path: "deleted-by-us", Mode: object.ModeBlob, ID: theirs, Stage: StageTheirs},
		Entry{Path: "lost", Mode: object.ModeBlob, ID: idOfByte(5)},
		Entry{Path: "not-a-blob", Mode: object.ModeBlob, ID: tree},
	)
	tests := []struct {
		path string
		want string
		ok   bool
	}{
		{"clean", "merged", true},
		{"conflict", "ours", true},
		{"deleted-by-us", "", false},
		{"untracked", "", false},
		{"lost", "", false},
		{"not-a-blob", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			data, ok := idx.ContentBlob(objects, tc.path)
			if ok != tc.ok || string(data) != tc.want {
				t.Fatalf("ContentBlob(%q) = %q, %v, want %q, %v", tc.path, data, ok, tc.want, tc.ok)
			}
		})
	}
}

func racyFixture(stamp time.Time) *Index {
	idx := indexOf(
		Entry{Path: "before", Mode: object.ModeBlob, Stat: Stat{MTime: stamp.Add(-time.Second), Size: 5}},
		Entry{Path: "same-second-earlier", Mode: object.ModeBlob, Stat: Stat{MTime: stamp.Add(-time.Nanosecond), Size: 5}},
		Entry{Path: "same-instant", Mode: object.ModeBlob, Stat: Stat{MTime: stamp, Size: 5}},
		Entry{Path: "later", Mode: object.ModeBlob, Stat: Stat{MTime: stamp.Add(time.Second), Size: 5}},
		Entry{Path: "submodule", Mode: object.ModeSubmodule, Stat: Stat{MTime: stamp, Size: 5}},
		Entry{Path: "verified", Mode: object.ModeBlob, Stat: Stat{MTime: stamp, Size: 5}},
	)
	idx.Timestamp = stamp
	return idx
}

func TestSmudgeRacilyCleanZeroesTheSizeOfModifiedRacyEntries(t *testing.T) {
	stamp := time.Unix(1700000000, 500)
	idx := racyFixture(stamp)
	var asked []string
	idx.SmudgeRacilyClean(func(entry *Entry) bool {
		asked = append(asked, entry.Path)
		return entry.Path != "verified"
	})
	if want := []string{"later", "same-instant", "verified"}; !slices.Equal(asked, want) {
		t.Fatalf("the content of %v was checked, want %v", asked, want)
	}
	reloaded := reread(t, encodeIndex(t, idx, Version2))
	sizes := map[string]uint32{}
	for entry := range reloaded.Entries() {
		sizes[entry.Path] = entry.Stat.Size
	}
	want := map[string]uint32{"before": 5, "same-second-earlier": 5, "same-instant": 0, "later": 0, "submodule": 5, "verified": 5}
	for path, size := range want {
		if sizes[path] != size {
			t.Errorf("%s keeps size %d, want %d", path, sizes[path], size)
		}
	}
}

func TestSmudgedEntryStaysModifiedAfterALaterIndexWrite(t *testing.T) {
	stamp := time.Unix(1700000000, 0)
	staged := Entry{Path: "a", Mode: object.ModeBlob, Stat: Stat{MTime: stamp, Size: 5}}
	idx := indexOf(staged)
	idx.Timestamp = stamp
	idx.SmudgeRacilyClean(func(*Entry) bool { return true })
	later := reread(t, encodeIndex(t, idx, Version2))
	later.Timestamp = stamp.Add(2 * time.Second)
	entry, _ := later.Get("a", StageMerged)
	sameSizeEdit := fakeInfo{size: 5, modified: stamp}
	if later.MatchesFile(entry, sameSizeEdit, true) {
		t.Fatalf("the smudged entry matches the edited file once the index is newer")
	}
	unsmudged := indexOf(staged)
	unsmudged.Timestamp = stamp.Add(2 * time.Second)
	kept, _ := unsmudged.Get("a", StageMerged)
	if !unsmudged.MatchesFile(kept, sameSizeEdit, true) {
		t.Fatalf("the fixture does not reproduce the racy-git problem")
	}
}

func TestHasRacyEntriesFollowsTheIndexTimestamp(t *testing.T) {
	stamp := time.Unix(1700000000, 500)
	if !racyFixture(stamp).HasRacyEntries() {
		t.Fatalf("HasRacyEntries = false for entries modified with the index")
	}
	older := racyFixture(stamp)
	older.Timestamp = stamp.Add(time.Hour)
	if older.HasRacyEntries() {
		t.Fatalf("HasRacyEntries = true for entries older than the index")
	}
	unknown := racyFixture(stamp)
	unknown.Timestamp = time.Time{}
	called := false
	unknown.SmudgeRacilyClean(func(*Entry) bool { called = true; return true })
	if unknown.HasRacyEntries() || called {
		t.Fatalf("an index without a timestamp treats entries as racy")
	}
}

func TestEntriesAddedUpToDateAreNotCheckedForRacyCleanliness(t *testing.T) {
	stamp := time.Unix(1700000000, 500)
	idx := New(Version2)
	idx.Timestamp = stamp
	idx.AddUpToDate(Entry{Path: "fresh", Mode: object.ModeBlob, Stat: Stat{MTime: stamp, Size: 5}})
	if idx.HasRacyEntries() {
		t.Fatalf("HasRacyEntries = true for an entry this process verified")
	}
	idx.Add(Entry{Path: "fresh", Mode: object.ModeBlob, Stat: Stat{MTime: stamp, Size: 5}})
	var asked []string
	idx.SmudgeRacilyClean(func(entry *Entry) bool {
		asked = append(asked, entry.Path)
		return false
	})
	if !slices.Equal(asked, []string{"fresh"}) {
		t.Fatalf("the content of %v was checked after the entry was replaced unverified", asked)
	}
}

func TestReadRejectsSparseIndex(t *testing.T) {
	data := buildIndex(t, Version2, Entry{Path: "dir/", Mode: object.ModeTree, SkipWorktree: true})
	body := data[:len(data)-hash.Size]
	body = append(bytes.Clone(body), "sdir\x00\x00\x00\x00"...)
	_, err := Read(bytes.NewReader(rewriteChecksum(append(body, make([]byte, hash.Size)...))))
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("Read returned %v, want %v", err, ErrUnsupported)
	}
	if !strings.Contains(err.Error(), "sparse") {
		t.Fatalf("Read returned %v, which does not explain the sparse index", err)
	}
}
