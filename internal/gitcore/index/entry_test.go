package index

import (
	"io/fs"
	"testing"
	"time"

	"github.com/oops1/gogit/internal/gitcore/object"
)

func TestStageValidAcceptsOnlyTheFourGitStages(t *testing.T) {
	for stage := range Stage(6) {
		if got, want := stage.Valid(), stage <= StageTheirs; got != want {
			t.Fatalf("Stage(%d).Valid() = %v, want %v", stage, got, want)
		}
	}
}

func TestEntryMatchesComparesModificationTimeAndSize(t *testing.T) {
	modified := time.Unix(1700000000, 250)
	base := Entry{Path: "a", Mode: object.ModeBlob, Stat: Stat{Size: 12, MTime: modified}}
	cases := []struct {
		name  string
		entry Entry
		info  fs.FileInfo
		racy  bool
		want  bool
	}{
		{name: "unchanged file", entry: base, info: fakeInfo{size: 12, modified: modified}, want: true},
		{name: "missing file", entry: base, info: nil},
		{name: "different size", entry: base, info: fakeInfo{size: 13, modified: modified}},
		{
			name:  "different fraction of a second",
			entry: base,
			info:  fakeInfo{size: 12, modified: time.Unix(1700000000, 251)},
		},
		{
			name:  "different second",
			entry: base,
			info:  fakeInfo{size: 12, modified: time.Unix(1700000001, 250)},
		},
		{name: "directory in place of a file", entry: base, info: fakeInfo{mode: fs.ModeDir}},
		{
			name:  "racily clean file",
			entry: base,
			info:  fakeInfo{size: 12, modified: modified},
			racy:  true,
		},
		{
			name:  "racily clean empty file",
			entry: Entry{Path: "a", Mode: object.ModeBlob, Stat: Stat{MTime: modified}},
			info:  fakeInfo{modified: modified},
			racy:  true,
			want:  true,
		},
		{
			name:  "assume valid entry",
			entry: Entry{Path: "a", Mode: object.ModeBlob, AssumeValid: true},
			info:  fakeInfo{size: 99},
			want:  true,
		},
		{
			name:  "skip worktree entry",
			entry: Entry{Path: "a", Mode: object.ModeBlob, SkipWorktree: true},
			info:  fakeInfo{size: 99},
			want:  true,
		},
		{
			name:  "intent to add entry",
			entry: Entry{Path: "a", Mode: object.ModeBlob, IntentToAdd: true},
			info:  fakeInfo{},
		},
		{
			name:  "symlink against a symlink",
			entry: Entry{Path: "a", Mode: object.ModeSymlink, Stat: Stat{Size: 5, MTime: modified}},
			info:  fakeInfo{size: 5, mode: fs.ModeSymlink, modified: modified},
			want:  true,
		},
		{
			name:  "symlink against a regular file",
			entry: Entry{Path: "a", Mode: object.ModeSymlink, Stat: Stat{Size: 5, MTime: modified}},
			info:  fakeInfo{size: 5, modified: modified},
		},
		{
			name:  "submodule against a directory",
			entry: Entry{Path: "a", Mode: object.ModeSubmodule},
			info:  fakeInfo{mode: fs.ModeDir},
			want:  true,
		},
		{
			name:  "submodule against a file",
			entry: Entry{Path: "a", Mode: object.ModeSubmodule},
			info:  fakeInfo{},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := testCase.entry.Matches(testCase.info, testCase.racy); got != testCase.want {
				t.Fatalf("Matches = %v, want %v", got, testCase.want)
			}
		})
	}
}

func TestEntryMatchesIgnoresFileInfoOfTheZeroInterface(t *testing.T) {
	entry := Entry{Path: "a", Mode: object.ModeBlob}
	if entry.Matches(nil, false) {
		t.Fatal("Matches reported an unchanged file for a missing one")
	}
}

func TestComparePathStageOrdersPathsThenStages(t *testing.T) {
	cases := []struct {
		pathA  string
		stageA Stage
		pathB  string
		stageB Stage
		want   int
	}{
		{pathA: "a", pathB: "b", want: -1},
		{pathA: "b", pathB: "a", want: 1},
		{pathA: "a", pathB: "a", want: 0},
		{pathA: "a", stageA: StageOurs, pathB: "a", stageB: StageTheirs, want: -1},
		{pathA: "a", stageA: StageTheirs, pathB: "a", stageB: StageOurs, want: 1},
		{pathA: "a", stageA: StageOurs, pathB: "a", stageB: StageOurs, want: 0},
	}
	for _, testCase := range cases {
		got := comparePathStage(testCase.pathA, testCase.stageA, testCase.pathB, testCase.stageB)
		if got != testCase.want {
			t.Fatalf("comparePathStage(%q, %d, %q, %d) = %d, want %d",
				testCase.pathA, testCase.stageA, testCase.pathB, testCase.stageB, got, testCase.want)
		}
	}
}

func TestEntryFlagHelpers(t *testing.T) {
	plain := Entry{Path: "a"}
	if plain.Extended() || plain.Conflicted() {
		t.Fatalf("a plain entry reports %v and %v", plain.Extended(), plain.Conflicted())
	}
	intent := Entry{IntentToAdd: true}
	if !intent.Extended() {
		t.Fatal("an intent to add entry is not extended")
	}
	skipped := Entry{SkipWorktree: true}
	if !skipped.Extended() {
		t.Fatal("a skip worktree entry is not extended")
	}
	staged := Entry{Stage: StageOurs}
	if !staged.Conflicted() {
		t.Fatal("a staged entry is not conflicted")
	}
}
