package ops

import (
	"context"
	"errors"
	"runtime"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/object"
)

func TestStageAddsNewFile(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "hello\n")
	if err := Stage(t.Context(), r.repo, []string{"a.txt"}, StageOptions{}); err != nil {
		t.Fatalf("Stage returned error %v", err)
	}
	idx := r.index()
	entry, ok := entryOf(t, idx, "a.txt")
	if !ok {
		t.Fatalf("a.txt is not staged")
	}
	if entry.Mode != object.ModeBlob {
		t.Fatalf("mode = %s, want %s", entry.Mode, object.ModeBlob)
	}
}

func TestStageUpdatesModifiedFile(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "hello\n")
	mustStage(t, r, "a.txt")
	r.commitAll("initial")
	r.writeFile("a.txt", "goodbye\n")
	if err := Stage(t.Context(), r.repo, []string{"a.txt"}, StageOptions{}); err != nil {
		t.Fatalf("Stage returned error %v", err)
	}
	idx := r.index()
	entry, _ := entryOf(t, idx, "a.txt")
	db := r.db()
	kind, data, err := db.Get(entry.ID)
	if err != nil {
		t.Fatalf("Get returned error %v", err)
	}
	if kind != object.TypeBlob || string(data) != "goodbye\n" {
		t.Fatalf("staged content = %q", data)
	}
}

func TestStageRemovesDeletedFile(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "hello\n")
	mustStage(t, r, "a.txt")
	r.commitAll("initial")
	r.remove("a.txt")
	if err := Stage(t.Context(), r.repo, []string{"a.txt"}, StageOptions{}); err != nil {
		t.Fatalf("Stage returned error %v", err)
	}
	idx := r.index()
	if _, ok := entryOf(t, idx, "a.txt"); ok {
		t.Fatalf("a.txt is still staged")
	}
}

func TestStageDirectoryRecursively(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("dir/a.txt", "a\n")
	r.writeFile("dir/sub/b.txt", "b\n")
	if err := Stage(t.Context(), r.repo, []string{"dir"}, StageOptions{}); err != nil {
		t.Fatalf("Stage returned error %v", err)
	}
	idx := r.index()
	if _, ok := entryOf(t, idx, "dir/a.txt"); !ok {
		t.Fatalf("dir/a.txt is not staged")
	}
	if _, ok := entryOf(t, idx, "dir/sub/b.txt"); !ok {
		t.Fatalf("dir/sub/b.txt is not staged")
	}
}

func TestStageDirectoryRemovesDeletedFilesWithin(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("dir/a.txt", "a\n")
	r.writeFile("dir/b.txt", "b\n")
	mustStage(t, r, "dir")
	r.commitAll("initial")
	r.remove("dir/b.txt")
	if err := Stage(t.Context(), r.repo, []string{"dir"}, StageOptions{}); err != nil {
		t.Fatalf("Stage returned error %v", err)
	}
	idx := r.index()
	if _, ok := entryOf(t, idx, "dir/b.txt"); ok {
		t.Fatalf("dir/b.txt is still staged")
	}
	if _, ok := entryOf(t, idx, "dir/a.txt"); !ok {
		t.Fatalf("dir/a.txt was unexpectedly removed")
	}
}

func TestStageWholeDirectoryDeletionRemovesTrackedEntries(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("dir/a.txt", "a\n")
	r.writeFile("dir/sub/b.txt", "b\n")
	mustStage(t, r, "dir")
	r.commitAll("initial")
	r.remove("dir")
	if err := Stage(t.Context(), r.repo, []string{"dir"}, StageOptions{}); err != nil {
		t.Fatalf("Stage returned error %v", err)
	}
	idx := r.index()
	if idx.Len() != 0 {
		t.Fatalf("index still has %d entries", idx.Len())
	}
}

func TestStageIgnoredFileSkippedWithoutForce(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile(".gitignore", "*.log\n")
	mustStage(t, r, ".gitignore")
	r.writeFile("debug.log", "noise\n")
	if err := Stage(t.Context(), r.repo, []string{"debug.log"}, StageOptions{}); err != nil {
		t.Fatalf("Stage returned error %v", err)
	}
	idx := r.index()
	if _, ok := entryOf(t, idx, "debug.log"); ok {
		t.Fatalf("debug.log should not be staged")
	}
}

func TestStageIgnoredFileAddedWithForce(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile(".gitignore", "*.log\n")
	mustStage(t, r, ".gitignore")
	r.writeFile("debug.log", "noise\n")
	if err := Stage(t.Context(), r.repo, []string{"debug.log"}, StageOptions{Force: true}); err != nil {
		t.Fatalf("Stage returned error %v", err)
	}
	idx := r.index()
	if _, ok := entryOf(t, idx, "debug.log"); !ok {
		t.Fatalf("debug.log should be staged with Force")
	}
}

func TestStageIgnoredDirectorySkippedWithoutForce(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile(".gitignore", "build/\n")
	mustStage(t, r, ".gitignore")
	r.writeFile("build/output.bin", "binary\n")
	if err := Stage(t.Context(), r.repo, []string{"build"}, StageOptions{}); err != nil {
		t.Fatalf("Stage returned error %v", err)
	}
	idx := r.index()
	if _, ok := entryOf(t, idx, "build/output.bin"); ok {
		t.Fatalf("build/output.bin should not be staged")
	}
}

func TestStageCRLFFileNormalizedWithTextAuto(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile(".gitattributes", "*.txt text=auto\n")
	mustStage(t, r, ".gitattributes")
	r.writeFile("crlf.txt", "line1\r\nline2\r\n")
	if err := Stage(t.Context(), r.repo, []string{"crlf.txt"}, StageOptions{}); err != nil {
		t.Fatalf("Stage returned error %v", err)
	}
	idx := r.index()
	entry, _ := entryOf(t, idx, "crlf.txt")
	db := r.db()
	_, data, err := db.Get(entry.ID)
	if err != nil {
		t.Fatalf("Get returned error %v", err)
	}
	if string(data) != "line1\nline2\n" {
		t.Fatalf("staged content = %q, want normalized LF", data)
	}
}

func TestStageSymlink(t *testing.T) {
	r := newTestRepo(t)
	if !r.symlink("target.txt", "link") {
		t.Skip("symlinks are not supported on this platform")
	}
	if err := Stage(t.Context(), r.repo, []string{"link"}, StageOptions{}); err != nil {
		t.Fatalf("Stage returned error %v", err)
	}
	idx := r.index()
	entry, ok := entryOf(t, idx, "link")
	if !ok {
		t.Fatalf("link is not staged")
	}
	if entry.Mode != object.ModeSymlink {
		t.Fatalf("mode = %s, want symlink", entry.Mode)
	}
}

func TestStageExecutableBit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("core.filemode is disabled on windows")
	}
	r := newTestRepo(t)
	r.writeFile("run.sh", "echo hi\n")
	r.chmodExecutable("run.sh")
	if err := Stage(t.Context(), r.repo, []string{"run.sh"}, StageOptions{}); err != nil {
		t.Fatalf("Stage returned error %v", err)
	}
	idx := r.index()
	entry, _ := entryOf(t, idx, "run.sh")
	if entry.Mode != object.ModeExecutable {
		t.Fatalf("mode = %s, want executable", entry.Mode)
	}
}

func TestStageUpdatesCacheTree(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "hello\n")
	if err := Stage(t.Context(), r.repo, []string{"a.txt"}, StageOptions{}); err != nil {
		t.Fatalf("Stage returned error %v", err)
	}
	idx := r.index()
	if idx.CacheTree == nil || !idx.CacheTree.Valid() {
		t.Fatalf("cache tree was not updated")
	}
}

func TestStageOnBareRepositoryReturnsError(t *testing.T) {
	r := newBareTestRepo(t)
	err := Stage(t.Context(), r.repo, []string{"a.txt"}, StageOptions{})
	if !errors.Is(err, ErrBareRepository) {
		t.Fatalf("err = %v, want ErrBareRepository", err)
	}
}

func TestStageInvalidPathReturnsError(t *testing.T) {
	r := newTestRepo(t)
	err := Stage(t.Context(), r.repo, []string{"../escape"}, StageOptions{})
	if !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("err = %v, want ErrInvalidPath", err)
	}
}

func TestStageCanceledContextReturnsError(t *testing.T) {
	r := newTestRepo(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err := Stage(ctx, r.repo, []string{"a.txt"}, StageOptions{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestStageWhileIndexLockedReturnsError(t *testing.T) {
	r := newTestRepo(t)
	lock, err := lockIndex(r.repo)
	if err != nil {
		t.Fatalf("lockIndex returned error %v", err)
	}
	defer lock.abort()
	r.writeFile("a.txt", "hello\n")
	err = Stage(t.Context(), r.repo, []string{"a.txt"}, StageOptions{})
	if !errors.Is(err, ErrIndexLocked) {
		t.Fatalf("err = %v, want ErrIndexLocked", err)
	}
}

func mustStage(t testing.TB, r *testRepo, path string) {
	t.Helper()
	if err := Stage(t.Context(), r.repo, []string{path}, StageOptions{}); err != nil {
		t.Fatalf("Stage returned error %v", err)
	}
}
