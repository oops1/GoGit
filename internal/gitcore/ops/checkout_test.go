package ops

import (
	"context"
	"errors"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/progress"
	"github.com/oops1/gogit/internal/gitcore/refs"
)

func TestCheckoutTreeWritesFilesFromCommit(t *testing.T) {
	r := newTestRepo(t)
	db := r.db()
	blobID, err := db.Put(object.TypeBlob, []byte("hello\n"))
	if err != nil {
		t.Fatalf("Put returned error %v", err)
	}
	treeID := putTree(t, r, object.TreeEntry{Mode: object.ModeBlob, Name: "a.txt", ID: blobID})
	commitID := putCommit(t, r, treeID)

	if err := CheckoutTree(t.Context(), r.repo, commitID, CheckoutOptions{}); err != nil {
		t.Fatalf("CheckoutTree returned error %v", err)
	}
	if got := r.readFile("a.txt"); got != "hello\n" {
		t.Fatalf("a.txt = %q", got)
	}
	idx := r.index()
	entry, ok := entryOf(t, idx, "a.txt")
	if !ok {
		t.Fatalf("a.txt was not added to the index")
	}
	if entry.ID != blobID || entry.Mode != object.ModeBlob {
		t.Fatalf("index entry = %+v", entry)
	}
}

func TestCheckoutTreeCreatesNestedDirectories(t *testing.T) {
	r := newTestRepo(t)
	db := r.db()
	blobID, err := db.Put(object.TypeBlob, []byte("nested\n"))
	if err != nil {
		t.Fatalf("Put returned error %v", err)
	}
	innerID := putTree(t, r, object.TreeEntry{Mode: object.ModeBlob, Name: "c.txt", ID: blobID})
	outerID := putTree(t, r, object.TreeEntry{Mode: object.ModeTree, Name: "b", ID: innerID})
	commitID := putCommit(t, r, outerID)

	if err := CheckoutTree(t.Context(), r.repo, commitID, CheckoutOptions{}); err != nil {
		t.Fatalf("CheckoutTree returned error %v", err)
	}
	if got := r.readFile("b/c.txt"); got != "nested\n" {
		t.Fatalf("b/c.txt = %q", got)
	}
}

func TestCheckoutTreeRestoresExecutableFile(t *testing.T) {
	r := newTestRepo(t)
	db := r.db()
	blobID, err := db.Put(object.TypeBlob, []byte("echo hi\n"))
	if err != nil {
		t.Fatalf("Put returned error %v", err)
	}
	treeID := putTree(t, r, object.TreeEntry{Mode: object.ModeExecutable, Name: "run.sh", ID: blobID})
	commitID := putCommit(t, r, treeID)

	if err := CheckoutTree(t.Context(), r.repo, commitID, CheckoutOptions{}); err != nil {
		t.Fatalf("CheckoutTree returned error %v", err)
	}
	if got := r.readFile("run.sh"); got != "echo hi\n" {
		t.Fatalf("run.sh = %q", got)
	}
}

func TestCheckoutTreeWritesSymlink(t *testing.T) {
	r := newTestRepo(t)
	db := r.db()
	blobID, err := db.Put(object.TypeBlob, []byte("target.txt"))
	if err != nil {
		t.Fatalf("Put returned error %v", err)
	}
	treeID := putTree(t, r, object.TreeEntry{Mode: object.ModeSymlink, Name: "link", ID: blobID})
	commitID := putCommit(t, r, treeID)
	swapRootSymlinkSucceeds(t)

	if err := CheckoutTree(t.Context(), r.repo, commitID, CheckoutOptions{}); err != nil {
		t.Fatalf("CheckoutTree returned error %v", err)
	}
	idx := r.index()
	entry, ok := entryOf(t, idx, "link")
	if !ok || entry.Mode != object.ModeSymlink {
		t.Fatalf("link entry = %+v ok=%v", entry, ok)
	}
}

func TestCheckoutTreeConvertsLineEndingsOnCheckout(t *testing.T) {
	r := newTestRepo(t)
	r.appendConfig("[core]\n\tautocrlf = true\n")
	r.repo = r.reopen()
	db := r.db()
	blobID, err := db.Put(object.TypeBlob, []byte("one\ntwo\n"))
	if err != nil {
		t.Fatalf("Put returned error %v", err)
	}
	treeID := putTree(t, r, object.TreeEntry{Mode: object.ModeBlob, Name: "a.txt", ID: blobID})
	commitID := putCommit(t, r, treeID)

	if err := CheckoutTree(t.Context(), r.repo, commitID, CheckoutOptions{}); err != nil {
		t.Fatalf("CheckoutTree returned error %v", err)
	}
	if got := r.readFile("a.txt"); got != "one\r\ntwo\r\n" {
		t.Fatalf("a.txt = %q, want CRLF line endings", got)
	}
}

func TestCheckoutTreeRefusesPathEscapingWorkingTree(t *testing.T) {
	r := newTestRepo(t)
	db := r.db()
	blobID, err := db.Put(object.TypeBlob, []byte("evil\n"))
	if err != nil {
		t.Fatalf("Put returned error %v", err)
	}
	treeID := putTree(t, r, object.TreeEntry{Mode: object.ModeBlob, Name: "..", ID: blobID})
	commitID := putCommit(t, r, treeID)

	err = CheckoutTree(t.Context(), r.repo, commitID, CheckoutOptions{})
	if err == nil {
		t.Fatalf("expected an error when a tree entry tries to escape the working tree")
	}
	if r.exists("../evil") {
		t.Fatalf("checkout escaped the working tree")
	}
}

func TestCheckoutTreeRefusesOnLocalChangesWithoutForce(t *testing.T) {
	r := newTestRepo(t)
	db := r.db()
	helloID, err := db.Put(object.TypeBlob, []byte("hello\n"))
	if err != nil {
		t.Fatalf("Put returned error %v", err)
	}
	first := putCommit(t, r, putTree(t, r, object.TreeEntry{Mode: object.ModeBlob, Name: "a.txt", ID: helloID}))
	if err := CheckoutTree(t.Context(), r.repo, first, CheckoutOptions{}); err != nil {
		t.Fatalf("CheckoutTree returned error %v", err)
	}
	r.writeFile("a.txt", "dirty\n")

	changedID, err := db.Put(object.TypeBlob, []byte("feature change\n"))
	if err != nil {
		t.Fatalf("Put returned error %v", err)
	}
	second := putCommit(t, r, putTree(t, r, object.TreeEntry{Mode: object.ModeBlob, Name: "a.txt", ID: changedID}))

	err = CheckoutTree(t.Context(), r.repo, second, CheckoutOptions{})
	var overwrite *OverwriteError
	if !errors.As(err, &overwrite) {
		t.Fatalf("err = %v, want *OverwriteError", err)
	}
	if got := r.readFile("a.txt"); got != "dirty\n" {
		t.Fatalf("a.txt was modified: %q", got)
	}
}

func TestCheckoutTreeForceOverwritesLocalChanges(t *testing.T) {
	r := newTestRepo(t)
	db := r.db()
	helloID, err := db.Put(object.TypeBlob, []byte("hello\n"))
	if err != nil {
		t.Fatalf("Put returned error %v", err)
	}
	first := putCommit(t, r, putTree(t, r, object.TreeEntry{Mode: object.ModeBlob, Name: "a.txt", ID: helloID}))
	if err := CheckoutTree(t.Context(), r.repo, first, CheckoutOptions{}); err != nil {
		t.Fatalf("CheckoutTree returned error %v", err)
	}
	r.writeFile("a.txt", "dirty\n")

	changedID, err := db.Put(object.TypeBlob, []byte("feature change\n"))
	if err != nil {
		t.Fatalf("Put returned error %v", err)
	}
	second := putCommit(t, r, putTree(t, r, object.TreeEntry{Mode: object.ModeBlob, Name: "a.txt", ID: changedID}))

	if err := CheckoutTree(t.Context(), r.repo, second, CheckoutOptions{Force: true}); err != nil {
		t.Fatalf("CheckoutTree returned error %v", err)
	}
	if got := r.readFile("a.txt"); got != "feature change\n" {
		t.Fatalf("a.txt = %q, want feature change", got)
	}
}

func TestCheckoutTreeDoesNotChangeHEAD(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "hello\n")
	mustStage(t, r, "a.txt")
	first := r.commitAll("initial")
	r.createBranch("feature", first)
	db := r.db()
	blobID, err := db.Put(object.TypeBlob, []byte("world\n"))
	if err != nil {
		t.Fatalf("Put returned error %v", err)
	}
	treeID := putTree(t, r, object.TreeEntry{Mode: object.ModeBlob, Name: "b.txt", ID: blobID})
	commitID := putCommit(t, r, treeID)
	r.createBranch("other", commitID)

	if err := CheckoutTree(t.Context(), r.repo, commitID, CheckoutOptions{Force: true}); err != nil {
		t.Fatalf("CheckoutTree returned error %v", err)
	}
	target, symbolic := r.headSymbolicTarget()
	if !symbolic || target != refs.BranchName("main") {
		t.Fatalf("HEAD = %s symbolic=%v, want unchanged main", target, symbolic)
	}
	if !r.exists("b.txt") {
		t.Fatalf("b.txt should have been written to the working tree")
	}
}

func TestCheckoutTreeOnBareRepositoryReturnsError(t *testing.T) {
	r := newBareTestRepo(t)
	err := CheckoutTree(t.Context(), r.repo, hash.Zero, CheckoutOptions{})
	if !errors.Is(err, ErrBareRepository) {
		t.Fatalf("err = %v, want ErrBareRepository", err)
	}
}

func TestCheckoutTreeContextCanceledReturnsError(t *testing.T) {
	r := newTestRepo(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err := CheckoutTree(ctx, r.repo, hash.Zero, CheckoutOptions{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func setupCheckoutCancellationFixture(t *testing.T) (*testRepo, hash.ObjectID) {
	t.Helper()
	r := newTestRepo(t)
	db := r.db()
	oldID, err := db.Put(object.TypeBlob, []byte("old\n"))
	if err != nil {
		t.Fatalf("Put returned error %v", err)
	}
	first := putCommit(t, r, putTree(t, r, object.TreeEntry{Mode: object.ModeBlob, Name: "old.txt", ID: oldID}))
	if err := CheckoutTree(t.Context(), r.repo, first, CheckoutOptions{}); err != nil {
		t.Fatalf("CheckoutTree returned error %v", err)
	}

	newID, err := db.Put(object.TypeBlob, []byte("new\n"))
	if err != nil {
		t.Fatalf("Put returned error %v", err)
	}
	second := putCommit(t, r, putTree(t, r,
		object.TreeEntry{Mode: object.ModeBlob, Name: "new1.txt", ID: newID},
		object.TreeEntry{Mode: object.ModeBlob, Name: "new2.txt", ID: newID},
	))
	return r, second
}

func TestCheckoutTreeContextCanceledDuringOverwriteScanChangesNothing(t *testing.T) {
	r, second := setupCheckoutCancellationFixture(t)

	ctx := newCountingContext(t, 2)
	err := CheckoutTree(ctx, r.repo, second, CheckoutOptions{Force: true})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if !r.exists("old.txt") {
		t.Fatalf("old.txt was removed before the overwrite scan even finished")
	}
	if r.exists("new1.txt") || r.exists("new2.txt") {
		t.Fatalf("checkout wrote target files after cancellation")
	}
}

func TestCheckoutTreeContextCanceledBeforeRemovingStaleFileKeepsIt(t *testing.T) {
	r, second := setupCheckoutCancellationFixture(t)

	ctx := newCountingContext(t, 5)
	err := CheckoutTree(ctx, r.repo, second, CheckoutOptions{Force: true})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if !r.exists("old.txt") {
		t.Fatalf("old.txt was removed despite cancellation before the removal")
	}
	if r.exists("new1.txt") || r.exists("new2.txt") {
		t.Fatalf("checkout wrote target files after cancellation")
	}
}

func TestCheckoutTreeContextCanceledMidApplyLeavesIndexLockFree(t *testing.T) {
	r, second := setupCheckoutCancellationFixture(t)

	ctx := newCountingContext(t, 6)
	err := CheckoutTree(ctx, r.repo, second, CheckoutOptions{Force: true})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if r.exists("new1.txt") || r.exists("new2.txt") {
		t.Fatalf("checkout wrote target files after cancellation")
	}

	if err := CheckoutTree(t.Context(), r.repo, second, CheckoutOptions{Force: true}); err != nil {
		t.Fatalf("CheckoutTree after cancellation returned error %v, want the index lock to be free", err)
	}
	if got := r.readFile("new1.txt"); got != "new\n" {
		t.Fatalf("new1.txt = %q, want %q", got, "new\n")
	}
}

func TestCheckoutTreeFailsWhenObjectDatabaseIsUnreadable(t *testing.T) {
	r := newTestRepo(t)
	r.breakObjectsDir()
	err := CheckoutTree(t.Context(), r.repo, hash.Zero, CheckoutOptions{})
	if err == nil {
		t.Fatalf("expected an error")
	}
}

func TestCheckoutTreeFailsWhenCommitIsMissing(t *testing.T) {
	r := newTestRepo(t)
	missing := bogusObjectID(t, r.repo.ObjectFormat)
	err := CheckoutTree(t.Context(), r.repo, missing, CheckoutOptions{})
	if err == nil {
		t.Fatalf("expected an error")
	}
}

func TestCheckoutTreeReportsCheckoutPhase(t *testing.T) {
	r := newTestRepo(t)
	db := r.db()
	blobID, err := db.Put(object.TypeBlob, []byte("hello\n"))
	if err != nil {
		t.Fatalf("Put returned error %v", err)
	}
	treeID := putTree(t, r, object.TreeEntry{Mode: object.ModeBlob, Name: "a.txt", ID: blobID})
	commitID := putCommit(t, r, treeID)

	var reports []progress.Report
	prog := progress.Func(func(report progress.Report) { reports = append(reports, report) })
	if err := CheckoutTree(t.Context(), r.repo, commitID, CheckoutOptions{Progress: prog}); err != nil {
		t.Fatalf("CheckoutTree returned error %v", err)
	}
	found := false
	for _, report := range reports {
		if report.Phase == progress.PhaseCheckout {
			found = true
		}
	}
	if !found {
		t.Fatalf("reports = %v, want a %s phase", reports, progress.PhaseCheckout)
	}
}
