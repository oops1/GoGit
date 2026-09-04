package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/config"
	"github.com/oops1/gogit/internal/gitcore/diff"
	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/index"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/worktree"
	"github.com/oops1/gogit/internal/ui/changes"
)

func openTestRepository(t *testing.T, target string) *openedRepository {
	t.Helper()
	o, _, err := openRepositoryAt("r1", target)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = o.close() })
	return o
}

func TestStartWorkingClearsTheGridWhenTheRepositoryHasNoWorktree(t *testing.T) {
	a := newTestApp(t)

	a.startWorking()

	if filesModeOnDispatcher(a) != filesModeWorking {
		t.Fatal("filesMode must reset to working")
	}
	if got := filesRowCountOnDispatcher(t, a); got != 0 {
		t.Fatalf("files grid = %d rows, want 0", got)
	}
}

func TestShowWorkingDiffClearsTheDiffViewWithoutAnOpenRepository(t *testing.T) {
	a := newTestApp(t)
	a.diffView.SetDocument(changes.FromFile(diff.File{OldPath: "stale.txt"}))

	a.showWorkingDiff(worktree.Entry{Path: "x.txt"})

	if doc := a.diffView.Document(); !doc.IsEmpty() {
		t.Fatalf("diff view = %+v, want cleared", doc)
	}
}

func TestRunWorkingTruncatesEntriesToMaxFiles(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	initTestRepoWithBranch(t, target, "main")
	for i := range changes.MaxFiles + 5 {
		if err := writeFile(target, fmt.Sprintf("file%04d.txt", i), "x\n"); err != nil {
			t.Fatal(err)
		}
	}
	a := activatedWorkingApp(t, target)

	waitForWorkingRows(t, a, changes.MaxFiles+1)

	if got := filesRowCountOnDispatcher(t, a); got != changes.MaxFiles+1 {
		t.Fatalf("rows = %d, want %d (MaxFiles rows plus the truncation marker)", got, changes.MaxFiles+1)
	}
	a.filesMu.Lock()
	entries := len(a.currentEntries)
	a.filesMu.Unlock()
	if entries != changes.MaxFiles {
		t.Fatalf("currentEntries = %d, want %d", entries, changes.MaxFiles)
	}
}

func TestIndexDiffFailsWhenHeadCannotBeResolved(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	initTestRepoWithBranch(t, target, "main")
	db, store := withJournalRepo(t, target)
	tree := putChangesTree(t, db, map[string]string{"a.txt": "hi\n"})
	root := putChangesCommit(t, db, tree)
	setRef(t, store, refs.BranchName("main"), root)
	o := openTestRepository(t, target)
	if err := os.WriteFile(filepath.Join(target, ".git", "HEAD"), []byte("ref: refs/heads/a\n"), 0o666); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, ".git", "refs", "heads", "a"), []byte("ref: refs/heads/b\n"), 0o666); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, ".git", "refs", "heads", "b"), []byte("ref: refs/heads/a\n"), 0o666); err != nil {
		t.Fatal(err)
	}

	if _, err := indexDiff(t.Context(), o, worktree.Entry{Path: "a.txt"}); err == nil {
		t.Fatal("indexDiff returned no error")
	}
}

func TestIndexDiffFailsWhenTheHeadBlobIsMissingFromTheObjectDatabase(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	initTestRepoWithBranch(t, target, "main")
	db, store := withJournalRepo(t, target)
	ghostTree := &object.Tree{Entries: []object.TreeEntry{{Mode: object.ModeBlob, Name: "ghost.txt", ID: oid(t, "ff")}}}
	ghostTreeID, err := db.PutObject(ghostTree)
	if err != nil {
		t.Fatal(err)
	}
	root := putChangesCommit(t, db, ghostTreeID)
	setRef(t, store, refs.BranchName("main"), root)
	o := openTestRepository(t, target)

	if _, err := indexDiff(t.Context(), o, worktree.Entry{Path: "ghost.txt"}); !errors.Is(err, odb.ErrNotFound) {
		t.Fatalf("indexDiff returned error %v, want odb.ErrNotFound", err)
	}
}

func TestIndexDiffFailsWhenTheIndexBlobIsMissingFromTheObjectDatabase(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	initTestRepoWithBranch(t, target, "main")
	writeGhostIndexEntry(t, target, "ghost.txt", index.StageMerged)
	o := openTestRepository(t, target)

	if _, err := indexDiff(t.Context(), o, worktree.Entry{Path: "ghost.txt"}); !errors.Is(err, odb.ErrNotFound) {
		t.Fatalf("indexDiff returned error %v, want odb.ErrNotFound", err)
	}
}

func TestWorkTreeDiffFailsWhenTheIndexBlobIsMissingFromTheObjectDatabase(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	initTestRepoWithBranch(t, target, "main")
	writeGhostIndexEntry(t, target, "ghost.txt", index.StageMerged)
	o := openTestRepository(t, target)

	if _, err := workTreeDiff(t.Context(), o, worktree.Entry{Path: "ghost.txt", Unstaged: worktree.StatusModified}); !errors.Is(err, odb.ErrNotFound) {
		t.Fatalf("workTreeDiff returned error %v, want odb.ErrNotFound", err)
	}
}

func writeGhostIndexEntry(t *testing.T, target, path string, stage index.Stage) {
	t.Helper()
	idx := index.New(index.Version2)
	idx.Add(index.Entry{Path: path, Mode: object.ModeBlob, ID: oid(t, "aa"), Stage: stage})
	if err := idx.WriteFile(filepath.Join(target, ".git", "index"), index.Version2); err != nil {
		t.Fatal(err)
	}
}

func TestBlobVsWorktreeDiffFailsWhenTheContextIsAlreadyCancelled(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	initTestRepoWithBranch(t, target, "main")
	o := openTestRepository(t, target)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if _, err := blobVsWorktreeDiff(ctx, o, "a.txt", hash.Zero); !errors.Is(err, context.Canceled) {
		t.Fatalf("blobVsWorktreeDiff returned error %v, want context.Canceled", err)
	}
}

func TestRunWorkingDiffLogsAWarningForARealError(t *testing.T) {
	widget.ClearStrings()
	t.Cleanup(widget.ClearStrings)
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	initTestRepoWithBranch(t, target, "main")
	writeGhostIndexEntry(t, target, "ghost.txt", index.StageMerged)
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	cfg := config.Default()
	cfg.Repositories = []config.Repository{{ID: "r1", Name: "Main", Path: target}}
	a, err := New(cfg, config.Paths{Dir: t.TempDir()}, logger)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)
	a.ActivateRepository("r1")

	a.runWorkingDiff(t.Context(), a.opened(), worktree.Entry{Path: "ghost.txt"})

	if !strings.Contains(buf.String(), "load working diff failed") {
		t.Fatalf("expected the working diff error to be logged: %s", buf.String())
	}
}

func TestBuildDiffFileReturnsAnEmptyFileWhenBothSidesAreAbsent(t *testing.T) {
	file := buildDiffFile("a.txt", "a.txt", nil, nil, false, false)

	if file.Binary || len(file.Hunks) != 0 {
		t.Fatalf("file = %+v, want an empty, non-binary result", file)
	}
}

func TestBuildDiffFileMarksBinaryContentWithoutComputingHunks(t *testing.T) {
	file := buildDiffFile("a.bin", "a.bin", []byte("old\x00"), []byte("new\x00"), true, true)

	if !file.Binary || len(file.Hunks) != 0 {
		t.Fatalf("file = %+v, want Binary=true and no hunks", file)
	}
}
