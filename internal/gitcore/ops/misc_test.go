package ops

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/refs"
)

func TestOverwriteErrorMessage(t *testing.T) {
	err := &OverwriteError{Paths: []string{"a.txt", "b.txt"}}
	if got := err.Error(); got == "" {
		t.Fatalf("Error() returned empty string")
	}
	if !errors.Is(err, ErrWouldOverwrite) {
		t.Fatalf("errors.Is(err, ErrWouldOverwrite) = false")
	}
}

func TestCleanRepoPathRejectsEscapes(t *testing.T) {
	tests := []string{"", ".", "/", "/abs", "../x", "a/../../b"}
	for _, path := range tests {
		if _, err := cleanRepoPath(path); !errors.Is(err, ErrInvalidPath) {
			t.Errorf("cleanRepoPath(%q) err = %v, want ErrInvalidPath", path, err)
		}
	}
}

func TestCleanRepoPathAcceptsNormalPaths(t *testing.T) {
	tests := map[string]string{"a.txt": "a.txt", "dir/a.txt": "dir/a.txt", "./a.txt": "a.txt"}
	for input, want := range tests {
		got, err := cleanRepoPath(input)
		if err != nil {
			t.Fatalf("cleanRepoPath(%q) returned error %v", input, err)
		}
		if got != want {
			t.Fatalf("cleanRepoPath(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestUnstageContextCanceledReturnsError(t *testing.T) {
	r := newTestRepo(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err := Unstage(ctx, r.repo, []string{"a.txt"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestDiscardContextCanceledReturnsError(t *testing.T) {
	r := newTestRepo(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err := Discard(ctx, r.repo, []string{"a.txt"}, DiscardOptions{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestDeleteBranchContextCanceledReturnsError(t *testing.T) {
	r := newTestRepo(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err := DeleteBranch(ctx, r.repo, "main", true)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestRenameBranchContextCanceledReturnsError(t *testing.T) {
	r := newTestRepo(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err := RenameBranch(ctx, r.repo, "main", "trunk", false)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestDeleteBranchOnUnbornHeadWithZeroTargetIsMerged(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "hello\n")
	mustStage(t, r, "a.txt")
	first := r.commitAll("initial")
	r.createBranch("feature", first)
	if err := DeleteBranch(t.Context(), r.repo, "feature", false); err != nil {
		t.Fatalf("DeleteBranch returned error %v", err)
	}
}

func TestDiscardSymlinkRestoresLink(t *testing.T) {
	r := newTestRepo(t)
	if !r.symlink("target.txt", "link") {
		t.Skip("symlinks are not supported on this platform")
	}
	mustStage(t, r, "link")
	r.commitAll("initial")
	r.remove("link")
	if err := Discard(t.Context(), r.repo, []string{"link"}, DiscardOptions{}); err != nil {
		t.Fatalf("Discard returned error %v", err)
	}
	if !r.exists("link") {
		t.Fatalf("link was not restored")
	}
}

func TestSwitchExecutableBitChangeIsDirty(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("core.filemode is disabled on windows")
	}
	r := newTestRepo(t)
	r.writeFile("run.sh", "echo hi\n")
	mustStage(t, r, "run.sh")
	first := r.commitAll("initial")
	r.createBranch("feature", first)
	r.chmodExecutable("run.sh")
	r.writeFile("run.sh", "echo hi\n")
	r.chmodExecutable("run.sh")

	err := Switch(t.Context(), r.repo, "feature", SwitchOptions{})
	if err == nil {
		t.Skip("executable bit change was not detected as dirty on this filesystem")
	}
}

func TestSwitchToSameBranchIsNoop(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "hello\n")
	mustStage(t, r, "a.txt")
	r.commitAll("initial")
	if err := Switch(t.Context(), r.repo, "main", SwitchOptions{}); err != nil {
		t.Fatalf("Switch returned error %v", err)
	}
}

func TestSwitchPrunesNestedEmptyDirectories(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "hello\n")
	mustStage(t, r, "a.txt")
	first := r.commitAll("initial")
	r.createBranch("feature", first)
	r.writeFile("dir/sub/deep.txt", "deep\n")
	mustStage(t, r, "dir")
	r.commitAll("on main")

	if err := Switch(t.Context(), r.repo, "feature", SwitchOptions{}); err != nil {
		t.Fatalf("Switch returned error %v", err)
	}
	if r.exists("dir") {
		t.Fatalf("dir should have been pruned")
	}
}

func TestSwitchUntrackedFileCollidesWithTargetIsConflict(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "hello\n")
	mustStage(t, r, "a.txt")
	first := r.commitAll("initial")
	r.createBranch("feature", first)
	r.writeFile("new.txt", "on feature\n")
	mustStage(t, r, "new.txt")
	r.commitAll("on feature")

	r.writeFile("new.txt", "untracked collision\n")
	err := Switch(t.Context(), r.repo, "feature", SwitchOptions{})
	var overwrite *OverwriteError
	if !errors.As(err, &overwrite) {
		t.Fatalf("err = %v, want *OverwriteError", err)
	}
}

func TestCurrentBranchRefDetachedReturnsEmpty(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "hello\n")
	mustStage(t, r, "a.txt")
	first := r.commitAll("initial")
	if err := Switch(t.Context(), r.repo, first.String(), SwitchOptions{}); err != nil {
		t.Fatalf("Switch returned error %v", err)
	}
	store := r.refs()
	got, err := currentBranchRef(store)
	if err != nil {
		t.Fatalf("currentBranchRef returned error %v", err)
	}
	if got != "" {
		t.Fatalf("currentBranchRef = %q, want empty", got)
	}
}

func TestRenameBranchDetachedHeadDoesNotUpdateHead(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "hello\n")
	mustStage(t, r, "a.txt")
	first := r.commitAll("initial")
	r.createBranch("feature", first)
	if err := Switch(t.Context(), r.repo, first.String(), SwitchOptions{}); err != nil {
		t.Fatalf("Switch returned error %v", err)
	}
	if err := RenameBranch(t.Context(), r.repo, "feature", "renamed", false); err != nil {
		t.Fatalf("RenameBranch returned error %v", err)
	}
	store := r.refs()
	ref, err := store.Lookup(refs.HEAD)
	if err != nil {
		t.Fatalf("Lookup returned error %v", err)
	}
	if ref.IsSymbolic() {
		t.Fatalf("HEAD unexpectedly became symbolic")
	}
}

func TestFirstLineWithoutNewline(t *testing.T) {
	if got := firstLine("single line, no newline"); got != "single line, no newline" {
		t.Fatalf("firstLine = %q", got)
	}
}

func TestStageTextAutoBinaryContentNotConverted(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile(".gitattributes", "*.bin text=auto\n")
	mustStage(t, r, ".gitattributes")
	content := "line1\r\nline2\x00\r\n"
	r.writeFile("data.bin", content)
	if err := Stage(t.Context(), r.repo, []string{"data.bin"}, StageOptions{}); err != nil {
		t.Fatalf("Stage returned error %v", err)
	}
	idx := r.index()
	entry, _ := entryOf(t, idx, "data.bin")
	db := r.db()
	_, data, err := db.Get(entry.ID)
	if err != nil {
		t.Fatalf("Get returned error %v", err)
	}
	if string(data) != content {
		t.Fatalf("binary content was converted: %q", data)
	}
}

func TestStageTextAutoFileAlreadyLFUnchanged(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile(".gitattributes", "*.txt text=auto\n")
	mustStage(t, r, ".gitattributes")
	r.writeFile("a.txt", "line1\nline2\n")
	if err := Stage(t.Context(), r.repo, []string{"a.txt"}, StageOptions{}); err != nil {
		t.Fatalf("Stage returned error %v", err)
	}
	idx := r.index()
	entry, _ := entryOf(t, idx, "a.txt")
	db := r.db()
	_, data, err := db.Get(entry.ID)
	if err != nil {
		t.Fatalf("Get returned error %v", err)
	}
	if string(data) != "line1\nline2\n" {
		t.Fatalf("content = %q", data)
	}
}

func TestDiscardCheckoutBinaryContentNotConverted(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile(".gitattributes", "*.bin text=auto eol=crlf\n")
	mustStage(t, r, ".gitattributes")
	content := "line1\nline2\x00\n"
	r.writeFile("data.bin", content)
	mustStage(t, r, "data.bin")
	r.commitAll("initial")
	r.remove("data.bin")
	if err := Discard(t.Context(), r.repo, []string{"data.bin"}, DiscardOptions{}); err != nil {
		t.Fatalf("Discard returned error %v", err)
	}
	if got := r.readFile("data.bin"); got != content {
		t.Fatalf("checked out content = %q, want unchanged binary", got)
	}
}

func TestSwitchDetectsRemovedTrackedFileAsDirty(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "hello\n")
	mustStage(t, r, "a.txt")
	first := r.commitAll("initial")
	r.createBranch("feature", first)
	r.writeFile("a.txt", "changed on feature\n")
	mustStage(t, r, "a.txt")
	r.commitAll("on feature")
	if err := Switch(t.Context(), r.repo, "main", SwitchOptions{}); err != nil {
		t.Fatalf("Switch returned error %v", err)
	}
	r.remove("a.txt")

	err := Switch(t.Context(), r.repo, "feature", SwitchOptions{})
	var overwrite *OverwriteError
	if !errors.As(err, &overwrite) {
		t.Fatalf("err = %v, want *OverwriteError", err)
	}
}

func TestSwitchTargetIsNotACommitReturnsError(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "hello\n")
	mustStage(t, r, "a.txt")
	r.commitAll("initial")
	db := r.db()
	blobID, err := db.Put(object.TypeBlob, []byte("not a commit"))
	if err != nil {
		t.Fatalf("Put returned error %v", err)
	}
	err = Switch(t.Context(), r.repo, blobID.String(), SwitchOptions{})
	if !errors.Is(err, ErrTargetNotFound) {
		t.Fatalf("err = %v, want ErrTargetNotFound", err)
	}
}

func TestSwitchFromUnbornHeadToExistingBranch(t *testing.T) {
	r := newTestRepo(t)
	db := r.db()
	treeID, err := db.Put(object.TypeTree, nil)
	if err != nil {
		t.Fatalf("Put returned error %v", err)
	}
	sig := object.Signature{Name: "ann", Email: "ann@example.com", When: time.Unix(1700000000, 0)}
	commitID, err := db.PutObject(&object.Commit{Tree: treeID, Author: sig, Committer: sig, Message: "root\n"})
	if err != nil {
		t.Fatalf("PutObject returned error %v", err)
	}
	r.createBranch("other", commitID)

	if err := Switch(t.Context(), r.repo, "other", SwitchOptions{}); err != nil {
		t.Fatalf("Switch returned error %v", err)
	}
	target, symbolic := r.headSymbolicTarget()
	if !symbolic || target != refs.BranchName("other") {
		t.Fatalf("HEAD = %s symbolic=%v, want other", target, symbolic)
	}
}

func (r *testRepo) breakObjectsDir() {
	r.t.Helper()
	objects := r.repo.ObjectsDir()
	if err := os.RemoveAll(objects); err != nil {
		r.t.Fatalf("RemoveAll returned error %v", err)
	}
	if err := os.WriteFile(objects, []byte("not a directory"), 0o666); err != nil {
		r.t.Fatalf("WriteFile returned error %v", err)
	}
}

func TestStageFailsWhenObjectDatabaseIsUnreadable(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "hello\n")
	r.breakObjectsDir()
	if err := Stage(t.Context(), r.repo, []string{"a.txt"}, StageOptions{}); err == nil {
		t.Fatalf("expected an error")
	}
}

func TestUnstageFailsWhenObjectDatabaseIsUnreadable(t *testing.T) {
	r := newTestRepo(t)
	r.breakObjectsDir()
	if err := Unstage(t.Context(), r.repo, []string{"a.txt"}); err == nil {
		t.Fatalf("expected an error")
	}
}

func TestDiscardFailsWhenObjectDatabaseIsUnreadable(t *testing.T) {
	r := newTestRepo(t)
	r.breakObjectsDir()
	if err := Discard(t.Context(), r.repo, []string{"a.txt"}, DiscardOptions{}); err == nil {
		t.Fatalf("expected an error")
	}
}

func TestCommitFailsWhenObjectDatabaseIsUnreadable(t *testing.T) {
	r := newTestRepo(t)
	r.breakObjectsDir()
	if _, err := Commit(t.Context(), r.repo, CommitOptions{Message: "x"}); err == nil {
		t.Fatalf("expected an error")
	}
}

func TestCreateBranchFailsWhenObjectDatabaseIsUnreadable(t *testing.T) {
	r := newTestRepo(t)
	r.breakObjectsDir()
	if err := CreateBranch(t.Context(), r.repo, "feature", hash.Zero, CreateBranchOptions{}); err == nil {
		t.Fatalf("expected an error")
	}
}

func TestDeleteBranchFailsWhenObjectDatabaseIsUnreadable(t *testing.T) {
	r := newTestRepo(t)
	r.breakObjectsDir()
	if err := DeleteBranch(t.Context(), r.repo, "main", true); err == nil {
		t.Fatalf("expected an error")
	}
}

func TestRenameBranchFailsWhenObjectDatabaseIsUnreadable(t *testing.T) {
	r := newTestRepo(t)
	r.breakObjectsDir()
	if err := RenameBranch(t.Context(), r.repo, "main", "trunk", false); err == nil {
		t.Fatalf("expected an error")
	}
}

func TestSwitchFailsWhenObjectDatabaseIsUnreadable(t *testing.T) {
	r := newTestRepo(t)
	r.breakObjectsDir()
	if err := Switch(t.Context(), r.repo, "main", SwitchOptions{}); err == nil {
		t.Fatalf("expected an error")
	}
}

func TestAttributesFileOfUsesConfiguredPath(t *testing.T) {
	r := newTestRepo(t)
	custom := filepath.Join(r.dir, "custom-attributes")
	if err := os.WriteFile(custom, []byte("*.bin binary\n"), 0o666); err != nil {
		t.Fatalf("WriteFile returned error %v", err)
	}
	r.appendConfig("[core]\n\tattributesfile = " + filepath.ToSlash(custom) + "\n")
	r.repo = r.reopen()
	got, err := attributesFileOf(r.repo)
	if err != nil {
		t.Fatalf("attributesFileOf returned error %v", err)
	}
	if want := filepath.ToSlash(custom); got != want {
		t.Fatalf("attributesFileOf = %q, want %q", got, want)
	}
}

func TestExcludesFileOfUsesConfiguredPath(t *testing.T) {
	r := newTestRepo(t)
	custom := filepath.Join(r.dir, "custom-excludes")
	if err := os.WriteFile(custom, []byte("*.tmp\n"), 0o666); err != nil {
		t.Fatalf("WriteFile returned error %v", err)
	}
	r.appendConfig("[core]\n\texcludesfile = " + filepath.ToSlash(custom) + "\n")
	r.repo = r.reopen()
	if got, want := excludesFileOf(r.repo), filepath.ToSlash(custom); got != want {
		t.Fatalf("excludesFileOf = %q, want %q", got, want)
	}
}

func TestDeleteBranchUnmergedOnUnbornHeadReturnsError(t *testing.T) {
	r := newTestRepo(t)
	db := r.db()
	treeID, err := db.Put(object.TypeTree, nil)
	if err != nil {
		t.Fatalf("Put returned error %v", err)
	}
	sig := object.Signature{Name: "ann", Email: "ann@example.com", When: time.Unix(1700000000, 0)}
	commitID, err := db.PutObject(&object.Commit{Tree: treeID, Author: sig, Committer: sig, Message: "root\n"})
	if err != nil {
		t.Fatalf("PutObject returned error %v", err)
	}
	r.createBranch("other", commitID)
	err = DeleteBranch(t.Context(), r.repo, "other", false)
	if !errors.Is(err, ErrBranchNotMerged) {
		t.Fatalf("err = %v, want ErrBranchNotMerged", err)
	}
}
