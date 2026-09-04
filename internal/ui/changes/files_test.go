package changes

import (
	"testing"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/gitcore/diff"
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
