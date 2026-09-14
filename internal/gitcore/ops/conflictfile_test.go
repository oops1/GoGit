package ops

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/merge"
)

func conflictedRepo(t *testing.T) *testRepo {
	t.Helper()
	tr := newTestRepo(t)
	tr.conflictingFork()
	if _, err := tr.merge("feature", MergeOptions{}); err != nil {
		t.Fatal(err)
	}
	return tr
}

func TestAConflictedFileComesWithAllThreeSides(t *testing.T) {
	tr := conflictedRepo(t)

	file, err := ReadConflict(t.Context(), tr.repo, "f")

	if err != nil {
		t.Fatalf("ReadConflict returned error %v", err)
	}
	if !file.HasBase || !file.HasOurs || !file.HasTheirs || file.Binary {
		t.Fatalf("file = %+v", file)
	}
	if string(file.Base) != tenLines("f") {
		t.Fatalf("base = %q", file.Base)
	}
	if string(file.Ours) != changeLine(tenLines("f"), 4, "OURS") {
		t.Fatalf("ours = %q", file.Ours)
	}
	if string(file.Theirs) != changeLine(tenLines("f"), 4, "THEIRS") {
		t.Fatalf("theirs = %q", file.Theirs)
	}
	if file.MarkerSize != merge.DefaultMarkerSize || file.Style != merge.StyleMerge {
		t.Fatalf("marker size = %d, style = %v", file.MarkerSize, file.Style)
	}
}

func TestTheBlocksOfAConflictedFileMarkTheDisputedPart(t *testing.T) {
	tr := conflictedRepo(t)

	file, err := ReadConflict(t.Context(), tr.repo, "f")

	if err != nil {
		t.Fatalf("ReadConflict returned error %v", err)
	}
	conflicts := 0
	for _, block := range file.Blocks {
		if block.Conflict {
			conflicts++
			if strings.Join(block.Ours, "") != "OURS\n" || strings.Join(block.Theirs, "") != "THEIRS\n" {
				t.Fatalf("block = %+v", block)
			}
		}
	}
	if conflicts != 1 {
		t.Fatalf("blocks = %+v, want one conflict", file.Blocks)
	}
}

func TestAConflictedFileFollowsTheConfiguredStyle(t *testing.T) {
	for style, want := range map[string]merge.Style{"diff3": merge.StyleDiff3, "zdiff3": merge.StyleZDiff3, "merge": merge.StyleMerge} {
		tr := conflictedRepo(t)
		tr.appendConfig("[merge]\n\tconflictStyle = " + style + "\n")
		tr.repo = tr.reopen()

		file, err := ReadConflict(t.Context(), tr.repo, "f")

		if err != nil {
			t.Fatalf("ReadConflict returned error %v", err)
		}
		if file.Style != want {
			t.Fatalf("style %q read back as %v", style, file.Style)
		}
	}
}

func TestAFileWithoutAConflictCannotBeRead(t *testing.T) {
	tr := conflictedRepo(t)

	if _, err := ReadConflict(t.Context(), tr.repo, "keep"); !errors.Is(err, ErrNotConflicted) {
		t.Fatalf("err = %v", err)
	}
}

func TestReadingAConflictHonoursACancelledContext(t *testing.T) {
	tr := conflictedRepo(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if _, err := ReadConflict(ctx, tr.repo, "f"); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}

func TestReadingAConflictRefusesAPathOutsideTheRepository(t *testing.T) {
	tr := conflictedRepo(t)

	if _, err := ReadConflict(t.Context(), tr.repo, "../outside"); err == nil {
		t.Fatal("a path above the repository was accepted")
	}
}

func TestAConflictOverBinaryFilesComesWithoutBlocks(t *testing.T) {
	tr := newTestRepo(t)
	tr.fork(map[string]string{"bin": "ours\x00"}, map[string]string{"bin": "theirs\x00"})
	if _, err := tr.merge("feature", MergeOptions{}); err != nil {
		t.Fatal(err)
	}

	file, err := ReadConflict(t.Context(), tr.repo, "bin")

	if err != nil {
		t.Fatalf("ReadConflict returned error %v", err)
	}
	if !file.Binary || file.Blocks != nil {
		t.Fatalf("file = %+v", file)
	}
}

func TestAConflictBetweenTwoNewFilesHasNoBase(t *testing.T) {
	tr := newTestRepo(t)
	tr.fork(map[string]string{"new": "ours\n"}, map[string]string{"new": "theirs\n"})
	if _, err := tr.merge("feature", MergeOptions{}); err != nil {
		t.Fatal(err)
	}

	file, err := ReadConflict(t.Context(), tr.repo, "new")

	if err != nil {
		t.Fatalf("ReadConflict returned error %v", err)
	}
	if file.HasBase || len(file.Base) != 0 || !file.HasOurs || !file.HasTheirs {
		t.Fatalf("file = %+v", file)
	}
}

func TestSavingAResolutionWritesTheFileAndLeavesItConflicted(t *testing.T) {
	tr := conflictedRepo(t)

	err := SaveResolution(t.Context(), tr.repo, "f", []byte("resolved\n"), ResolutionOptions{})

	if err != nil {
		t.Fatalf("SaveResolution returned error %v", err)
	}
	if got := tr.readFile("f"); got != "resolved\n" {
		t.Fatalf("file = %q", got)
	}
	if !tr.index().HasConflicts() {
		t.Fatal("the file was marked resolved without being asked")
	}
}

func TestSavingAResolutionCanMarkThePathResolved(t *testing.T) {
	tr := conflictedRepo(t)

	err := SaveResolution(t.Context(), tr.repo, "f", []byte("resolved\n"), ResolutionOptions{MarkResolved: true})

	if err != nil {
		t.Fatalf("SaveResolution returned error %v", err)
	}
	if tr.index().HasConflicts() {
		t.Fatal("the resolved path is still conflicted in the index")
	}
	if got := tr.readFile("f"); got != "resolved\n" {
		t.Fatalf("file = %q", got)
	}
}

func TestSavingAResolutionCreatesTheDirectoryOfANestedPath(t *testing.T) {
	tr := newTestRepo(t)
	tr.fork(map[string]string{"dir/f": "ours\n"}, map[string]string{"dir/f": "theirs\n"})
	if _, err := tr.merge("feature", MergeOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(tr.dir, "dir")); err != nil {
		t.Fatal(err)
	}

	err := SaveResolution(t.Context(), tr.repo, "dir/f", []byte("resolved\n"), ResolutionOptions{})

	if err != nil {
		t.Fatalf("SaveResolution returned error %v", err)
	}
	if got := tr.readFile("dir/f"); got != "resolved\n" {
		t.Fatalf("file = %q", got)
	}
}

func TestSavingAResolutionHonoursACancelledContext(t *testing.T) {
	tr := conflictedRepo(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if err := SaveResolution(ctx, tr.repo, "f", []byte("resolved\n"), ResolutionOptions{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}

func TestSavingAResolutionRefusesAPathOutsideTheRepository(t *testing.T) {
	tr := conflictedRepo(t)

	if err := SaveResolution(t.Context(), tr.repo, "../outside", nil, ResolutionOptions{}); err == nil {
		t.Fatal("a path above the repository was accepted")
	}
}
