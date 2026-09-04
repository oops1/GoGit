package ops

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/index"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/refs"
)

var errInjected = errors.New("ops: injected failure")

func swapOpenRoot(t testing.TB, replacement func(string) (*os.Root, error)) {
	t.Helper()
	original := fsOpenRoot
	fsOpenRoot = replacement
	t.Cleanup(func() { fsOpenRoot = original })
}

func swapRootOpenFile(t testing.TB, replacement func(*os.Root, string, int, fs.FileMode) (*os.File, error)) {
	t.Helper()
	original := fsRootOpenFile
	fsRootOpenFile = replacement
	t.Cleanup(func() { fsRootOpenFile = original })
}

func swapRootReadFile(t testing.TB, replacement func(*os.Root, string) ([]byte, error)) {
	t.Helper()
	original := fsRootReadFile
	fsRootReadFile = replacement
	t.Cleanup(func() { fsRootReadFile = original })
}

func swapRootRemove(t testing.TB, replacement func(*os.Root, string) error) {
	t.Helper()
	original := fsRootRemove
	fsRootRemove = replacement
	t.Cleanup(func() { fsRootRemove = original })
}

func swapRootRename(t testing.TB, replacement func(*os.Root, string, string) error) {
	t.Helper()
	original := fsRootRename
	fsRootRename = replacement
	t.Cleanup(func() { fsRootRename = original })
}

func swapRefsOpen(t testing.TB, replacement func(refs.Options) (*refs.Store, error)) {
	t.Helper()
	original := refsOpen
	refsOpen = replacement
	t.Cleanup(func() { refsOpen = original })
}

func swapDBPutObject(t testing.TB, replacement func(*odb.DB, object.Object) (hash.ObjectID, error)) {
	t.Helper()
	original := dbPutObject
	dbPutObject = replacement
	t.Cleanup(func() { dbPutObject = original })
}

func swapTxUpdate(t testing.TB, replacement func(*refs.Transaction, refs.Name, hash.ObjectID, hash.ObjectID) error) {
	t.Helper()
	original := txUpdate
	txUpdate = replacement
	t.Cleanup(func() { txUpdate = original })
}

func swapTxDelete(t testing.TB, replacement func(*refs.Transaction, refs.Name, hash.ObjectID) error) {
	t.Helper()
	original := txDelete
	txDelete = replacement
	t.Cleanup(func() { txDelete = original })
}

func swapTxSetSymbolic(t testing.TB, replacement func(*refs.Transaction, refs.Name, refs.Name) error) {
	t.Helper()
	original := txSetSymbolic
	txSetSymbolic = replacement
	t.Cleanup(func() { txSetSymbolic = original })
}

func swapTxDetach(t testing.TB, replacement func(*refs.Transaction, refs.Name, hash.ObjectID) error) {
	t.Helper()
	original := txDetach
	txDetach = replacement
	t.Cleanup(func() { txDetach = original })
}

func swapIdxWrite(t testing.TB, replacement func(*index.Index, io.Writer, int) error) {
	t.Helper()
	original := idxWrite
	idxWrite = replacement
	t.Cleanup(func() { idxWrite = original })
}

func swapFileClose(t testing.TB, replacement func(*os.File) error) {
	t.Helper()
	original := fsFileClose
	fsFileClose = replacement
	t.Cleanup(func() { fsFileClose = original })
}

type fakeSymlinkInfo struct{ name string }

func (f fakeSymlinkInfo) Name() string       { return f.name }
func (f fakeSymlinkInfo) Size() int64        { return 0 }
func (f fakeSymlinkInfo) Mode() fs.FileMode  { return fs.ModeSymlink }
func (f fakeSymlinkInfo) ModTime() time.Time { return time.Time{} }
func (f fakeSymlinkInfo) IsDir() bool        { return false }
func (f fakeSymlinkInfo) Sys() any           { return nil }

func TestOpenWorkingTreeFailsWhenOpenRootFails(t *testing.T) {
	r := newTestRepo(t)
	swapOpenRoot(t, func(string) (*os.Root, error) { return nil, errInjected })
	r.writeFile("a.txt", "hello\n")
	if err := Stage(t.Context(), r.repo, []string{"a.txt"}, StageOptions{}); !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want errInjected", err)
	}
}

func TestOpenWorkingTreeFailsWhenAttributesFileConfigInvalid(t *testing.T) {
	r := newTestRepo(t)
	r.appendConfig("[core]\n\tattributesfile = ~otheruser/attrs\n")
	r.repo = r.reopen()
	r.writeFile("a.txt", "hello\n")
	err := Stage(t.Context(), r.repo, []string{"a.txt"}, StageOptions{})
	if err == nil {
		t.Fatalf("expected an error")
	}
}

func TestAttributesFileOfInvalidExpansionReturnsError(t *testing.T) {
	r := newTestRepo(t)
	r.appendConfig("[core]\n\tattributesfile = ~otheruser/attrs\n")
	r.repo = r.reopen()
	if _, err := attributesFileOf(r.repo); err == nil {
		t.Fatalf("expected an error")
	}
}

func TestAttributesFileOfEmptyValueUsesDefault(t *testing.T) {
	r := newTestRepo(t)
	r.appendConfig("[core]\n\tattributesfile =\n")
	r.repo = r.reopen()
	got, err := attributesFileOf(r.repo)
	if err != nil {
		t.Fatalf("attributesFileOf returned error %v", err)
	}
	if got == "" {
		t.Fatalf("attributesFileOf returned empty default")
	}
}

func TestCheckoutConvertLeavesExistingCRLFUnchanged(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile(".gitattributes", "*.txt text=auto eol=crlf\n")
	mustStage(t, r, ".gitattributes")
	db := r.db()
	blobID, err := db.Put(object.TypeBlob, []byte("line1\r\nline2\r\n"))
	if err != nil {
		t.Fatalf("Put returned error %v", err)
	}
	idx := r.index()
	idx.Add(index.Entry{Path: "a.txt", Mode: object.ModeBlob, ID: blobID, Stage: index.StageMerged})
	r.saveIndex(idx)
	if err := Discard(t.Context(), r.repo, []string{"a.txt"}, DiscardOptions{}); err != nil {
		t.Fatalf("Discard returned error %v", err)
	}
	if got := r.readFile("a.txt"); got != "line1\r\nline2\r\n" {
		t.Fatalf("a.txt = %q, want unchanged CRLF", got)
	}
}

func TestOpenRepoContextFailsWhenRefsOpenFails(t *testing.T) {
	r := newTestRepo(t)
	swapRefsOpen(t, func(refs.Options) (*refs.Store, error) { return nil, errInjected })
	err := CreateBranch(t.Context(), r.repo, "feature", hash.Zero, CreateBranchOptions{})
	if !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want errInjected", err)
	}
}

func TestCurrentBranchRefFailsWhenHeadIsMalformed(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "hello\n")
	mustStage(t, r, "a.txt")
	first := r.commitAll("initial")
	r.createBranch("feature", first)
	r.writeRawHead("not a valid ref\n")
	err := DeleteBranch(t.Context(), r.repo, "feature", true)
	if err == nil {
		t.Fatalf("expected an error")
	}
}

func TestCreateBranchMissingIdentityReturnsError(t *testing.T) {
	r := newTestRepoNoIdentity(t)
	err := CreateBranch(t.Context(), r.repo, "feature", hash.Zero, CreateBranchOptions{})
	if !errors.Is(err, ErrMissingIdentity) {
		t.Fatalf("err = %v, want ErrMissingIdentity", err)
	}
}

func TestCreateBranchWithZeroStartPointReturnsError(t *testing.T) {
	r := newTestRepo(t)
	err := CreateBranch(t.Context(), r.repo, "feature", hash.Zero, CreateBranchOptions{})
	if err == nil {
		t.Fatalf("expected an error")
	}
}

func TestCreateBranchConflictingWithExistingBranchPathReturnsError(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "hello\n")
	mustStage(t, r, "a.txt")
	first := r.commitAll("initial")
	r.createBranch("topic", first)
	err := CreateBranch(t.Context(), r.repo, "topic/sub", first, CreateBranchOptions{})
	if err == nil || errors.Is(err, ErrBranchExists) {
		t.Fatalf("err = %v, want a generic conflict error", err)
	}
}

func TestDeleteBranchInvalidNameReturnsError(t *testing.T) {
	r := newTestRepo(t)
	err := DeleteBranch(t.Context(), r.repo, "bad..name", true)
	if !errors.Is(err, ErrInvalidBranchName) {
		t.Fatalf("err = %v, want ErrInvalidBranchName", err)
	}
}

func TestDeleteBranchFailsWhenTargetRefIsMalformed(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "hello\n")
	mustStage(t, r, "a.txt")
	r.commitAll("initial")
	r.writeRawRef("refs/heads/feature", "not a valid ref\n")
	err := DeleteBranch(t.Context(), r.repo, "feature", true)
	if err == nil {
		t.Fatalf("expected an error")
	}
}

func TestDeleteBranchFailsWhenCurrentBranchRefIsMalformed(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "hello\n")
	mustStage(t, r, "a.txt")
	first := r.commitAll("initial")
	r.createBranch("feature", first)
	r.writeRawRef("refs/heads/main", "not a valid ref\n")
	err := DeleteBranch(t.Context(), r.repo, "feature", false)
	if err == nil {
		t.Fatalf("expected an error")
	}
}

func TestDeleteBranchFailsWhenTxDeleteFails(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "hello\n")
	mustStage(t, r, "a.txt")
	first := r.commitAll("initial")
	r.createBranch("feature", first)
	swapTxDelete(t, func(*refs.Transaction, refs.Name, hash.ObjectID) error { return errInjected })
	err := DeleteBranch(t.Context(), r.repo, "feature", true)
	if !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want errInjected", err)
	}
}

func TestRenameBranchInvalidFromNameReturnsError(t *testing.T) {
	r := newTestRepo(t)
	err := RenameBranch(t.Context(), r.repo, "bad..name", "renamed", false)
	if !errors.Is(err, ErrInvalidBranchName) {
		t.Fatalf("err = %v, want ErrInvalidBranchName", err)
	}
}

func TestRenameBranchMissingIdentityReturnsError(t *testing.T) {
	r := newTestRepoNoIdentity(t)
	r.writeFile("a.txt", "hello\n")
	mustStage(t, r, "a.txt")
	err := RenameBranch(t.Context(), r.repo, "main", "trunk", false)
	if !errors.Is(err, ErrMissingIdentity) {
		t.Fatalf("err = %v, want ErrMissingIdentity", err)
	}
}

func TestRenameBranchFailsWhenSourceRefIsMalformed(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "hello\n")
	mustStage(t, r, "a.txt")
	first := r.commitAll("initial")
	r.createBranch("feature", first)
	r.writeRawRef("refs/heads/feature", "not a valid ref\n")
	err := RenameBranch(t.Context(), r.repo, "feature", "renamed", false)
	if err == nil {
		t.Fatalf("expected an error")
	}
}

func TestRenameBranchFailsWhenHeadIsMalformed(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "hello\n")
	mustStage(t, r, "a.txt")
	first := r.commitAll("initial")
	r.createBranch("feature", first)
	r.writeRawHead("not a valid ref\n")
	err := RenameBranch(t.Context(), r.repo, "feature", "renamed", false)
	if err == nil {
		t.Fatalf("expected an error")
	}
}

func TestRenameBranchFailsWhenSourceIsZeroHash(t *testing.T) {
	r := newTestRepo(t)
	r.writeRawRef("refs/heads/feature", hash.Zero.String()+"\n")
	err := RenameBranch(t.Context(), r.repo, "feature", "renamed", false)
	if err == nil {
		t.Fatalf("expected an error")
	}
}

func TestRenameBranchToSameNameReturnsError(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "hello\n")
	mustStage(t, r, "a.txt")
	first := r.commitAll("initial")
	r.createBranch("feature", first)
	err := RenameBranch(t.Context(), r.repo, "feature", "feature", false)
	if err == nil {
		t.Fatalf("expected an error")
	}
}

func TestRenameBranchFailsWhenTxSetSymbolicFails(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "hello\n")
	mustStage(t, r, "a.txt")
	r.commitAll("initial")
	swapTxSetSymbolic(t, func(*refs.Transaction, refs.Name, refs.Name) error { return errInjected })
	err := RenameBranch(t.Context(), r.repo, "main", "trunk", false)
	if !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want errInjected", err)
	}
}

func TestRenameBranchConflictingWithExistingBranchPathReturnsError(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "hello\n")
	mustStage(t, r, "a.txt")
	first := r.commitAll("initial")
	r.createBranch("topic", first)
	err := RenameBranch(t.Context(), r.repo, "main", "topic/sub", false)
	if err == nil || errors.Is(err, ErrBranchExists) {
		t.Fatalf("err = %v, want a generic conflict error", err)
	}
}

func TestCommitFailsWhileIndexLocked(t *testing.T) {
	r := newTestRepo(t)
	lock, err := lockIndex(r.repo)
	if err != nil {
		t.Fatalf("lockIndex returned error %v", err)
	}
	defer lock.abort()
	r.writeFile("a.txt", "hello\n")
	_, err = Commit(t.Context(), r.repo, CommitOptions{Message: "x"})
	if !errors.Is(err, ErrIndexLocked) {
		t.Fatalf("err = %v, want ErrIndexLocked", err)
	}
}

func TestCommitFailsWhenCacheTreeClaimsTooManyEntries(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "hello\n")
	mustStage(t, r, "a.txt")
	idx := r.index()
	if idx.CacheTree == nil || !idx.CacheTree.Valid() {
		t.Fatalf("cache tree was not built by Stage")
	}
	idx.CacheTree.EntryCount = 999
	r.saveIndex(idx)
	_, err := Commit(t.Context(), r.repo, CommitOptions{Message: "x"})
	if !errors.Is(err, index.ErrMalformed) {
		t.Fatalf("err = %v, want index.ErrMalformed", err)
	}
}

func TestCommitFailsWhenHeadFileIsMalformed(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "hello\n")
	mustStage(t, r, "a.txt")
	r.writeRawHead("not a valid ref\n")
	_, err := Commit(t.Context(), r.repo, CommitOptions{Message: "x"})
	if err == nil {
		t.Fatalf("expected an error")
	}
}

func TestCommitFailsWhenCurrentBranchRefIsMalformed(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "hello\n")
	mustStage(t, r, "a.txt")
	r.writeRawRef("refs/heads/main", "not a valid ref\n")
	_, err := Commit(t.Context(), r.repo, CommitOptions{Message: "x"})
	if err == nil {
		t.Fatalf("expected an error")
	}
}

func TestCommitAmendFailsWhenHeadCommitObjectMissing(t *testing.T) {
	r := newTestRepo(t)
	bogus := bogusObjectID(t, r.repo.ObjectFormat)
	r.createBranch("main", bogus)
	_, err := Commit(t.Context(), r.repo, CommitOptions{Message: "amend", Amend: true})
	if err == nil {
		t.Fatalf("expected an error")
	}
}

func TestCommitFailsWhenParentCommitObjectMissing(t *testing.T) {
	r := newTestRepo(t)
	bogus := bogusObjectID(t, r.repo.ObjectFormat)
	r.createBranch("main", bogus)
	r.writeFile("a.txt", "hello\n")
	mustStage(t, r, "a.txt")
	_, err := Commit(t.Context(), r.repo, CommitOptions{Message: "x"})
	if err == nil {
		t.Fatalf("expected an error")
	}
}

func TestCommitFailsWhenPutObjectFails(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "hello\n")
	mustStage(t, r, "a.txt")
	swapDBPutObject(t, func(*odb.DB, object.Object) (hash.ObjectID, error) { return hash.Zero, errInjected })
	_, err := Commit(t.Context(), r.repo, CommitOptions{Message: "x"})
	if !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want errInjected", err)
	}
}

func TestCommitFailsWhenTxUpdateFails(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "hello\n")
	mustStage(t, r, "a.txt")
	swapTxUpdate(t, func(*refs.Transaction, refs.Name, hash.ObjectID, hash.ObjectID) error { return errInjected })
	_, err := Commit(t.Context(), r.repo, CommitOptions{Message: "x"})
	if !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want errInjected", err)
	}
}

func TestCommitFailsWhenTxCommitFails(t *testing.T) {
	r := newTestRepo(t)
	db := r.db()
	blobID, err := db.Put(object.TypeBlob, []byte("x"))
	if err != nil {
		t.Fatalf("Put returned error %v", err)
	}
	if err := CreateBranch(t.Context(), r.repo, "main/sub", blobID, CreateBranchOptions{}); err != nil {
		t.Fatalf("CreateBranch returned error %v", err)
	}
	r.writeFile("a.txt", "hello\n")
	mustStage(t, r, "a.txt")
	_, err = Commit(t.Context(), r.repo, CommitOptions{Message: "x"})
	if err == nil {
		t.Fatalf("expected an error")
	}
}

func TestCommitFailsWhenLockCommitFails(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "hello\n")
	mustStage(t, r, "a.txt")
	swapRootRename(t, func(*os.Root, string, string) error { return errInjected })
	_, err := Commit(t.Context(), r.repo, CommitOptions{Message: "x"})
	if !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want errInjected", err)
	}
}

func TestIsEmptyCommitFailsWhenTreeLookupFails(t *testing.T) {
	r := newTestRepo(t)
	db := r.db()
	bogus := bogusObjectID(t, r.repo.ObjectFormat)
	if _, err := isEmptyCommit(db, bogus, nil); err == nil {
		t.Fatalf("expected an error")
	}
}

func TestCommitReflogMessageForMergeCommit(t *testing.T) {
	a, err := hash.Sum(hash.SHA1, "commit", []byte("parent-a"))
	if err != nil {
		t.Fatalf("hash.Sum returned error %v", err)
	}
	b, err := hash.Sum(hash.SHA1, "commit", []byte("parent-b"))
	if err != nil {
		t.Fatalf("hash.Sum returned error %v", err)
	}
	got := commitReflogMessage(false, []hash.ObjectID{a, b}, "merge stuff\n")
	want := "commit (merge): merge stuff"
	if got != want {
		t.Fatalf("commitReflogMessage = %q, want %q", got, want)
	}
}

func swapRootOpenFailForPath(t testing.TB, failPath string) {
	t.Helper()
	original := fsRootOpen
	fsRootOpen = func(root *os.Root, name string) (*os.File, error) {
		if filepath.ToSlash(name) == failPath {
			return nil, errInjected
		}
		return original(root, name)
	}
	t.Cleanup(func() { fsRootOpen = original })
}

func swapRootOpenFailOnNthCallForPath(t testing.TB, failPath string, n int) {
	t.Helper()
	original := fsRootOpen
	calls := 0
	fsRootOpen = func(root *os.Root, name string) (*os.File, error) {
		if filepath.ToSlash(name) == failPath {
			calls++
			if calls == n {
				return nil, errInjected
			}
		}
		return original(root, name)
	}
	t.Cleanup(func() { fsRootOpen = original })
}

func swapRootOpenFileFailForPath(t testing.TB, failPath string) {
	t.Helper()
	original := fsRootOpenFile
	fsRootOpenFile = func(root *os.Root, name string, flag int, perm fs.FileMode) (*os.File, error) {
		if filepath.ToSlash(name) == failPath {
			return nil, errInjected
		}
		return original(root, name, flag, perm)
	}
	t.Cleanup(func() { fsRootOpenFile = original })
}

func swapRootOpenFileReturnsClosedFile(t testing.TB, failPath string) {
	t.Helper()
	original := fsRootOpenFile
	fsRootOpenFile = func(root *os.Root, name string, flag int, perm fs.FileMode) (*os.File, error) {
		file, err := original(root, name, flag, perm)
		if err != nil {
			return nil, err
		}
		if filepath.ToSlash(name) == failPath {
			_ = file.Close()
		}
		return file, nil
	}
	t.Cleanup(func() { fsRootOpenFile = original })
}

func swapRootLstatFailForPath(t testing.TB, failPath string) {
	t.Helper()
	original := fsRootLstat
	fsRootLstat = func(root *os.Root, name string) (fs.FileInfo, error) {
		if filepath.ToSlash(name) == failPath {
			return nil, errInjected
		}
		return original(root, name)
	}
	t.Cleanup(func() { fsRootLstat = original })
}

func swapRootLstatSymlinkForPath(t testing.TB, path string) {
	t.Helper()
	original := fsRootLstat
	fsRootLstat = func(root *os.Root, name string) (fs.FileInfo, error) {
		if filepath.ToSlash(name) == path {
			return fakeSymlinkInfo{name: filepath.Base(name)}, nil
		}
		return original(root, name)
	}
	t.Cleanup(func() { fsRootLstat = original })
}

func swapRootReadlinkForPath(t testing.TB, path, target string) {
	t.Helper()
	original := fsRootReadlink
	fsRootReadlink = func(root *os.Root, name string) (string, error) {
		if filepath.ToSlash(name) == path {
			return target, nil
		}
		return original(root, name)
	}
	t.Cleanup(func() { fsRootReadlink = original })
}

func swapRootSymlinkSucceeds(t testing.TB) {
	t.Helper()
	original := fsRootSymlink
	fsRootSymlink = func(*os.Root, string, string) error { return nil }
	t.Cleanup(func() { fsRootSymlink = original })
}

func swapRootMkdirAllFailForPath(t testing.TB, failPath string) {
	t.Helper()
	original := fsRootMkdirAll
	fsRootMkdirAll = func(root *os.Root, name string, perm fs.FileMode) error {
		if filepath.ToSlash(name) == failPath {
			return errInjected
		}
		return original(root, name, perm)
	}
	t.Cleanup(func() { fsRootMkdirAll = original })
}

func TestDiscardFailsWhenIndexFileIsCorrupt(t *testing.T) {
	r := newTestRepo(t)
	r.corruptIndexFile()
	if err := Discard(t.Context(), r.repo, []string{"a.txt"}, DiscardOptions{}); err == nil {
		t.Fatalf("expected an error")
	}
}

func TestDiscardContextCanceledMidLoopReturnsError(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "hello\n")
	r.writeFile("b.txt", "world\n")
	mustStage(t, r, "a.txt")
	mustStage(t, r, "b.txt")
	r.commitAll("initial")
	r.writeFile("a.txt", "changed\n")
	r.writeFile("b.txt", "changed\n")
	ctx := newCountingContext(t, 3)
	err := Discard(ctx, r.repo, []string{"a.txt", "b.txt"}, DiscardOptions{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if got := r.readFile("a.txt"); got != "hello\n" {
		t.Fatalf("a.txt = %q, want restored before cancellation", got)
	}
}

func TestDiscardFailsWhenRestoreEntryOpenFileFails(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "hello\n")
	mustStage(t, r, "a.txt")
	r.commitAll("initial")
	r.writeFile("a.txt", "changed\n")
	swapRootOpenFileFailForPath(t, "a.txt")
	if err := Discard(t.Context(), r.repo, []string{"a.txt"}, DiscardOptions{}); !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want errInjected", err)
	}
}

func TestDiscardDirectoryFailsWhenNestedRestoreEntryFails(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("dir/a.txt", "hello\n")
	mustStage(t, r, "dir")
	r.commitAll("initial")
	r.writeFile("dir/a.txt", "changed\n")
	swapRootOpenFileFailForPath(t, "dir/a.txt")
	if err := Discard(t.Context(), r.repo, []string{"dir"}, DiscardOptions{}); !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want errInjected", err)
	}
}

func TestDiscardDirectoryPrefixSkipsConflictedEntries(t *testing.T) {
	r := newTestRepo(t)
	db := r.db()
	blobID, err := db.Put(object.TypeBlob, []byte("a\n"))
	if err != nil {
		t.Fatalf("Put returned error %v", err)
	}
	idx := r.index()
	idx.Add(index.Entry{Path: "dir/a.txt", Mode: object.ModeBlob, ID: blobID, Stage: index.StageMerged})
	idx.Add(index.Entry{Path: "dir/c.txt", Mode: object.ModeBlob, ID: blobID, Stage: index.StageOurs})
	idx.Add(index.Entry{Path: "dir/c.txt", Mode: object.ModeBlob, ID: blobID, Stage: index.StageTheirs})
	r.saveIndex(idx)
	if err := Discard(t.Context(), r.repo, []string{"dir"}, DiscardOptions{}); err != nil {
		t.Fatalf("Discard returned error %v", err)
	}
	if got := r.readFile("dir/a.txt"); got != "a\n" {
		t.Fatalf("dir/a.txt = %q, want %q", got, "a\n")
	}
}

func TestDiscardSubmoduleEntryIsNoop(t *testing.T) {
	r := newTestRepo(t)
	bogus := bogusObjectID(t, r.repo.ObjectFormat)
	idx := r.index()
	idx.Add(index.Entry{Path: "sub", Mode: object.ModeSubmodule, ID: bogus, Stage: index.StageMerged})
	r.saveIndex(idx)
	if err := Discard(t.Context(), r.repo, []string{"sub"}, DiscardOptions{}); err != nil {
		t.Fatalf("Discard returned error %v", err)
	}
	if r.exists("sub") {
		t.Fatalf("submodule path should not have been materialized")
	}
}

func TestDiscardFailsWhenBlobObjectMissing(t *testing.T) {
	r := newTestRepo(t)
	bogus := bogusObjectID(t, r.repo.ObjectFormat)
	idx := r.index()
	idx.Add(index.Entry{Path: "a.txt", Mode: object.ModeBlob, ID: bogus, Stage: index.StageMerged})
	r.saveIndex(idx)
	if err := Discard(t.Context(), r.repo, []string{"a.txt"}, DiscardOptions{}); err == nil {
		t.Fatalf("expected an error")
	}
}

func TestDiscardEntryPointingAtNonBlobIsNoop(t *testing.T) {
	r := newTestRepo(t)
	db := r.db()
	treeID, err := db.Put(object.TypeTree, nil)
	if err != nil {
		t.Fatalf("Put returned error %v", err)
	}
	idx := r.index()
	idx.Add(index.Entry{Path: "a.txt", Mode: object.ModeBlob, ID: treeID, Stage: index.StageMerged})
	r.saveIndex(idx)
	if err := Discard(t.Context(), r.repo, []string{"a.txt"}, DiscardOptions{}); err != nil {
		t.Fatalf("Discard returned error %v", err)
	}
	if r.exists("a.txt") {
		t.Fatalf("a.txt should not have been materialized")
	}
}

func TestDiscardSymlinkModeEntryUsesSymlinkInjection(t *testing.T) {
	r := newTestRepo(t)
	db := r.db()
	blobID, err := db.Put(object.TypeBlob, []byte("target.txt"))
	if err != nil {
		t.Fatalf("Put returned error %v", err)
	}
	idx := r.index()
	idx.Add(index.Entry{Path: "link", Mode: object.ModeSymlink, ID: blobID, Stage: index.StageMerged})
	r.saveIndex(idx)
	swapRootSymlinkSucceeds(t)
	if err := Discard(t.Context(), r.repo, []string{"link"}, DiscardOptions{}); err != nil {
		t.Fatalf("Discard returned error %v", err)
	}
}

func TestDiscardExecutableModeEntryRestoresExecutableFile(t *testing.T) {
	r := newTestRepo(t)
	db := r.db()
	blobID, err := db.Put(object.TypeBlob, []byte("echo hi\n"))
	if err != nil {
		t.Fatalf("Put returned error %v", err)
	}
	idx := r.index()
	idx.Add(index.Entry{Path: "run.sh", Mode: object.ModeExecutable, ID: blobID, Stage: index.StageMerged})
	r.saveIndex(idx)
	if err := Discard(t.Context(), r.repo, []string{"run.sh"}, DiscardOptions{}); err != nil {
		t.Fatalf("Discard returned error %v", err)
	}
	if got := r.readFile("run.sh"); got != "echo hi\n" {
		t.Fatalf("run.sh = %q", got)
	}
}

func TestDiscardFailsWhenMkdirAllFails(t *testing.T) {
	r := newTestRepo(t)
	db := r.db()
	blobID, err := db.Put(object.TypeBlob, []byte("a\n"))
	if err != nil {
		t.Fatalf("Put returned error %v", err)
	}
	idx := r.index()
	idx.Add(index.Entry{Path: "dir/a.txt", Mode: object.ModeBlob, ID: blobID, Stage: index.StageMerged})
	r.saveIndex(idx)
	swapRootMkdirAllFailForPath(t, "dir")
	if err := Discard(t.Context(), r.repo, []string{"dir/a.txt"}, DiscardOptions{}); !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want errInjected", err)
	}
}

func TestDiscardFailsWhenWriteFails(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "hello\n")
	mustStage(t, r, "a.txt")
	r.commitAll("initial")
	r.writeFile("a.txt", "changed\n")
	swapRootOpenFileReturnsClosedFile(t, "a.txt")
	if err := Discard(t.Context(), r.repo, []string{"a.txt"}, DiscardOptions{}); err == nil {
		t.Fatalf("expected an error")
	}
}

func TestDiscardRemoveUntrackedMixedDirectoryPreservesTracked(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("dir/a.txt", "a\n")
	mustStage(t, r, "dir")
	r.commitAll("initial")
	r.writeFile("dir/untracked.txt", "junk\n")
	if err := Discard(t.Context(), r.repo, []string{"dir"}, DiscardOptions{RemoveUntracked: true}); err != nil {
		t.Fatalf("Discard returned error %v", err)
	}
	if r.exists("dir/untracked.txt") {
		t.Fatalf("dir/untracked.txt should have been removed")
	}
	if !r.exists("dir/a.txt") {
		t.Fatalf("dir/a.txt should have been preserved")
	}
}

func TestDiscardRemoveUntrackedIgnoredDirectoryPreserved(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile(".gitignore", "build/\n")
	mustStage(t, r, ".gitignore")
	r.commitAll("initial")
	r.writeFile("build/output.bin", "binary\n")
	if err := Discard(t.Context(), r.repo, []string{"build"}, DiscardOptions{RemoveUntracked: true}); err != nil {
		t.Fatalf("Discard returned error %v", err)
	}
	if !r.exists("build/output.bin") {
		t.Fatalf("ignored directory contents should have been preserved")
	}
}

func TestDiscardRemoveUntrackedFailsWhenLstatFails(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("untracked.txt", "junk\n")
	swapRootLstatFailForPath(t, "untracked.txt")
	err := Discard(t.Context(), r.repo, []string{"untracked.txt"}, DiscardOptions{RemoveUntracked: true})
	if !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want errInjected", err)
	}
}

func TestDiscardRemoveUntrackedFailsWhenFirstReadDirFails(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("dir/untracked.txt", "junk\n")
	swapRootOpenFailForPath(t, "dir")
	err := Discard(t.Context(), r.repo, []string{"dir"}, DiscardOptions{RemoveUntracked: true})
	if !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want errInjected", err)
	}
}

func TestDiscardRemoveUntrackedFailsWhenNestedRemoveFails(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("dir/sub/untracked.txt", "junk\n")
	swapRootOpenFailForPath(t, "dir/sub")
	err := Discard(t.Context(), r.repo, []string{"dir"}, DiscardOptions{RemoveUntracked: true})
	if !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want errInjected", err)
	}
}

func TestDiscardRemoveUntrackedFailsWhenSecondReadDirFails(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("dir/untracked.txt", "junk\n")
	swapRootOpenFailOnNthCallForPath(t, "dir", 2)
	err := Discard(t.Context(), r.repo, []string{"dir"}, DiscardOptions{RemoveUntracked: true})
	if !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want errInjected", err)
	}
}

func TestLockIndexFailsWhenOpenFileFailsForOtherReason(t *testing.T) {
	r := newTestRepo(t)
	swapRootOpenFile(t, func(*os.Root, string, int, fs.FileMode) (*os.File, error) { return nil, errInjected })
	if _, err := lockIndex(r.repo); !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want errInjected", err)
	}
}

func TestLockIndexFailsWhenReadIndexFails(t *testing.T) {
	r := newTestRepo(t)
	r.corruptIndexFile()
	if _, err := lockIndex(r.repo); err == nil {
		t.Fatalf("expected an error")
	}
}

func TestReadIndexFailsWhenIndexFileIsCorrupt(t *testing.T) {
	r := newTestRepo(t)
	r.corruptIndexFile()
	if _, err := readIndex(r.repo); err == nil {
		t.Fatalf("expected an error")
	}
}

func TestIndexLockCommitFailsWhenIdxWriteFails(t *testing.T) {
	r := newTestRepo(t)
	lock, err := lockIndex(r.repo)
	if err != nil {
		t.Fatalf("lockIndex returned error %v", err)
	}
	swapIdxWrite(t, func(*index.Index, io.Writer, int) error { return errInjected })
	if err := lock.commit(); !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want errInjected", err)
	}
}

func TestIndexLockCommitFailsWhenFileCloseFails(t *testing.T) {
	r := newTestRepo(t)
	lock, err := lockIndex(r.repo)
	if err != nil {
		t.Fatalf("lockIndex returned error %v", err)
	}
	swapFileClose(t, func(*os.File) error { return errInjected })
	err = lock.commit()
	if err == nil {
		t.Fatalf("expected an error")
	}
}

func TestReadDirRootFailsForRegularFile(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("plain.txt", "hi\n")
	root, err := fsOpenRoot(r.dir)
	if err != nil {
		t.Fatalf("fsOpenRoot returned error %v", err)
	}
	defer func() { _ = root.Close() }()
	if _, err := readDirRoot(root, "plain.txt"); err == nil {
		t.Fatalf("expected an error")
	}
}

func TestReadDirRootFailsWhenCloseFails(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("dir/a.txt", "hi\n")
	root, err := fsOpenRoot(r.dir)
	if err != nil {
		t.Fatalf("fsOpenRoot returned error %v", err)
	}
	defer func() { _ = root.Close() }()
	swapFileClose(t, func(*os.File) error { return errInjected })
	if _, err := readDirRoot(root, "dir"); !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want errInjected", err)
	}
}

func swapDirentInfoFailForName(t testing.TB, failName string) {
	t.Helper()
	original := direntInfo
	direntInfo = func(entry fs.DirEntry) (fs.FileInfo, error) {
		if entry.Name() == failName {
			return nil, errInjected
		}
		return original(entry)
	}
	t.Cleanup(func() { direntInfo = original })
}

type fakeExecInfo struct{ name string }

func (f fakeExecInfo) Name() string       { return f.name }
func (f fakeExecInfo) Size() int64        { return 0 }
func (f fakeExecInfo) Mode() fs.FileMode  { return 0o755 }
func (f fakeExecInfo) ModTime() time.Time { return time.Time{} }
func (f fakeExecInfo) IsDir() bool        { return false }
func (f fakeExecInfo) Sys() any           { return nil }

func TestStageFailsWhenLstatFailsForOtherReason(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "hello\n")
	swapRootLstatFailForPath(t, "a.txt")
	err := Stage(t.Context(), r.repo, []string{"a.txt"}, StageOptions{})
	if !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want errInjected", err)
	}
}

func TestStageFailsWhenFinalWriteTreeFails(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "a\n")
	mustStage(t, r, "a.txt")
	r.writeFile("dir/b.txt", "b\n")
	mustStage(t, r, "dir/b.txt")
	idx := r.index()
	sub := idx.CacheTree.Find("dir")
	if sub == nil {
		t.Fatalf("dir cache subtree was not built")
	}
	sub.EntryCount = 99
	idx.CacheTree.Invalidate()
	r.saveIndex(idx)
	r.writeFile("a.txt", "a\n")
	err := Stage(t.Context(), r.repo, []string{"a.txt"}, StageOptions{})
	if !errors.Is(err, index.ErrMalformed) {
		t.Fatalf("err = %v, want index.ErrMalformed", err)
	}
}

func TestStageContextCanceledOnEntry(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "hello\n")
	ctx := newCountingContext(t, 2)
	err := Stage(ctx, r.repo, []string{"a.txt"}, StageOptions{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestStageDirFailsWhenReadDirFails(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("dir/a.txt", "hello\n")
	swapRootOpenFailForPath(t, "dir")
	err := Stage(t.Context(), r.repo, []string{"dir"}, StageOptions{})
	if !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want errInjected", err)
	}
}

func TestStageDirContextCanceledMidLoop(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("dir/a.txt", "hello\n")
	ctx := newCountingContext(t, 3)
	err := Stage(ctx, r.repo, []string{"dir"}, StageOptions{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestStageDirFailsWhenNestedStageDirFails(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("dir/sub/a.txt", "hello\n")
	swapRootOpenFailForPath(t, "dir/sub")
	err := Stage(t.Context(), r.repo, []string{"dir"}, StageOptions{})
	if !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want errInjected", err)
	}
}

func TestStageDirSkipsIgnoredChildFile(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile(".gitignore", "*.log\n")
	mustStage(t, r, ".gitignore")
	r.writeFile("dir/keep.txt", "keep\n")
	r.writeFile("dir/skip.log", "skip\n")
	if err := Stage(t.Context(), r.repo, []string{"dir"}, StageOptions{}); err != nil {
		t.Fatalf("Stage returned error %v", err)
	}
	idx := r.index()
	if _, ok := entryOf(t, idx, "dir/skip.log"); ok {
		t.Fatalf("dir/skip.log should not be staged")
	}
	if _, ok := entryOf(t, idx, "dir/keep.txt"); !ok {
		t.Fatalf("dir/keep.txt should be staged")
	}
}

func TestStageDirFailsWhenDirentInfoFails(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("dir/a.txt", "hello\n")
	swapDirentInfoFailForName(t, "a.txt")
	err := Stage(t.Context(), r.repo, []string{"dir"}, StageOptions{})
	if !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want errInjected", err)
	}
}

func TestStageDirFailsWhenChildStageEntryFails(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("dir/a.txt", "hello\n")
	original := fsRootReadFile
	fsRootReadFile = func(root *os.Root, name string) ([]byte, error) {
		if filepath.ToSlash(name) == "dir/a.txt" {
			return nil, errInjected
		}
		return original(root, name)
	}
	t.Cleanup(func() { fsRootReadFile = original })
	err := Stage(t.Context(), r.repo, []string{"dir"}, StageOptions{})
	if !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want errInjected", err)
	}
}

func TestStageSymlinkEntryUsesLstatInjection(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("link", "target.txt")
	swapRootLstatSymlinkForPath(t, "link")
	swapRootReadlinkForPath(t, "link", "target.txt")
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

func TestStageSymlinkEntryFailsWhenReadlinkFails(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("link", "target.txt")
	swapRootLstatSymlinkForPath(t, "link")
	err := Stage(t.Context(), r.repo, []string{"link"}, StageOptions{})
	if err == nil {
		t.Fatalf("expected an error")
	}
}

func TestStageFailsWhenReadFileFails(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "hello\n")
	swapRootReadFile(t, func(*os.Root, string) ([]byte, error) { return nil, errInjected })
	err := Stage(t.Context(), r.repo, []string{"a.txt"}, StageOptions{})
	if !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want errInjected", err)
	}
}

func TestStageExecutableBitViaInjection(t *testing.T) {
	r := newTestRepo(t)
	r.appendConfig("[core]\n\tfilemode = true\n")
	r.repo = r.reopen()
	r.writeFile("run.sh", "echo hi\n")
	original := fsRootLstat
	fsRootLstat = func(root *os.Root, name string) (fs.FileInfo, error) {
		if filepath.ToSlash(name) == "run.sh" {
			return fakeExecInfo{name: "run.sh"}, nil
		}
		return original(root, name)
	}
	t.Cleanup(func() { fsRootLstat = original })
	if err := Stage(t.Context(), r.repo, []string{"run.sh"}, StageOptions{}); err != nil {
		t.Fatalf("Stage returned error %v", err)
	}
	idx := r.index()
	entry, _ := entryOf(t, idx, "run.sh")
	if entry.Mode != object.ModeExecutable {
		t.Fatalf("mode = %s, want executable", entry.Mode)
	}
}

func swapDBPut(t testing.TB, replacement func(*odb.DB, object.Type, []byte) (hash.ObjectID, error)) {
	t.Helper()
	original := dbPut
	dbPut = replacement
	t.Cleanup(func() { dbPut = original })
}

func swapHashSum(t testing.TB, replacement func(hash.Format, string, []byte) (hash.ObjectID, error)) {
	t.Helper()
	original := hashSum
	hashSum = replacement
	t.Cleanup(func() { hashSum = original })
}

func TestDiscardRemoveUntrackedNonexistentPathIsNoop(t *testing.T) {
	r := newTestRepo(t)
	err := Discard(t.Context(), r.repo, []string{"missing.txt"}, DiscardOptions{RemoveUntracked: true})
	if err != nil {
		t.Fatalf("Discard returned error %v", err)
	}
}

func TestStageFailsWhenPutFails(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "hello\n")
	swapDBPut(t, func(*odb.DB, object.Type, []byte) (hash.ObjectID, error) { return hash.Zero, errInjected })
	err := Stage(t.Context(), r.repo, []string{"a.txt"}, StageOptions{})
	if !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want errInjected", err)
	}
}

func putTree(t testing.TB, r *testRepo, entries ...object.TreeEntry) hash.ObjectID {
	t.Helper()
	db := r.db()
	tree := &object.Tree{Entries: entries}
	id, err := db.PutObject(tree)
	if err != nil {
		t.Fatalf("PutObject returned error %v", err)
	}
	return id
}

func putCommit(t testing.TB, r *testRepo, treeID hash.ObjectID, parents ...hash.ObjectID) hash.ObjectID {
	t.Helper()
	db := r.db()
	sig := testSignature()
	commit := &object.Commit{Tree: treeID, Parents: parents, Author: sig, Committer: sig, Message: "x\n"}
	id, err := db.PutObject(commit)
	if err != nil {
		t.Fatalf("PutObject returned error %v", err)
	}
	return id
}

func TestSwitchMissingIdentityReturnsError(t *testing.T) {
	r := newTestRepoNoIdentity(t)
	err := Switch(t.Context(), r.repo, "main", SwitchOptions{})
	if !errors.Is(err, ErrMissingIdentity) {
		t.Fatalf("err = %v, want ErrMissingIdentity", err)
	}
}

func TestSwitchFailsWhenRefsOpenFails(t *testing.T) {
	r := newTestRepo(t)
	swapRefsOpen(t, func(refs.Options) (*refs.Store, error) { return nil, errInjected })
	err := Switch(t.Context(), r.repo, "main", SwitchOptions{})
	if !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want errInjected", err)
	}
}

func TestSwitchFailsWhenTargetCommitObjectIsMalformed(t *testing.T) {
	r := newTestRepo(t)
	db := r.db()
	badID, err := db.Put(object.TypeCommit, []byte("not a valid commit body"))
	if err != nil {
		t.Fatalf("Put returned error %v", err)
	}
	r.createBranch("feature", badID)
	err = Switch(t.Context(), r.repo, "feature", SwitchOptions{})
	if err == nil {
		t.Fatalf("expected an error")
	}
}

func TestSwitchFailsWhenTargetTreeIsMissing(t *testing.T) {
	r := newTestRepo(t)
	bogusTree := bogusObjectID(t, r.repo.ObjectFormat)
	commitID := putCommit(t, r, bogusTree)
	r.createBranch("feature", commitID)
	err := Switch(t.Context(), r.repo, "feature", SwitchOptions{})
	if err == nil {
		t.Fatalf("expected an error")
	}
}

func TestSwitchFailsWhenNestedTreeIsMissing(t *testing.T) {
	r := newTestRepo(t)
	bogusSub := bogusObjectID(t, r.repo.ObjectFormat)
	topID := putTree(t, r, object.TreeEntry{Mode: object.ModeTree, Name: "sub", ID: bogusSub})
	commitID := putCommit(t, r, topID)
	r.createBranch("feature", commitID)
	err := Switch(t.Context(), r.repo, "feature", SwitchOptions{})
	if err == nil {
		t.Fatalf("expected an error")
	}
}

func TestSwitchFailsWhenHeadIsMalformed(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "hello\n")
	mustStage(t, r, "a.txt")
	first := r.commitAll("initial")
	r.createBranch("feature", first)
	r.writeRawHead("not a valid ref\n")
	err := Switch(t.Context(), r.repo, first.String(), SwitchOptions{})
	if err == nil {
		t.Fatalf("expected an error")
	}
}

func TestSwitchFailsWhenCurrentBranchRefIsMalformed(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "hello\n")
	mustStage(t, r, "a.txt")
	first := r.commitAll("initial")
	r.writeRawRef("refs/heads/main", "not a valid ref\n")
	err := Switch(t.Context(), r.repo, first.String(), SwitchOptions{})
	if err == nil {
		t.Fatalf("expected an error")
	}
}

func TestSwitchFailsWhileIndexLocked(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "hello\n")
	mustStage(t, r, "a.txt")
	r.commitAll("initial")
	lock, err := lockIndex(r.repo)
	if err != nil {
		t.Fatalf("lockIndex returned error %v", err)
	}
	defer lock.abort()
	err = Switch(t.Context(), r.repo, "main", SwitchOptions{})
	if !errors.Is(err, ErrIndexLocked) {
		t.Fatalf("err = %v, want ErrIndexLocked", err)
	}
}

func TestSwitchFailsWhenLockCommitFails(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "hello\n")
	mustStage(t, r, "a.txt")
	first := r.commitAll("initial")
	r.createBranch("feature", first)
	swapRootRename(t, func(*os.Root, string, string) error { return errInjected })
	err := Switch(t.Context(), r.repo, "feature", SwitchOptions{})
	if !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want errInjected", err)
	}
}

func TestResolveSwitchTargetFailsWhenPeelFails(t *testing.T) {
	r := newTestRepo(t)
	bogus := bogusObjectID(t, r.repo.ObjectFormat)
	err := Switch(t.Context(), r.repo, bogus.String(), SwitchOptions{})
	if !errors.Is(err, ErrTargetNotFound) {
		t.Fatalf("err = %v, want ErrTargetNotFound", err)
	}
}

func TestSwitchFromDetachedHeadUsesNonSymbolicBranch(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "hello\n")
	mustStage(t, r, "a.txt")
	first := r.commitAll("initial")
	r.createBranch("feature", first)
	if err := Switch(t.Context(), r.repo, first.String(), SwitchOptions{}); err != nil {
		t.Fatalf("Switch returned error %v", err)
	}
	if err := Switch(t.Context(), r.repo, "feature", SwitchOptions{}); err != nil {
		t.Fatalf("Switch returned error %v", err)
	}
}

func TestUpdateHeadAfterSwitchFailsWhenSetSymbolicFails(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "hello\n")
	mustStage(t, r, "a.txt")
	first := r.commitAll("initial")
	r.createBranch("feature", first)
	swapTxSetSymbolic(t, func(*refs.Transaction, refs.Name, refs.Name) error { return errInjected })
	err := Switch(t.Context(), r.repo, "feature", SwitchOptions{})
	if !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want errInjected", err)
	}
}

func TestUpdateHeadAfterSwitchFailsWhenDetachFails(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "hello\n")
	mustStage(t, r, "a.txt")
	first := r.commitAll("initial")
	swapTxDetach(t, func(*refs.Transaction, refs.Name, hash.ObjectID) error { return errInjected })
	err := Switch(t.Context(), r.repo, first.String(), SwitchOptions{})
	if !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want errInjected", err)
	}
}

func TestComputeOverwritesFailsWhenLocalOnlyPathIsDirtyCheckErrors(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("cur.txt", "hello\n")
	mustStage(t, r, "cur.txt")
	first := r.commitAll("initial")
	r.createBranch("feature", first)
	r.remove("cur.txt")
	mustStage(t, r, "cur.txt")
	r.commitAll("removed on feature branch stand-in")
	if err := Switch(t.Context(), r.repo, "main", SwitchOptions{}); err != nil {
		t.Fatalf("Switch returned error %v", err)
	}
	r.writeFile("cur.txt", "back again\n")
	swapRootLstatFailForPath(t, "cur.txt")
	err := Switch(t.Context(), r.repo, "feature", SwitchOptions{})
	if !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want errInjected", err)
	}
}

func TestComputeOverwritesFailsWhenTargetOnlyPathIsDirtyCheckErrors(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "hello\n")
	mustStage(t, r, "a.txt")
	first := r.commitAll("initial")
	r.createBranch("feature", first)
	r.writeFile("new.txt", "on feature\n")
	mustStage(t, r, "new.txt")
	r.commitAll("on feature")
	swapRootLstatFailForPath(t, "new.txt")
	err := Switch(t.Context(), r.repo, "feature", SwitchOptions{})
	if !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want errInjected", err)
	}
}

func TestIsDirtySubmoduleEntryIsNotDirty(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "hello\n")
	mustStage(t, r, "a.txt")
	first := r.commitAll("initial")
	r.createBranch("feature", first)
	bogus := bogusObjectID(t, r.repo.ObjectFormat)
	idx := r.index()
	idx.Add(index.Entry{Path: "sub", Mode: object.ModeSubmodule, ID: bogus, Stage: index.StageMerged})
	r.saveIndex(idx)
	if err := os.MkdirAll(r.path("sub"), 0o777); err != nil {
		t.Fatalf("MkdirAll returned error %v", err)
	}
	if err := Switch(t.Context(), r.repo, "feature", SwitchOptions{}); err != nil {
		t.Fatalf("Switch returned error %v", err)
	}
}

func TestIsDirtyFailsWhenReadWorktreeBytesFails(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "hello\n")
	mustStage(t, r, "a.txt")
	first := r.commitAll("initial")
	r.createBranch("feature", first)
	r.writeFile("b.txt", "world\n")
	mustStage(t, r, "b.txt")
	r.commitAll("on feature")
	if err := Switch(t.Context(), r.repo, "main", SwitchOptions{}); err != nil {
		t.Fatalf("Switch returned error %v", err)
	}
	r.writeFile("a.txt", "changed\n")
	swapRootReadFile(t, func(*os.Root, string) ([]byte, error) { return nil, errInjected })
	err := Switch(t.Context(), r.repo, "feature", SwitchOptions{})
	if !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want errInjected", err)
	}
}

func TestIsDirtyFailsWhenHashSumFails(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "hello\n")
	mustStage(t, r, "a.txt")
	first := r.commitAll("initial")
	r.createBranch("feature", first)
	r.writeFile("b.txt", "world\n")
	mustStage(t, r, "b.txt")
	r.commitAll("on feature")
	if err := Switch(t.Context(), r.repo, "main", SwitchOptions{}); err != nil {
		t.Fatalf("Switch returned error %v", err)
	}
	r.writeFile("a.txt", "changed\n")
	swapHashSum(t, func(hash.Format, string, []byte) (hash.ObjectID, error) { return hash.Zero, errInjected })
	err := Switch(t.Context(), r.repo, "feature", SwitchOptions{})
	if !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want errInjected", err)
	}
}

func TestIsDirtyDetectsExecutableBitMismatchViaInjection(t *testing.T) {
	r := newTestRepo(t)
	r.appendConfig("[core]\n\tfilemode = true\n")
	r.repo = r.reopen()
	r.writeFile("run.sh", "echo hi\n")
	mustStage(t, r, "run.sh")
	r.commitAll("initial")
	emptyTreeID := putTree(t, r)
	emptyCommitID := putCommit(t, r, emptyTreeID)
	r.createBranch("feature", emptyCommitID)
	original := fsRootLstat
	fsRootLstat = func(root *os.Root, name string) (fs.FileInfo, error) {
		if filepath.ToSlash(name) == "run.sh" {
			return fakeExecInfo{name: "run.sh"}, nil
		}
		return original(root, name)
	}
	t.Cleanup(func() { fsRootLstat = original })
	err := Switch(t.Context(), r.repo, "feature", SwitchOptions{})
	var overwrite *OverwriteError
	if !errors.As(err, &overwrite) {
		t.Fatalf("err = %v, want *OverwriteError", err)
	}
}

func TestIsDirtySymlinkEntryUsesInjection(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "hello\n")
	mustStage(t, r, "a.txt")
	r.commitAll("initial")
	db := r.db()
	oldTarget, err := db.Put(object.TypeBlob, []byte("old-target"))
	if err != nil {
		t.Fatalf("Put returned error %v", err)
	}
	idx := r.index()
	idx.Add(index.Entry{Path: "link", Mode: object.ModeSymlink, ID: oldTarget, Stage: index.StageMerged})
	r.saveIndex(idx)
	swapRootLstatSymlinkForPath(t, "link")
	swapRootReadlinkForPath(t, "link", "new-target")
	err = Switch(t.Context(), r.repo, "main", SwitchOptions{})
	var overwrite *OverwriteError
	if !errors.As(err, &overwrite) {
		t.Fatalf("err = %v, want *OverwriteError", err)
	}
}

func TestIsDirtySymlinkEntryFailsWhenReadlinkFails(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "hello\n")
	mustStage(t, r, "a.txt")
	r.commitAll("initial")
	db := r.db()
	oldTarget, err := db.Put(object.TypeBlob, []byte("old-target"))
	if err != nil {
		t.Fatalf("Put returned error %v", err)
	}
	idx := r.index()
	idx.Add(index.Entry{Path: "link", Mode: object.ModeSymlink, ID: oldTarget, Stage: index.StageMerged})
	r.saveIndex(idx)
	swapRootLstatSymlinkForPath(t, "link")
	err = Switch(t.Context(), r.repo, "main", SwitchOptions{})
	if err == nil {
		t.Fatalf("expected an error")
	}
}

func TestApplyFailsWhenRemoveFailsForOtherReason(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "hello\n")
	mustStage(t, r, "a.txt")
	first := r.commitAll("initial")
	r.createBranch("feature", first)
	r.remove("a.txt")
	mustStage(t, r, "a.txt")
	r.commitAll("removed")
	if err := Switch(t.Context(), r.repo, "feature", SwitchOptions{}); err != nil {
		t.Fatalf("Switch returned error %v", err)
	}
	r.writeFile("a.txt", "hello\n")
	mustStage(t, r, "a.txt")
	r.commitAll("re-added on feature")
	swapRootRemove(t, func(*os.Root, string) error { return errInjected })
	err := Switch(t.Context(), r.repo, "main", SwitchOptions{Force: true})
	if !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want errInjected", err)
	}
}

func TestCheckoutFailsWhenMkdirAllFails(t *testing.T) {
	r := newTestRepo(t)
	blobID := func() hash.ObjectID {
		db := r.db()
		id, err := db.Put(object.TypeBlob, []byte("hi\n"))
		if err != nil {
			t.Fatalf("Put returned error %v", err)
		}
		return id
	}()
	treeID := putTree(t, r, object.TreeEntry{Mode: object.ModeBlob, Name: "a.txt", ID: blobID})
	commitID := putCommit(t, r, treeID)
	r.createBranch("feature", commitID)
	nestedTreeID := putTree(t, r, object.TreeEntry{Mode: object.ModeTree, Name: "dir", ID: treeID})
	nestedCommitID := putCommit(t, r, nestedTreeID, commitID)
	r.createBranch("feature2", nestedCommitID)
	if err := Switch(t.Context(), r.repo, "feature", SwitchOptions{}); err != nil {
		t.Fatalf("Switch returned error %v", err)
	}
	swapRootMkdirAllFailForPath(t, "dir")
	err := Switch(t.Context(), r.repo, "feature2", SwitchOptions{})
	if !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want errInjected", err)
	}
}

func TestCheckoutSubmoduleEntryIsNoop(t *testing.T) {
	r := newTestRepo(t)
	bogus := bogusObjectID(t, r.repo.ObjectFormat)
	treeID := putTree(t, r, object.TreeEntry{Mode: object.ModeSubmodule, Name: "sub", ID: bogus})
	commitID := putCommit(t, r, treeID)
	r.createBranch("feature", commitID)
	if err := Switch(t.Context(), r.repo, "feature", SwitchOptions{}); err != nil {
		t.Fatalf("Switch returned error %v", err)
	}
	if r.exists("sub") {
		t.Fatalf("submodule path should not have been materialized")
	}
}

func TestCheckoutFailsWhenBlobObjectMissing(t *testing.T) {
	r := newTestRepo(t)
	bogus := bogusObjectID(t, r.repo.ObjectFormat)
	treeID := putTree(t, r, object.TreeEntry{Mode: object.ModeBlob, Name: "a.txt", ID: bogus})
	commitID := putCommit(t, r, treeID)
	r.createBranch("feature", commitID)
	err := Switch(t.Context(), r.repo, "feature", SwitchOptions{})
	if err == nil {
		t.Fatalf("expected an error")
	}
}

func TestCheckoutEntryPointingAtNonBlobIsNoop(t *testing.T) {
	r := newTestRepo(t)
	db := r.db()
	innerTreeID, err := db.Put(object.TypeTree, nil)
	if err != nil {
		t.Fatalf("Put returned error %v", err)
	}
	treeID := putTree(t, r, object.TreeEntry{Mode: object.ModeBlob, Name: "a.txt", ID: innerTreeID})
	commitID := putCommit(t, r, treeID)
	r.createBranch("feature", commitID)
	if err := Switch(t.Context(), r.repo, "feature", SwitchOptions{}); err != nil {
		t.Fatalf("Switch returned error %v", err)
	}
	if r.exists("a.txt") {
		t.Fatalf("a.txt should not have been materialized")
	}
}

func TestCheckoutSymlinkEntryUsesSymlinkInjection(t *testing.T) {
	r := newTestRepo(t)
	db := r.db()
	blobID, err := db.Put(object.TypeBlob, []byte("target.txt"))
	if err != nil {
		t.Fatalf("Put returned error %v", err)
	}
	treeID := putTree(t, r, object.TreeEntry{Mode: object.ModeSymlink, Name: "link", ID: blobID})
	commitID := putCommit(t, r, treeID)
	r.createBranch("feature", commitID)
	swapRootSymlinkSucceeds(t)
	if err := Switch(t.Context(), r.repo, "feature", SwitchOptions{}); err != nil {
		t.Fatalf("Switch returned error %v", err)
	}
}

func TestCheckoutExecutableEntryRestoresExecutableFile(t *testing.T) {
	r := newTestRepo(t)
	db := r.db()
	blobID, err := db.Put(object.TypeBlob, []byte("echo hi\n"))
	if err != nil {
		t.Fatalf("Put returned error %v", err)
	}
	treeID := putTree(t, r, object.TreeEntry{Mode: object.ModeExecutable, Name: "run.sh", ID: blobID})
	commitID := putCommit(t, r, treeID)
	r.createBranch("feature", commitID)
	if err := Switch(t.Context(), r.repo, "feature", SwitchOptions{}); err != nil {
		t.Fatalf("Switch returned error %v", err)
	}
	if got := r.readFile("run.sh"); got != "echo hi\n" {
		t.Fatalf("run.sh = %q", got)
	}
}

func TestCheckoutFailsWhenOpenFileFails(t *testing.T) {
	r := newTestRepo(t)
	db := r.db()
	blobID, err := db.Put(object.TypeBlob, []byte("hi\n"))
	if err != nil {
		t.Fatalf("Put returned error %v", err)
	}
	treeID := putTree(t, r, object.TreeEntry{Mode: object.ModeBlob, Name: "a.txt", ID: blobID})
	commitID := putCommit(t, r, treeID)
	r.createBranch("feature", commitID)
	swapRootOpenFileFailForPath(t, "a.txt")
	err = Switch(t.Context(), r.repo, "feature", SwitchOptions{})
	if !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want errInjected", err)
	}
}

func TestCheckoutFailsWhenWriteFails(t *testing.T) {
	r := newTestRepo(t)
	db := r.db()
	blobID, err := db.Put(object.TypeBlob, []byte("hi\n"))
	if err != nil {
		t.Fatalf("Put returned error %v", err)
	}
	treeID := putTree(t, r, object.TreeEntry{Mode: object.ModeBlob, Name: "a.txt", ID: blobID})
	commitID := putCommit(t, r, treeID)
	r.createBranch("feature", commitID)
	swapRootOpenFileReturnsClosedFile(t, "a.txt")
	err = Switch(t.Context(), r.repo, "feature", SwitchOptions{})
	if err == nil {
		t.Fatalf("expected an error")
	}
}

func TestSwitchDoesNotPruneNonEmptyDirectory(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("dir/a.txt", "a\n")
	r.writeFile("dir/b.txt", "b\n")
	mustStage(t, r, "dir")
	r.commitAll("initial")
	db := r.db()
	bID, err := db.Put(object.TypeBlob, []byte("b\n"))
	if err != nil {
		t.Fatalf("Put returned error %v", err)
	}
	featureTreeID := putTree(t, r,
		object.TreeEntry{Mode: object.ModeTree, Name: "dir", ID: putTree(t, r, object.TreeEntry{Mode: object.ModeBlob, Name: "b.txt", ID: bID})},
	)
	featureCommitID := putCommit(t, r, featureTreeID)
	r.createBranch("feature", featureCommitID)
	if err := Switch(t.Context(), r.repo, "feature", SwitchOptions{}); err != nil {
		t.Fatalf("Switch returned error %v", err)
	}
	if !r.exists("dir") || !r.exists("dir/b.txt") {
		t.Fatalf("dir/b.txt should still exist, dir should not have been pruned")
	}
	if r.exists("dir/a.txt") {
		t.Fatalf("dir/a.txt should have been removed")
	}
}

func TestPruneEmptyDirsFailsWhenRemoveFails(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("dir/a.txt", "a\n")
	mustStage(t, r, "dir")
	r.commitAll("initial")
	emptyTreeID := putTree(t, r)
	emptyCommitID := putCommit(t, r, emptyTreeID)
	r.createBranch("feature", emptyCommitID)
	originalRemove := fsRootRemove
	swapRootRemove(t, func(root *os.Root, name string) error {
		if filepath.ToSlash(name) == "dir" {
			return errInjected
		}
		return originalRemove(root, name)
	})
	if err := Switch(t.Context(), r.repo, "feature", SwitchOptions{}); err != nil {
		t.Fatalf("Switch returned error %v", err)
	}
	if r.exists("dir/a.txt") {
		t.Fatalf("dir/a.txt should have been removed")
	}
}

func swapDBCommit(t testing.TB, replacement func(*odb.DB, hash.ObjectID) (*object.Commit, error)) {
	t.Helper()
	original := dbCommit
	dbCommit = replacement
	t.Cleanup(func() { dbCommit = original })
}

func swapDBPeel(t testing.TB, replacement func(*odb.DB, hash.ObjectID) (object.Type, hash.ObjectID, error)) {
	t.Helper()
	original := dbPeel
	dbPeel = replacement
	t.Cleanup(func() { dbPeel = original })
}

func TestSwitchFailsWhenCommitLookupFails(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "hello\n")
	mustStage(t, r, "a.txt")
	r.commitAll("initial")
	swapDBCommit(t, func(*odb.DB, hash.ObjectID) (*object.Commit, error) { return nil, errInjected })
	err := Switch(t.Context(), r.repo, "main", SwitchOptions{})
	if !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want errInjected", err)
	}
}

func TestResolveSwitchTargetFailsWhenPeelCallFails(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "hello\n")
	mustStage(t, r, "a.txt")
	r.commitAll("initial")
	swapDBPeel(t, func(*odb.DB, hash.ObjectID) (object.Type, hash.ObjectID, error) {
		return 0, hash.Zero, errInjected
	})
	err := Switch(t.Context(), r.repo, "main", SwitchOptions{})
	if !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want errInjected", err)
	}
}

func TestCurrentHeadStateReturnsZeroWhenHeadFileMissing(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "hello\n")
	mustStage(t, r, "a.txt")
	first := r.commitAll("initial")
	if err := r.repo.Root().Remove("HEAD"); err != nil {
		t.Fatalf("Remove returned error %v", err)
	}
	if err := Switch(t.Context(), r.repo, first.String(), SwitchOptions{}); err != nil {
		t.Fatalf("Switch returned error %v", err)
	}
}

func TestSwitchUntrackedFileWithoutIndexEntryConflicts(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "hello\n")
	mustStage(t, r, "a.txt")
	db := r.db()
	aID, err := db.Put(object.TypeBlob, []byte("hello\n"))
	if err != nil {
		t.Fatalf("Put returned error %v", err)
	}
	r.commitAll("initial")
	newID, err := db.Put(object.TypeBlob, []byte("on feature\n"))
	if err != nil {
		t.Fatalf("Put returned error %v", err)
	}
	featureTreeID := putTree(t, r,
		object.TreeEntry{Mode: object.ModeBlob, Name: "a.txt", ID: aID},
		object.TreeEntry{Mode: object.ModeBlob, Name: "new.txt", ID: newID},
	)
	featureCommitID := putCommit(t, r, featureTreeID)
	r.createBranch("feature", featureCommitID)

	r.writeFile("new.txt", "untracked collision\n")
	err = Switch(t.Context(), r.repo, "feature", SwitchOptions{})
	var overwrite *OverwriteError
	if !errors.As(err, &overwrite) {
		t.Fatalf("err = %v, want *OverwriteError", err)
	}
	if len(overwrite.Paths) != 1 || overwrite.Paths[0] != "new.txt" {
		t.Fatalf("paths = %v", overwrite.Paths)
	}
}

func TestResolveHeadCommitFailsWhenHeadIsMalformed(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "hello\n")
	mustStage(t, r, "a.txt")
	r.commitAll("initial")
	r.writeRawHead("not a valid ref\n")
	err := Unstage(t.Context(), r.repo, []string{"a.txt"})
	if err == nil {
		t.Fatalf("expected an error")
	}
}

func TestCommitTreeEntriesFailsWhenCommitObjectMissing(t *testing.T) {
	r := newTestRepo(t)
	bogus := bogusObjectID(t, r.repo.ObjectFormat)
	r.createBranch("main", bogus)
	err := Unstage(t.Context(), r.repo, []string{"a.txt"})
	if err == nil {
		t.Fatalf("expected an error")
	}
}

func TestCommitTreeEntriesFailsWhenTreeObjectMissing(t *testing.T) {
	r := newTestRepo(t)
	bogusTree := bogusObjectID(t, r.repo.ObjectFormat)
	commitID := putCommit(t, r, bogusTree)
	r.createBranch("main", commitID)
	err := Unstage(t.Context(), r.repo, []string{"a.txt"})
	if err == nil {
		t.Fatalf("expected an error")
	}
}

func TestCommitTreeEntriesReturnsEmptyForZeroCommit(t *testing.T) {
	entries, err := commitTreeEntries(nil, hash.Zero)
	if err != nil {
		t.Fatalf("commitTreeEntries returned error %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("entries = %v, want empty", entries)
	}
}

func TestUnstageFailsWhenRefsOpenFails(t *testing.T) {
	r := newTestRepo(t)
	swapRefsOpen(t, func(refs.Options) (*refs.Store, error) { return nil, errInjected })
	err := Unstage(t.Context(), r.repo, []string{"a.txt"})
	if !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want errInjected", err)
	}
}

func TestUnstageContextCanceledMidLoop(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "hello\n")
	mustStage(t, r, "a.txt")
	r.commitAll("initial")
	ctx := newCountingContext(t, 2)
	err := Unstage(ctx, r.repo, []string{"a.txt", "b.txt"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestUnstageDirectorySkipsEntriesOutsidePrefix(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("outside.txt", "o\n")
	r.writeFile("dir/a.txt", "a\n")
	r.writeFile("dir/b.txt", "b\n")
	mustStage(t, r, "outside.txt")
	mustStage(t, r, "dir")
	r.commitAll("initial")
	r.writeFile("dir/a.txt", "changed\n")
	mustStage(t, r, "dir")
	if err := Unstage(t.Context(), r.repo, []string{"dir"}); err != nil {
		t.Fatalf("Unstage returned error %v", err)
	}
	idx := r.index()
	if _, ok := entryOf(t, idx, "dir/b.txt"); !ok {
		t.Fatalf("dir/b.txt should remain tracked")
	}
	if _, ok := entryOf(t, idx, "outside.txt"); !ok {
		t.Fatalf("outside.txt should remain tracked")
	}
}

func TestCollectTreeReturnsNilForZeroTree(t *testing.T) {
	r := newTestRepo(t)
	commitID := putCommit(t, r, hash.Zero)
	r.createBranch("main", commitID)
	if err := Unstage(t.Context(), r.repo, []string{"a.txt"}); err != nil {
		t.Fatalf("Unstage returned error %v", err)
	}
}
