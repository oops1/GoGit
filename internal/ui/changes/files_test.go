package changes

import (
	"testing"
	"time"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/gitcore/diff"
	"github.com/oops1/gogit/internal/gitcore/worktree"
)

func registerStateStrings(t *testing.T) {
	t.Helper()
	strings := map[string]string{
		"Files.State.Modified":    "Modified",
		"Files.State.Added":       "Added",
		"Files.State.Deleted":     "Deleted",
		"Files.State.Renamed":     "Renamed",
		"Files.State.Copied":      "Copied",
		"Files.State.TypeChanged": "TypeChanged",
		"Files.State.Conflict":    "Conflict",
		"Files.State.Untracked":   "Untracked",
		"Files.State.Ignored":     "Ignored",
		"Files.State.Unmodified":  "Unmodified",
		"Diff.Truncated":          "Diff truncated, showing a partial result",
	}
	for key, value := range strings {
		widget.RegisterString("en", key, value)
	}
	widget.SetLanguage("en")
	t.Cleanup(widget.ClearStrings)
}

func TestFilesMapsStateNameAndPathForEachStatusCode(t *testing.T) {
	registerStateStrings(t)
	tests := []struct {
		name string
		file diff.File
		want Row
	}{
		{
			name: "modified",
			file: diff.File{OldPath: "src/main.go", NewPath: "src/main.go", Status: diff.StatusModified},
			want: Row{State: "Modified", Name: "main.go", RelDir: "src", RelPath: "src/main.go", Extension: "go"},
		},
		{
			name: "added",
			file: diff.File{NewPath: "src/new.go", Status: diff.StatusAdded, NewSize: 12},
			want: Row{State: "Added", Name: "new.go", RelDir: "src", RelPath: "src/new.go", Extension: "go", Size: "12"},
		},
		{
			name: "deleted",
			file: diff.File{OldPath: "src/old.go", Status: diff.StatusDeleted, OldSize: 7},
			want: Row{State: "Deleted", Name: "old.go", RelDir: "src", RelPath: "src/old.go", Extension: "go", Size: "7"},
		},
		{
			name: "copied",
			file: diff.File{OldPath: "src/a.go", NewPath: "src/b.go", Status: diff.StatusCopied},
			want: Row{State: "Copied", Name: "b.go", RelDir: "src", RelPath: "src/b.go", Extension: "go", RenamedFrom: "src/a.go"},
		},
		{
			name: "typeChanged",
			file: diff.File{OldPath: "src/link", NewPath: "src/link", Status: diff.StatusTypeChanged},
			want: Row{State: "TypeChanged", Name: "link", RelDir: "src", RelPath: "src/link"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows := Files([]diff.File{tt.file})
			if len(rows) != 1 || rows[0] != tt.want {
				t.Fatalf("row = %+v, want %+v", rows, tt.want)
			}
		})
	}
}

func TestFilesShowsRenamedFromForRenames(t *testing.T) {
	registerStateStrings(t)
	rows := Files([]diff.File{{OldPath: "src/old.go", NewPath: "src/new.go", Status: diff.StatusRenamed}})

	want := Row{State: "Renamed", Name: "new.go", RelDir: "src", RelPath: "src/new.go", Extension: "go", RenamedFrom: "src/old.go"}
	if len(rows) != 1 || rows[0] != want {
		t.Fatalf("row = %+v, want %+v", rows, want)
	}
}

func TestFilesLeavesRenamedFromEmptyWhenARenameKeepsTheSamePath(t *testing.T) {
	registerStateStrings(t)
	rows := Files([]diff.File{{OldPath: "src/same.go", NewPath: "src/same.go", Status: diff.StatusRenamed}})

	want := Row{State: "Renamed", Name: "same.go", RelDir: "src", RelPath: "src/same.go", Extension: "go"}
	if len(rows) != 1 || rows[0] != want {
		t.Fatalf("row = %+v, want %+v", rows, want)
	}
}

func TestFilesReportsNoRelDirForARootFile(t *testing.T) {
	registerStateStrings(t)
	rows := Files([]diff.File{{NewPath: "root.txt", Status: diff.StatusAdded}})

	if len(rows) != 1 || rows[0].RelDir != "" || rows[0].Name != "root.txt" {
		t.Fatalf("row = %+v, want RelDir empty and Name root.txt", rows)
	}
}

func TestFilesLeavesExtensionEmptyForADotfile(t *testing.T) {
	registerStateStrings(t)
	rows := Files([]diff.File{{NewPath: ".gitignore", Status: diff.StatusAdded}})

	if len(rows) != 1 || rows[0].Extension != "" {
		t.Fatalf("row = %+v, want empty Extension", rows)
	}
}

func manyAddedFiles(n int) []diff.File {
	files := make([]diff.File, n)
	for i := range files {
		files[i] = diff.File{NewPath: "file.go", Status: diff.StatusAdded}
	}
	return files
}

func TestFilesDoesNotTruncateWhenWithinMaxFiles(t *testing.T) {
	registerStateStrings(t)
	rows := Files(manyAddedFiles(MaxFiles))

	if len(rows) != MaxFiles {
		t.Fatalf("rows = %d, want %d", len(rows), MaxFiles)
	}
}

func TestFilesTruncatesAndAppendsAMarkerRowWhenExceedingMaxFiles(t *testing.T) {
	registerStateStrings(t)
	rows := Files(manyAddedFiles(MaxFiles + 5))

	if len(rows) != MaxFiles+1 {
		t.Fatalf("rows = %d, want %d", len(rows), MaxFiles+1)
	}
	marker := rows[MaxFiles]
	if marker.State != "" || marker.RelPath != "" || marker.Name == "" {
		t.Fatalf("marker row = %+v", marker)
	}
}

func TestFilesReturnsEmptySliceForNoFiles(t *testing.T) {
	rows := Files(nil)

	if len(rows) != 0 {
		t.Fatalf("rows = %d, want 0", len(rows))
	}
}

func TestWorkingRowsMapsStateNameAndPathForEachEntryKind(t *testing.T) {
	registerStateStrings(t)
	mtime := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	tests := []struct {
		name  string
		entry worktree.Entry
		want  Row
	}{
		{
			name:  "unstaged modified",
			entry: worktree.Entry{Path: "src/main.go", Staged: worktree.StatusUnmodified, Unstaged: worktree.StatusModified, Size: 42, ModTime: mtime},
			want:  Row{State: "Modified", WorkingState: "Modified", Name: "main.go", RelDir: "src", RelPath: "src/main.go", Extension: "go", Size: "42", Modified: "2026-01-02 03:04:05"},
		},
		{
			name:  "staged added",
			entry: worktree.Entry{Path: "src/new.go", Staged: worktree.StatusAdded, Unstaged: worktree.StatusUnmodified},
			want:  Row{State: "Added", IndexState: "Added", Name: "new.go", RelDir: "src", RelPath: "src/new.go", Extension: "go"},
		},
		{
			name:  "staged deleted",
			entry: worktree.Entry{Path: "src/old.go", Staged: worktree.StatusDeleted, Unstaged: worktree.StatusUnmodified},
			want:  Row{State: "Deleted", IndexState: "Deleted", Name: "old.go", RelDir: "src", RelPath: "src/old.go", Extension: "go"},
		},
		{
			name:  "untracked file always shows the untracked state",
			entry: worktree.Entry{Path: "src/new.go", Staged: worktree.StatusUnmodified, Unstaged: worktree.StatusUntracked},
			want:  Row{State: "Untracked", WorkingState: "Untracked", Name: "new.go", RelDir: "src", RelPath: "src/new.go", Extension: "go"},
		},
		{
			name:  "untracked directory keeps its trailing slash in RelPath but not the name",
			entry: worktree.Entry{Path: "src/newdir/", IsDir: true, Staged: worktree.StatusUnmodified, Unstaged: worktree.StatusUntracked},
			want:  Row{State: "Untracked", WorkingState: "Untracked", Name: "newdir", RelDir: "src", RelPath: "src/newdir/"},
		},
		{
			name:  "conflict",
			entry: worktree.Entry{Path: "src/conf.go", Staged: worktree.StatusUnmerged, Unstaged: worktree.StatusUnmerged, Conflict: worktree.ConflictBothModified},
			want:  Row{State: "Conflict", IndexState: "Conflict", WorkingState: "Conflict", Name: "conf.go", RelDir: "src", RelPath: "src/conf.go", Extension: "go"},
		},
		{
			name:  "staged rename shows RenamedFrom",
			entry: worktree.Entry{Path: "src/new.go", OrigPath: "src/old.go", Staged: worktree.StatusRenamed, Unstaged: worktree.StatusUnmodified},
			want:  Row{State: "Renamed", IndexState: "Renamed", Name: "new.go", RelDir: "src", RelPath: "src/new.go", Extension: "go", RenamedFrom: "src/old.go"},
		},
		{
			name:  "clean entry has no state words",
			entry: worktree.Entry{Path: "src/clean.go", Staged: worktree.StatusUnmodified, Unstaged: worktree.StatusUnmodified},
			want:  Row{State: "Unmodified", Name: "clean.go", RelDir: "src", RelPath: "src/clean.go", Extension: "go"},
		},
		{
			name:  "staged and unstaged changes together summarize as modified",
			entry: worktree.Entry{Path: "src/both.go", Staged: worktree.StatusModified, Unstaged: worktree.StatusModified},
			want:  Row{State: "Modified", IndexState: "Modified", WorkingState: "Modified", Name: "both.go", RelDir: "src", RelPath: "src/both.go", Extension: "go"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows := WorkingRows(worktree.Status{Entries: []worktree.Entry{tt.entry}})
			if len(rows) != 1 || rows[0] != tt.want {
				t.Fatalf("row = %+v, want %+v", rows, tt.want)
			}
		})
	}
}

func TestWorkingRowsOrdersConflictsBeforeStagedBeforeUnstagedBeforeUntracked(t *testing.T) {
	registerStateStrings(t)
	status := worktree.Status{Entries: []worktree.Entry{
		{Path: "untracked.txt", Staged: worktree.StatusUnmodified, Unstaged: worktree.StatusUntracked},
		{Path: "unstaged.txt", Staged: worktree.StatusUnmodified, Unstaged: worktree.StatusModified},
		{Path: "staged.txt", Staged: worktree.StatusAdded, Unstaged: worktree.StatusUnmodified},
		{Path: "conflict.txt", Staged: worktree.StatusUnmerged, Unstaged: worktree.StatusUnmerged, Conflict: worktree.ConflictBothModified},
	}}

	rows := WorkingRows(status)

	want := []string{"conflict.txt", "staged.txt", "unstaged.txt", "untracked.txt"}
	if len(rows) != len(want) {
		t.Fatalf("rows = %d, want %d", len(rows), len(want))
	}
	for i, path := range want {
		if rows[i].RelPath != path {
			t.Fatalf("rows[%d].RelPath = %q, want %q", i, rows[i].RelPath, path)
		}
	}
}

func TestWorkingRowsBreaksTiesWithinTheSameRankByPath(t *testing.T) {
	registerStateStrings(t)
	status := worktree.Status{Entries: []worktree.Entry{
		{Path: "b.txt", Staged: worktree.StatusUnmodified, Unstaged: worktree.StatusModified},
		{Path: "a.txt", Staged: worktree.StatusUnmodified, Unstaged: worktree.StatusModified},
	}}

	rows := WorkingRows(status)

	if len(rows) != 2 || rows[0].RelPath != "a.txt" || rows[1].RelPath != "b.txt" {
		t.Fatalf("rows = %+v, want a.txt before b.txt", rows)
	}
}

func TestWorkingRowsReturnsEmptySliceWithoutEntries(t *testing.T) {
	rows := WorkingRows(worktree.Status{})

	if len(rows) != 0 {
		t.Fatalf("rows = %d, want 0", len(rows))
	}
}

func manyModifiedEntries(n int) []worktree.Entry {
	entries := make([]worktree.Entry, n)
	for i := range entries {
		entries[i] = worktree.Entry{Path: "file.txt", Staged: worktree.StatusUnmodified, Unstaged: worktree.StatusModified}
	}
	return entries
}

func TestWorkingRowsDoesNotTruncateWhenWithinMaxFiles(t *testing.T) {
	registerStateStrings(t)
	rows := WorkingRows(worktree.Status{Entries: manyModifiedEntries(MaxFiles)})

	if len(rows) != MaxFiles {
		t.Fatalf("rows = %d, want %d", len(rows), MaxFiles)
	}
}

func TestWorkingRowsTruncatesAndAppendsAMarkerRowWhenExceedingMaxFiles(t *testing.T) {
	registerStateStrings(t)
	rows := WorkingRows(worktree.Status{Entries: manyModifiedEntries(MaxFiles + 5)})

	if len(rows) != MaxFiles+1 {
		t.Fatalf("rows = %d, want %d", len(rows), MaxFiles+1)
	}
	marker := rows[MaxFiles]
	if marker.State != "" || marker.RelPath != "" || marker.Name == "" {
		t.Fatalf("marker row = %+v", marker)
	}
}

func TestSortEntriesDoesNotMutateItsInput(t *testing.T) {
	original := []worktree.Entry{
		{Path: "b.txt", Unstaged: worktree.StatusModified},
		{Path: "a.txt", Staged: worktree.StatusUnmerged, Unstaged: worktree.StatusUnmerged, Conflict: worktree.ConflictBothModified},
	}
	clone := make([]worktree.Entry, len(original))
	copy(clone, original)

	sorted := SortEntries(original)

	if sorted[0].Path != "a.txt" || sorted[1].Path != "b.txt" {
		t.Fatalf("sorted = %+v, want conflict first", sorted)
	}
	for i := range original {
		if original[i] != clone[i] {
			t.Fatalf("SortEntries mutated its input at index %d: %+v", i, original[i])
		}
	}
}

func TestStatusCodeWordCoversEveryStatusCode(t *testing.T) {
	registerStateStrings(t)
	tests := map[worktree.StatusCode]string{
		worktree.StatusModified:    "Modified",
		worktree.StatusAdded:       "Added",
		worktree.StatusDeleted:     "Deleted",
		worktree.StatusRenamed:     "Renamed",
		worktree.StatusCopied:      "Copied",
		worktree.StatusTypeChanged: "TypeChanged",
		worktree.StatusUnmerged:    "Conflict",
		worktree.StatusUntracked:   "Untracked",
		worktree.StatusIgnored:     "Ignored",
		worktree.StatusUnmodified:  "Unmodified",
	}
	for code, want := range tests {
		if got := statusCodeWord(code); got != want {
			t.Fatalf("statusCodeWord(%v) = %q, want %q", code, got, want)
		}
	}
}

func TestDiffStatusWordFallsBackToUnmodifiedForAnUnknownStatus(t *testing.T) {
	registerStateStrings(t)
	if got := diffStatusWord(diff.Status(99)); got != "Unmodified" {
		t.Fatalf("diffStatusWord(unknown) = %q, want Unmodified", got)
	}
}

func TestExtensionOfIgnoresALeadingDotOnly(t *testing.T) {
	if got := extensionOf(".gitignore"); got != "" {
		t.Fatalf("extensionOf(.gitignore) = %q, want empty", got)
	}
	if got := extensionOf("archive.tar.gz"); got != "gz" {
		t.Fatalf("extensionOf(archive.tar.gz) = %q, want gz", got)
	}
	if got := extensionOf("noext"); got != "" {
		t.Fatalf("extensionOf(noext) = %q, want empty", got)
	}
}

func TestSizeStringIsEmptyForZeroOrNegativeSizes(t *testing.T) {
	if got := sizeString(0); got != "" {
		t.Fatalf("sizeString(0) = %q, want empty", got)
	}
	if got := sizeString(-1); got != "" {
		t.Fatalf("sizeString(-1) = %q, want empty", got)
	}
	if got := sizeString(5); got != "5" {
		t.Fatalf("sizeString(5) = %q, want 5", got)
	}
}
