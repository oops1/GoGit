package changes

import (
	"testing"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/gitcore/diff"
	"github.com/oops1/gogit/internal/gitcore/worktree"
)

func TestFilesMapsStatusNameAndPathForEachStatusCode(t *testing.T) {
	tests := []struct {
		name string
		file diff.File
		want Row
	}{
		{
			name: "modified",
			file: diff.File{OldPath: "src/main.go", NewPath: "src/main.go", Status: diff.StatusModified},
			want: Row{Status: "M", Name: "main.go", Path: "src/main.go"},
		},
		{
			name: "added",
			file: diff.File{NewPath: "src/new.go", Status: diff.StatusAdded},
			want: Row{Status: "A", Name: "new.go", Path: "src/new.go"},
		},
		{
			name: "deleted",
			file: diff.File{OldPath: "src/old.go", Status: diff.StatusDeleted},
			want: Row{Status: "D", Name: "old.go", Path: "src/old.go"},
		},
		{
			name: "copied",
			file: diff.File{OldPath: "src/a.go", NewPath: "src/b.go", Status: diff.StatusCopied},
			want: Row{Status: "C", Name: "b.go", Path: "src/a.go → src/b.go"},
		},
		{
			name: "typeChanged",
			file: diff.File{OldPath: "src/link", NewPath: "src/link", Status: diff.StatusTypeChanged},
			want: Row{Status: "T", Name: "link", Path: "src/link"},
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

func TestFilesShowsOldArrowNewPathForRenames(t *testing.T) {
	rows := Files([]diff.File{{OldPath: "src/old.go", NewPath: "src/new.go", Status: diff.StatusRenamed}})

	want := Row{Status: "R", Name: "new.go", Path: "src/old.go → src/new.go"}
	if len(rows) != 1 || rows[0] != want {
		t.Fatalf("row = %+v, want %+v", rows, want)
	}
}

func TestFilesDoesNotAddAnArrowWhenARenameKeepsTheSamePath(t *testing.T) {
	rows := Files([]diff.File{{OldPath: "src/same.go", NewPath: "src/same.go", Status: diff.StatusRenamed}})

	want := Row{Status: "R", Name: "same.go", Path: "src/same.go"}
	if len(rows) != 1 || rows[0] != want {
		t.Fatalf("row = %+v, want %+v", rows, want)
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
	rows := Files(manyAddedFiles(MaxFiles))

	if len(rows) != MaxFiles {
		t.Fatalf("rows = %d, want %d", len(rows), MaxFiles)
	}
}

func TestFilesTruncatesAndAppendsAMarkerRowWhenExceedingMaxFiles(t *testing.T) {
	widget.RegisterString("en", "Diff.Truncated", "Diff truncated, showing a partial result")
	widget.SetLanguage("en")
	t.Cleanup(widget.ClearStrings)

	rows := Files(manyAddedFiles(MaxFiles + 5))

	if len(rows) != MaxFiles+1 {
		t.Fatalf("rows = %d, want %d", len(rows), MaxFiles+1)
	}
	marker := rows[MaxFiles]
	if marker.Status != "" || marker.Path != "" || marker.Name == "" {
		t.Fatalf("marker row = %+v", marker)
	}
}

func TestFilesReturnsEmptySliceForNoFiles(t *testing.T) {
	rows := Files(nil)

	if len(rows) != 0 {
		t.Fatalf("rows = %d, want 0", len(rows))
	}
}

func TestWorkingRowsMapsStatusNameAndPathForEachEntryKind(t *testing.T) {
	tests := []struct {
		name  string
		entry worktree.Entry
		want  Row
	}{
		{
			name:  "unstaged modified",
			entry: worktree.Entry{Path: "src/main.go", Staged: worktree.StatusUnmodified, Unstaged: worktree.StatusModified},
			want:  Row{Status: " M", Name: "main.go", Path: "src/main.go"},
		},
		{
			name:  "staged added",
			entry: worktree.Entry{Path: "src/new.go", Staged: worktree.StatusAdded, Unstaged: worktree.StatusUnmodified},
			want:  Row{Status: "A ", Name: "new.go", Path: "src/new.go"},
		},
		{
			name:  "staged deleted",
			entry: worktree.Entry{Path: "src/old.go", Staged: worktree.StatusDeleted, Unstaged: worktree.StatusUnmodified},
			want:  Row{Status: "D ", Name: "old.go", Path: "src/old.go"},
		},
		{
			name:  "untracked file always renders as two question marks",
			entry: worktree.Entry{Path: "src/new.go", Staged: worktree.StatusUnmodified, Unstaged: worktree.StatusUntracked},
			want:  Row{Status: "??", Name: "new.go", Path: "src/new.go"},
		},
		{
			name:  "untracked directory keeps its trailing slash in the path but not the name",
			entry: worktree.Entry{Path: "src/newdir/", IsDir: true, Staged: worktree.StatusUnmodified, Unstaged: worktree.StatusUntracked},
			want:  Row{Status: "??", Name: "newdir", Path: "src/newdir/"},
		},
		{
			name:  "conflict",
			entry: worktree.Entry{Path: "src/conf.go", Staged: worktree.StatusUnmerged, Unstaged: worktree.StatusUnmerged, Conflict: worktree.ConflictBothModified},
			want:  Row{Status: "UU", Name: "conf.go", Path: "src/conf.go"},
		},
		{
			name:  "staged rename shows old arrow new path",
			entry: worktree.Entry{Path: "src/new.go", OrigPath: "src/old.go", Staged: worktree.StatusRenamed, Unstaged: worktree.StatusUnmodified},
			want:  Row{Status: "R ", Name: "new.go", Path: "src/old.go → src/new.go"},
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
		if rows[i].Path != path {
			t.Fatalf("rows[%d].Path = %q, want %q", i, rows[i].Path, path)
		}
	}
}

func TestWorkingRowsBreaksTiesWithinTheSameRankByPath(t *testing.T) {
	status := worktree.Status{Entries: []worktree.Entry{
		{Path: "b.txt", Staged: worktree.StatusUnmodified, Unstaged: worktree.StatusModified},
		{Path: "a.txt", Staged: worktree.StatusUnmodified, Unstaged: worktree.StatusModified},
	}}

	rows := WorkingRows(status)

	if len(rows) != 2 || rows[0].Path != "a.txt" || rows[1].Path != "b.txt" {
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
	rows := WorkingRows(worktree.Status{Entries: manyModifiedEntries(MaxFiles)})

	if len(rows) != MaxFiles {
		t.Fatalf("rows = %d, want %d", len(rows), MaxFiles)
	}
}

func TestWorkingRowsTruncatesAndAppendsAMarkerRowWhenExceedingMaxFiles(t *testing.T) {
	widget.RegisterString("en", "Diff.Truncated", "Diff truncated, showing a partial result")
	widget.SetLanguage("en")
	t.Cleanup(widget.ClearStrings)

	rows := WorkingRows(worktree.Status{Entries: manyModifiedEntries(MaxFiles + 5)})

	if len(rows) != MaxFiles+1 {
		t.Fatalf("rows = %d, want %d", len(rows), MaxFiles+1)
	}
	marker := rows[MaxFiles]
	if marker.Status != "" || marker.Path != "" || marker.Name == "" {
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
