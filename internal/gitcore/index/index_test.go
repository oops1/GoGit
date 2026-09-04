package index

import (
	"slices"
	"testing"
	"time"

	"github.com/oops1/gogit/internal/gitcore/object"
)

func TestAddInsertsEntriesInSortedOrder(t *testing.T) {
	idx := New(Version2)
	for _, path := range []string{"c", "a", "b/x", "b"} {
		idx.Add(blobEntry(path, StageMerged))
	}
	want := []string{"a", "b", "b/x", "c"}
	if got := paths(idx); !slices.Equal(got, want) {
		t.Fatalf("paths = %v, want %v", got, want)
	}
	if idx.Len() != len(want) {
		t.Fatalf("Len = %d, want %d", idx.Len(), len(want))
	}
}

func TestAddReplacesAnEntryWithTheSamePathAndStage(t *testing.T) {
	idx := New(Version2)
	idx.Add(blobEntry("a", StageMerged))
	replacement := blobEntry("a", StageMerged)
	replacement.Mode = object.ModeExecutable
	idx.Add(replacement)
	if idx.Len() != 1 {
		t.Fatalf("Len = %d, want 1", idx.Len())
	}
	if idx.At(0).Mode != object.ModeExecutable {
		t.Fatalf("the stored mode is %s", idx.At(0).Mode)
	}
}

func TestAddKeepsStagesOfTheSamePathApart(t *testing.T) {
	idx := New(Version2)
	for _, stage := range []Stage{StageTheirs, StageAncestor, StageOurs} {
		idx.Add(blobEntry("a", stage))
	}
	if idx.Len() != 3 {
		t.Fatalf("Len = %d, want 3", idx.Len())
	}
	for at := range 3 {
		if idx.At(at).Stage != Stage(at+1) {
			t.Fatalf("entry %d carries stage %d", at, idx.At(at).Stage)
		}
	}
	conflicts := idx.Conflicts("a")
	if len(conflicts) != 3 {
		t.Fatalf("Conflicts returned %d entries", len(conflicts))
	}
}

func TestAddStoresACopyOfTheEntry(t *testing.T) {
	idx := New(Version2)
	entry := blobEntry("a", StageMerged)
	idx.Add(entry)
	entry.Mode = object.ModeSymlink
	if idx.At(0).Mode != object.ModeBlob {
		t.Fatal("Add stored a reference to the caller's entry")
	}
}

func TestGetFindsEntriesByPathAndStage(t *testing.T) {
	idx := loadFixture(t, conflictV2)
	if _, ok := idx.Get("conflict.txt", StageMerged); ok {
		t.Fatal("Get found a merged entry for a conflicted path")
	}
	entry, ok := idx.Get("conflict.txt", StageOurs)
	if !ok {
		t.Fatal("Get found no entry for stage 2")
	}
	if entry.Stage != StageOurs {
		t.Fatalf("Get returned stage %d", entry.Stage)
	}
	if _, ok := idx.Get("missing.txt", StageMerged); ok {
		t.Fatal("Get found an entry that does not exist")
	}
}

func TestRemoveDropsEveryStageOfThePath(t *testing.T) {
	idx := loadFixture(t, conflictV2)
	if !idx.Remove("conflict.txt") {
		t.Fatal("Remove reported that nothing was removed")
	}
	if got := paths(idx); !slices.Equal(got, []string{"keep.txt"}) {
		t.Fatalf("paths = %v", got)
	}
	if idx.HasConflicts() {
		t.Fatal("the index still reports conflicts")
	}
}

func TestRemoveReportsAnUnknownPath(t *testing.T) {
	idx := loadFixture(t, basicV2)
	if idx.Remove("missing.txt") {
		t.Fatal("Remove reported a removal of a path that is not in the index")
	}
	if idx.Len() != 4 {
		t.Fatalf("Len = %d, want 4", idx.Len())
	}
}

func TestRemoveOnAnIndexWithoutACacheTree(t *testing.T) {
	idx := New(Version2)
	idx.Add(blobEntry("a", StageMerged))
	if !idx.Remove("a") {
		t.Fatal("Remove reported that nothing was removed")
	}
}

func TestEntriesStopsWhenTheCallerStops(t *testing.T) {
	idx := loadFixture(t, basicV2)
	seen := 0
	for range idx.Entries() {
		seen++
		break
	}
	if seen != 1 {
		t.Fatalf("the iterator produced %d entries after the caller stopped", seen)
	}
}

func TestPathsReturnsEveryPathOnceUnderThePrefix(t *testing.T) {
	idx := loadFixture(t, basicV2)
	if got := slices.Collect(idx.Paths("lib/")); !slices.Equal(got, []string{"lib/deep/note.txt", "lib/library.txt"}) {
		t.Fatalf("Paths(\"lib/\") = %v", got)
	}
	if got := slices.Collect(idx.Paths("")); len(got) != 4 {
		t.Fatalf("Paths(\"\") returned %d paths", len(got))
	}
	if got := slices.Collect(idx.Paths("zzz")); got != nil {
		t.Fatalf("Paths(\"zzz\") = %v", got)
	}
	if got := slices.Collect(idx.Paths("a")); !slices.Equal(got, []string{"a.txt"}) {
		t.Fatalf("Paths(\"a\") = %v", got)
	}
}

func TestPathsCollapsesConflictStages(t *testing.T) {
	idx := loadFixture(t, conflictV2)
	if got := slices.Collect(idx.Paths("")); !slices.Equal(got, []string{"conflict.txt", "keep.txt"}) {
		t.Fatalf("Paths(\"\") = %v", got)
	}
}

func TestPathsStopsWhenTheCallerStops(t *testing.T) {
	idx := loadFixture(t, basicV2)
	seen := 0
	for range idx.Paths("") {
		seen++
		break
	}
	if seen != 1 {
		t.Fatalf("the iterator produced %d paths after the caller stopped", seen)
	}
}

func TestIsRacyComparesTheIndexTimestampWithTheEntry(t *testing.T) {
	idx := New(Version2)
	idx.Add(blobEntry("a", StageMerged))
	entry := idx.At(0)
	if idx.IsRacy(entry) {
		t.Fatal("an index without a timestamp reports racy entries")
	}
	idx.Timestamp = time.Unix(1000, 500)
	cases := []struct {
		name     string
		modified time.Time
		want     bool
	}{
		{name: "older entry", modified: time.Unix(999, 0), want: false},
		{name: "younger entry", modified: time.Unix(1001, 0), want: true},
		{name: "same second with a smaller fraction", modified: time.Unix(1000, 400), want: false},
		{name: "same second with the same fraction", modified: time.Unix(1000, 500), want: true},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			entry.Stat.MTime = testCase.modified
			if got := idx.IsRacy(entry); got != testCase.want {
				t.Fatalf("IsRacy = %v, want %v", got, testCase.want)
			}
		})
	}
	entry.Mode = object.ModeSubmodule
	entry.Stat.MTime = time.Unix(2000, 0)
	if idx.IsRacy(entry) {
		t.Fatal("a submodule entry was reported as racy")
	}
}

func TestMatchesFileCombinesTheStatCheckWithTheRaceWindow(t *testing.T) {
	idx := New(Version2)
	idx.Add(Entry{Path: "a", Mode: object.ModeBlob, Stat: Stat{Size: 4, MTime: time.Unix(1000, 0)}})
	entry := idx.At(0)
	info := fakeInfo{size: 4, modified: time.Unix(1000, 0)}
	if !idx.MatchesFile(entry, info) {
		t.Fatal("MatchesFile reported a change for an untouched file")
	}
	idx.Timestamp = time.Unix(1000, 0)
	if idx.MatchesFile(entry, info) {
		t.Fatal("MatchesFile trusted the stat data inside the race window")
	}
}
