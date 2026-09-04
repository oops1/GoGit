package worktree

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/object"
)

func TestWorkingFileReturnsContentForARegularFile(t *testing.T) {
	tr := newTestRepo(t)
	tr.writeFile("a.txt", "hello\n")
	w := tr.open()

	data, ok, err := w.WorkingFile("a.txt")

	if err != nil || !ok || string(data) != "hello\n" {
		t.Fatalf("WorkingFile = %q, %v, %v", data, ok, err)
	}
}

func TestWorkingFileReportsAbsenceForAMissingFile(t *testing.T) {
	tr := newTestRepo(t)
	w := tr.open()

	data, ok, err := w.WorkingFile("missing.txt")

	if err != nil || ok || data != nil {
		t.Fatalf("WorkingFile = %q, %v, %v, want nil, false, nil", data, ok, err)
	}
}

func TestWorkingFileReportsAbsenceForADirectory(t *testing.T) {
	tr := newTestRepo(t)
	tr.mkdir("sub")
	w := tr.open()

	data, ok, err := w.WorkingFile("sub")

	if err != nil || ok || data != nil {
		t.Fatalf("WorkingFile = %q, %v, %v, want nil, false, nil", data, ok, err)
	}
}

func TestWorkingFileFailsWhenLstatFails(t *testing.T) {
	tr := newTestRepo(t)
	w := tr.open()
	original := fsLstatFile
	t.Cleanup(func() { fsLstatFile = original })
	fsLstatFile = func(root *os.Root, name string) (os.FileInfo, error) {
		return nil, errors.New("boom")
	}

	if _, _, err := w.WorkingFile("a.txt"); err == nil {
		t.Fatal("WorkingFile returned no error")
	}
}

func TestWorkingFileReadsASymlinkTargetUsingFaultInjection(t *testing.T) {
	tr := newTestRepo(t)
	tr.writeFile("link", "placeholder")
	w := tr.open()
	originalLstat, originalReadlink := fsLstatFile, fsReadlinkFile
	t.Cleanup(func() {
		fsLstatFile = originalLstat
		fsReadlinkFile = originalReadlink
	})
	fsLstatFile = func(root *os.Root, name string) (os.FileInfo, error) {
		return fakeFileInfo{mode: os.ModeSymlink}, nil
	}
	fsReadlinkFile = func(root *os.Root, name string) (string, error) { return "target.txt", nil }

	data, ok, err := w.WorkingFile("link")

	if err != nil || !ok || string(data) != "target.txt" {
		t.Fatalf("WorkingFile = %q, %v, %v", data, ok, err)
	}
}

func TestWorkingFileFailsWhenReadlinkFails(t *testing.T) {
	tr := newTestRepo(t)
	tr.writeFile("link", "placeholder")
	w := tr.open()
	originalLstat, originalReadlink := fsLstatFile, fsReadlinkFile
	t.Cleanup(func() {
		fsLstatFile = originalLstat
		fsReadlinkFile = originalReadlink
	})
	fsLstatFile = func(root *os.Root, name string) (os.FileInfo, error) {
		return fakeFileInfo{mode: os.ModeSymlink}, nil
	}
	fsReadlinkFile = func(root *os.Root, name string) (string, error) { return "", errors.New("boom") }

	if _, _, err := w.WorkingFile("link"); err == nil {
		t.Fatal("WorkingFile returned no error")
	}
}

func TestWorkingFileFailsWhenReadFileFails(t *testing.T) {
	tr := newTestRepo(t)
	tr.writeFile("a.txt", "hello\n")
	w := tr.open()
	original := fsReadFileFile
	t.Cleanup(func() { fsReadFileFile = original })
	fsReadFileFile = func(root *os.Root, name string) ([]byte, error) { return nil, errors.New("boom") }

	if _, _, err := w.WorkingFile("a.txt"); err == nil {
		t.Fatal("WorkingFile returned no error")
	}
}

func TestHeadBlobReturnsZeroWithoutAnyCommit(t *testing.T) {
	tr := newTestRepo(t)
	w := tr.open()

	mode, id, err := w.HeadBlob(t.Context(), "a.txt")

	if err != nil || mode != 0 || !id.IsZero() {
		t.Fatalf("HeadBlob = %v, %s, %v, want 0, zero, nil", mode, id, err)
	}
}

func TestHeadBlobFailsWhenTheContextIsAlreadyCancelled(t *testing.T) {
	tr := newTestRepo(t)
	tr.stage("a.txt", "hi\n")
	tr.commit("initial")
	w := tr.open()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if _, _, err := w.HeadBlob(ctx, "a.txt"); !errors.Is(err, context.Canceled) {
		t.Fatalf("HeadBlob returned error %v, want context.Canceled", err)
	}
}

func TestHeadBlobFailsWhenHeadCannotBeResolved(t *testing.T) {
	tr := newTestRepo(t)
	tr.stage("a.txt", "hi\n")
	tr.commit("initial")
	if err := os.WriteFile(tr.repo.GitPath("HEAD"), []byte("ref: refs/heads/a\n"), 0o666); err != nil {
		t.Fatalf("WriteFile returned error %v", err)
	}
	if err := os.WriteFile(tr.repo.CommonPath("refs/heads/a"), []byte("ref: refs/heads/b\n"), 0o666); err != nil {
		t.Fatalf("WriteFile returned error %v", err)
	}
	if err := os.WriteFile(tr.repo.CommonPath("refs/heads/b"), []byte("ref: refs/heads/a\n"), 0o666); err != nil {
		t.Fatalf("WriteFile returned error %v", err)
	}
	w := tr.open()

	if _, _, err := w.HeadBlob(t.Context(), "a.txt"); err == nil {
		t.Fatal("HeadBlob returned no error")
	}
}

func TestHeadBlobFailsWhenTheHeadCommitObjectIsMissing(t *testing.T) {
	tr := newTestRepo(t)
	tr.stage("a.txt", "hi\n")
	tr.commit("initial")
	blobID, err := tr.db.Put(object.TypeBlob, []byte("not a commit"))
	if err != nil {
		t.Fatalf("Put returned error %v", err)
	}
	tr.setBranchTarget(blobID)
	w := tr.open()

	if _, _, err := w.HeadBlob(t.Context(), "a.txt"); err == nil {
		t.Fatal("HeadBlob returned no error")
	}
}

func TestHeadBlobFindsATopLevelFile(t *testing.T) {
	tr := newTestRepo(t)
	tr.stage("a.txt", "hi\n")
	tr.commit("initial")
	w := tr.open()

	mode, id, err := w.HeadBlob(t.Context(), "a.txt")

	if err != nil || mode != object.ModeBlob || id.IsZero() {
		t.Fatalf("HeadBlob = %v, %s, %v", mode, id, err)
	}
}

func TestHeadBlobReturnsZeroForAMissingTopLevelFile(t *testing.T) {
	tr := newTestRepo(t)
	tr.stage("a.txt", "hi\n")
	tr.commit("initial")
	w := tr.open()

	mode, id, err := w.HeadBlob(t.Context(), "missing.txt")

	if err != nil || mode != 0 || !id.IsZero() {
		t.Fatalf("HeadBlob = %v, %s, %v, want 0, zero, nil", mode, id, err)
	}
}

func TestHeadBlobFindsANestedFile(t *testing.T) {
	tr := newTestRepo(t)
	tr.stage("dir/a.txt", "hi\n")
	tr.commit("initial")
	w := tr.open()

	mode, id, err := w.HeadBlob(t.Context(), "dir/a.txt")

	if err != nil || mode != object.ModeBlob || id.IsZero() {
		t.Fatalf("HeadBlob = %v, %s, %v", mode, id, err)
	}
}

func TestHeadBlobReturnsZeroWhenAnIntermediateComponentIsNotADirectory(t *testing.T) {
	tr := newTestRepo(t)
	tr.stage("a.txt", "hi\n")
	tr.commit("initial")
	w := tr.open()

	mode, id, err := w.HeadBlob(t.Context(), "a.txt/child.txt")

	if err != nil || mode != 0 || !id.IsZero() {
		t.Fatalf("HeadBlob = %v, %s, %v, want 0, zero, nil", mode, id, err)
	}
}

func TestHeadBlobFailsWhenANestedTreeObjectIsMissing(t *testing.T) {
	tr := newTestRepo(t)
	blobID, err := tr.db.Put(object.TypeBlob, []byte("not a tree"))
	if err != nil {
		t.Fatalf("Put returned error %v", err)
	}
	badTree := &object.Tree{Entries: []object.TreeEntry{{Mode: object.ModeTree, Name: "broken", ID: blobID}}}
	badTreeID, err := tr.db.PutObject(badTree)
	if err != nil {
		t.Fatalf("PutObject returned error %v", err)
	}
	tr.commitWithTree(badTreeID, "broken")
	w := tr.open()

	if _, _, err := w.HeadBlob(t.Context(), "broken/child.txt"); err == nil {
		t.Fatal("HeadBlob returned no error")
	}
}

func TestTreeLookupFailsWhenTheContextIsCancelledDuringRecursion(t *testing.T) {
	tr := newTestRepo(t)
	tr.stage("dir/a.txt", "hi\n")
	commitID := tr.commit("initial")
	commit, err := tr.db.Commit(commitID)
	if err != nil {
		t.Fatalf("Commit returned error %v", err)
	}
	w := tr.open()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if _, _, err := w.treeLookup(ctx, commit.Tree, "dir/a.txt"); !errors.Is(err, context.Canceled) {
		t.Fatalf("treeLookup returned error %v, want context.Canceled", err)
	}
}
