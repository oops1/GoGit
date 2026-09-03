package refs

import (
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
)

func commitOne(t *testing.T, store *Store, apply func(*Transaction) error) error {
	t.Helper()
	tx := store.Begin()
	tx.SetMessage("test update")
	if err := apply(tx); err != nil {
		return err
	}
	return tx.Commit()
}

func mustCommit(t *testing.T, store *Store, apply func(*Transaction) error) {
	t.Helper()
	if err := commitOne(t, store, apply); err != nil {
		t.Fatalf("Commit returned error %v", err)
	}
}

func TestCommitCreatesAndUpdatesLooseReference(t *testing.T) {
	dir := newGitDir(t)
	store := openStore(t, dir)

	mustCommit(t, store, func(tx *Transaction) error {
		return tx.Update(BranchName("feature/one"), oidFrom(t, "11"), hash.Zero)
	})
	if got := readAt(t, dir, "refs/heads/feature/one"); got != oidFrom(t, "11").String()+"\n" {
		t.Fatalf("reference file holds %q", got)
	}
	mustCommit(t, store, func(tx *Transaction) error {
		return tx.Update(BranchName("feature/one"), oidFrom(t, "22"), oidFrom(t, "11"))
	})
	ref, err := store.Lookup(BranchName("feature/one"))
	if err != nil || ref.Target != oidFrom(t, "22") {
		t.Fatalf("Lookup returned %+v, %v", ref, err)
	}
	if existsAt(dir, "refs/heads/feature/one.lock") {
		t.Fatal("lock file survived the commit")
	}
}

func TestCommitChecksOldValue(t *testing.T) {
	dir := newGitDir(t)
	writeAt(t, dir, "refs/heads/main", oidFrom(t, "11").String()+"\n")
	store := openStore(t, dir)

	err := commitOne(t, store, func(tx *Transaction) error {
		return tx.Update(BranchName("main"), oidFrom(t, "22"), oidFrom(t, "33"))
	})
	if !errors.Is(err, ErrOldValueMismatch) {
		t.Fatalf("Commit returned %v, want ErrOldValueMismatch", err)
	}
	err = commitOne(t, store, func(tx *Transaction) error {
		return tx.Update(BranchName("main"), oidFrom(t, "22"), hash.Zero)
	})
	if !errors.Is(err, ErrOldValueMismatch) {
		t.Fatalf("Commit over an existing reference returned %v", err)
	}
	err = commitOne(t, store, func(tx *Transaction) error {
		return tx.Update(BranchName("absent"), oidFrom(t, "22"), oidFrom(t, "11"))
	})
	if !errors.Is(err, ErrOldValueMismatch) {
		t.Fatalf("Commit of a missing reference returned %v", err)
	}
	if existsAt(dir, "refs/heads/main.lock") || existsAt(dir, "refs/heads/absent.lock") {
		t.Fatal("failed commit left lock files behind")
	}
	if got := readAt(t, dir, "refs/heads/main"); got != oidFrom(t, "11").String()+"\n" {
		t.Fatalf("reference changed to %q", got)
	}
}

func TestUpdateFollowsSymbolicHeadAndLogsBothNames(t *testing.T) {
	dir := newGitDir(t)
	store := openStore(t, dir)

	mustCommit(t, store, func(tx *Transaction) error {
		return tx.Update(HEAD, oidFrom(t, "11"), hash.Zero)
	})
	if existsAt(dir, "HEAD.lock") {
		t.Fatal("HEAD lock survived")
	}
	if got := readAt(t, dir, "HEAD"); got != "ref: refs/heads/main\n" {
		t.Fatalf("HEAD holds %q", got)
	}
	if got := readAt(t, dir, "refs/heads/main"); got != oidFrom(t, "11").String()+"\n" {
		t.Fatalf("branch holds %q", got)
	}
	for _, name := range []Name{HEAD, BranchName("main")} {
		entry, err := store.ReflogLast(name)
		if err != nil {
			t.Fatalf("ReflogLast(%s) returned error %v", name, err)
		}
		if !entry.Old.IsZero() || entry.New != oidFrom(t, "11") || entry.Message != "test update" {
			t.Fatalf("reflog entry of %s is %+v", name, entry)
		}
	}
}

func TestCommitWritesHeadReflogWhenBranchIsUpdatedDirectly(t *testing.T) {
	dir := newGitDir(t)
	store := openStore(t, dir)
	mustCommit(t, store, func(tx *Transaction) error {
		return tx.Set(BranchName("main"), oidFrom(t, "11"))
	})
	entry, err := store.ReflogLast(HEAD)
	if err != nil {
		t.Fatalf("ReflogLast returned error %v", err)
	}
	if entry.New != oidFrom(t, "11") {
		t.Fatalf("HEAD reflog entry is %+v", entry)
	}
	if lines := strings.Count(readAt(t, dir, "logs/HEAD"), "\n"); lines != 1 {
		t.Fatalf("HEAD reflog holds %d lines", lines)
	}
}

func TestCommitDoesNotDuplicateHeadReflogWhenHeadIsUpdated(t *testing.T) {
	dir := newGitDir(t)
	store := openStore(t, dir)
	mustCommit(t, store, func(tx *Transaction) error {
		return tx.Update(HEAD, oidFrom(t, "11"), hash.Zero)
	})
	if lines := strings.Count(readAt(t, dir, "logs/HEAD"), "\n"); lines != 1 {
		t.Fatalf("HEAD reflog holds %d lines", lines)
	}
}

func TestDetachWritesObjectIntoSymbolicReference(t *testing.T) {
	dir := newGitDir(t)
	writeAt(t, dir, "refs/heads/main", oidFrom(t, "11").String()+"\n")
	store := openStore(t, dir)

	mustCommit(t, store, func(tx *Transaction) error {
		return tx.Detach(HEAD, oidFrom(t, "22"))
	})
	if got := readAt(t, dir, "HEAD"); got != oidFrom(t, "22").String()+"\n" {
		t.Fatalf("HEAD holds %q", got)
	}
	if got := readAt(t, dir, "refs/heads/main"); got != oidFrom(t, "11").String()+"\n" {
		t.Fatalf("branch changed to %q", got)
	}
	entry, err := store.ReflogLast(HEAD)
	if err != nil {
		t.Fatalf("ReflogLast returned error %v", err)
	}
	if entry.Old != oidFrom(t, "11") || entry.New != oidFrom(t, "22") {
		t.Fatalf("HEAD reflog entry is %+v", entry)
	}
}

func TestSetSymbolicWritesSymbolicReferenceAndReflog(t *testing.T) {
	dir := newGitDir(t)
	writeAt(t, dir, "refs/heads/main", oidFrom(t, "11").String()+"\n")
	writeAt(t, dir, "refs/heads/other", oidFrom(t, "22").String()+"\n")
	store := openStore(t, dir)

	mustCommit(t, store, func(tx *Transaction) error {
		return tx.SetSymbolic(HEAD, BranchName("other"))
	})
	if got := readAt(t, dir, "HEAD"); got != "ref: refs/heads/other\n" {
		t.Fatalf("HEAD holds %q", got)
	}
	entry, err := store.ReflogLast(HEAD)
	if err != nil {
		t.Fatalf("ReflogLast returned error %v", err)
	}
	if entry.Old != oidFrom(t, "11") || entry.New != oidFrom(t, "22") {
		t.Fatalf("HEAD reflog entry is %+v", entry)
	}
}

func TestSetSymbolicWithoutMessageSkipsReflog(t *testing.T) {
	dir := newGitDir(t)
	store := openStore(t, dir)
	tx := store.Begin()
	if err := tx.SetSymbolic(HEAD, BranchName("other")); err != nil {
		t.Fatalf("SetSymbolic returned error %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit returned error %v", err)
	}
	if existsAt(dir, "logs/HEAD") {
		t.Fatal("reflog was written without a message")
	}
}

func TestSetSymbolicRejectsBadTargets(t *testing.T) {
	store := openStore(t, newGitDir(t))
	tx := store.Begin()
	if err := tx.SetSymbolic(HEAD, Name("refs/heads/bad name")); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("SetSymbolic returned %v, want ErrInvalidName", err)
	}
	if err := tx.SetSymbolic(HEAD, Name("logs/HEAD")); !errors.Is(err, ErrSymbolicOutsideRefs) {
		t.Fatalf("SetSymbolic returned %v, want ErrSymbolicOutsideRefs", err)
	}
}

func TestDeleteRemovesLooseReferenceReflogAndEmptyDirectories(t *testing.T) {
	dir := newGitDir(t)
	store := openStore(t, dir)
	mustCommit(t, store, func(tx *Transaction) error {
		return tx.Update(BranchName("feature/deep/one"), oidFrom(t, "11"), hash.Zero)
	})
	if !existsAt(dir, "logs/refs/heads/feature/deep/one") {
		t.Fatal("reflog was not created")
	}
	mustCommit(t, store, func(tx *Transaction) error {
		return tx.Delete(BranchName("feature/deep/one"), oidFrom(t, "11"))
	})
	for _, rel := range []string{
		"refs/heads/feature/deep/one",
		"refs/heads/feature/deep",
		"refs/heads/feature",
		"logs/refs/heads/feature/deep/one",
		"logs/refs/heads/feature",
	} {
		if existsAt(dir, rel) {
			t.Errorf("%s survived the delete", rel)
		}
	}
	if !existsAt(dir, "refs/heads") {
		t.Fatal("delete removed the top level directory")
	}
}

func TestDeleteRemovesPackedEntryAndKeepsOthers(t *testing.T) {
	dir := newGitDir(t)
	writeAt(t, dir, packedRefsFile, packedHeaderFull+
		oidFrom(t, "11").String()+" refs/heads/main\n"+
		oidFrom(t, "22").String()+" refs/tags/v1\n"+
		"^"+oidFrom(t, "33").String()+"\n")
	store := openStore(t, dir)

	mustCommit(t, store, func(tx *Transaction) error {
		return tx.Delete(BranchName("main"), oidFrom(t, "11"))
	})
	got := readAt(t, dir, packedRefsFile)
	if strings.Contains(got, "refs/heads/main") {
		t.Fatalf("packed-refs still holds the deleted reference: %q", got)
	}
	if !strings.Contains(got, "^"+oidFrom(t, "33").String()) {
		t.Fatalf("packed-refs lost the peeled line: %q", got)
	}
	if _, err := store.Lookup(BranchName("main")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Lookup returned %v after the delete", err)
	}
}

func TestDeleteOfMissingReferenceIsAccepted(t *testing.T) {
	dir := newGitDir(t)
	store := openStore(t, dir)
	mustCommit(t, store, func(tx *Transaction) error {
		return tx.Delete(BranchName("absent"), hash.Zero)
	})
	if existsAt(dir, "refs/heads/absent.lock") {
		t.Fatal("lock file survived")
	}
	err := commitOne(t, store, func(tx *Transaction) error {
		return tx.Delete(BranchName("absent"), oidFrom(t, "11"))
	})
	if !errors.Is(err, ErrOldValueMismatch) {
		t.Fatalf("Commit returned %v, want ErrOldValueMismatch", err)
	}
}

func TestTransactionRejectsBadInput(t *testing.T) {
	store := openStore(t, newGitDir(t))
	tx := store.Begin()
	if err := tx.Update(Name("refs/heads/bad name"), oidFrom(t, "11"), hash.Zero); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("Update returned %v, want ErrInvalidName", err)
	}
	if err := tx.Set(BranchName("main"), hash.Zero); !errors.Is(err, ErrInvalidTarget) {
		t.Fatalf("Set returned %v, want ErrInvalidTarget", err)
	}
	if err := tx.Set(BranchName("main"), oidFrom(t, "11")); err != nil {
		t.Fatalf("Set returned error %v", err)
	}
	if err := tx.Delete(BranchName("main"), hash.Zero); !errors.Is(err, ErrDuplicateUpdate) {
		t.Fatalf("Delete returned %v, want ErrDuplicateUpdate", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit returned error %v", err)
	}
	if err := tx.Commit(); !errors.Is(err, ErrCommitted) {
		t.Fatalf("second Commit returned %v, want ErrCommitted", err)
	}
	if err := tx.Set(BranchName("late"), oidFrom(t, "11")); !errors.Is(err, ErrCommitted) {
		t.Fatalf("Set after commit returned %v, want ErrCommitted", err)
	}
}

func TestRollbackPreventsCommit(t *testing.T) {
	dir := newGitDir(t)
	store := openStore(t, dir)
	tx := store.Begin()
	if err := tx.Set(BranchName("main"), oidFrom(t, "11")); err != nil {
		t.Fatalf("Set returned error %v", err)
	}
	tx.Rollback()
	if err := tx.Commit(); !errors.Is(err, ErrCommitted) {
		t.Fatalf("Commit returned %v, want ErrCommitted", err)
	}
	if existsAt(dir, "refs/heads/main") {
		t.Fatal("rolled back transaction wrote a reference")
	}
}

func TestCommitFailsWhenTheSameReferenceIsUpdatedTwice(t *testing.T) {
	dir := newGitDir(t)
	writeAt(t, dir, "refs/heads/main", oidFrom(t, "11").String()+"\n")
	store := openStore(t, dir)
	tx := store.Begin()
	if err := tx.Set(HEAD, oidFrom(t, "22")); err != nil {
		t.Fatalf("Set returned error %v", err)
	}
	if err := tx.Set(BranchName("main"), oidFrom(t, "33")); err != nil {
		t.Fatalf("Set returned error %v", err)
	}
	if err := tx.Commit(); !errors.Is(err, ErrDuplicateUpdate) {
		t.Fatalf("Commit returned %v, want ErrDuplicateUpdate", err)
	}
}

func TestCommitFailsWhenHeadEntryIsAlreadyPlanned(t *testing.T) {
	dir := newGitDir(t)
	writeAt(t, dir, "refs/heads/main", oidFrom(t, "11").String()+"\n")
	store := openStore(t, dir)
	mustCommit(t, store, func(tx *Transaction) error {
		if err := tx.Detach(HEAD, oidFrom(t, "22")); err != nil {
			return err
		}
		return tx.Set(BranchName("main"), oidFrom(t, "33"))
	})
	if got := readAt(t, dir, "HEAD"); got != oidFrom(t, "22").String()+"\n" {
		t.Fatalf("HEAD holds %q", got)
	}
	if lines := strings.Count(readAt(t, dir, "logs/HEAD"), "\n"); lines != 1 {
		t.Fatalf("HEAD reflog holds %d lines", lines)
	}
}

func TestCommitFailsWhenReferenceIsLocked(t *testing.T) {
	dir := newGitDir(t)
	writeAt(t, dir, "refs/heads/main.lock", "")
	store := openStore(t, dir)
	err := commitOne(t, store, func(tx *Transaction) error {
		return tx.Set(BranchName("main"), oidFrom(t, "11"))
	})
	if !errors.Is(err, ErrLocked) {
		t.Fatalf("Commit returned %v, want ErrLocked", err)
	}
	if existsAt(dir, "refs/heads/main") {
		t.Fatal("locked reference was written")
	}
}

func TestCommitFailsWhenPackedRefsIsLocked(t *testing.T) {
	dir := newGitDir(t)
	writeAt(t, dir, packedRefsFile, packedHeaderPlain+oidFrom(t, "11").String()+" refs/heads/main\n")
	writeAt(t, dir, packedRefsFile+lockSuffix, "")
	store := openStore(t, dir)
	err := commitOne(t, store, func(tx *Transaction) error {
		return tx.Delete(BranchName("main"), hash.Zero)
	})
	if !errors.Is(err, ErrLocked) {
		t.Fatalf("Commit returned %v, want ErrLocked", err)
	}
}

func TestCommitDetectsNameConflicts(t *testing.T) {
	loose := newGitDir(t)
	writeAt(t, loose, "refs/heads/main", oidFrom(t, "11").String()+"\n")
	looseStore := openStore(t, loose)
	err := commitOne(t, looseStore, func(tx *Transaction) error {
		return tx.Set(BranchName("main/deep"), oidFrom(t, "22"))
	})
	if !errors.Is(err, ErrNameConflict) {
		t.Fatalf("Commit returned %v, want ErrNameConflict", err)
	}

	packed := newGitDir(t)
	writeAt(t, packed, packedRefsFile, packedHeaderPlain+oidFrom(t, "11").String()+" refs/heads/main\n")
	packedStore := openStore(t, packed)
	err = commitOne(t, packedStore, func(tx *Transaction) error {
		return tx.Set(BranchName("main/deep"), oidFrom(t, "22"))
	})
	if !errors.Is(err, ErrNameConflict) {
		t.Fatalf("Commit returned %v, want ErrNameConflict", err)
	}
	err = commitOne(t, packedStore, func(tx *Transaction) error {
		return tx.Set(BranchName("main"), oidFrom(t, "22"))
	})
	if err != nil {
		t.Fatalf("Commit returned error %v", err)
	}

	directory := newGitDir(t)
	writeAt(t, directory, "refs/heads/main/deep", oidFrom(t, "11").String()+"\n")
	directoryStore := openStore(t, directory)
	err = commitOne(t, directoryStore, func(tx *Transaction) error {
		return tx.Set(BranchName("main"), oidFrom(t, "22"))
	})
	if !errors.Is(err, ErrNameConflict) {
		t.Fatalf("Commit returned %v, want ErrNameConflict", err)
	}

	packedChild := newGitDir(t)
	writeAt(t, packedChild, packedRefsFile, packedHeaderPlain+oidFrom(t, "11").String()+" refs/heads/main/deep\n")
	packedChildStore := openStore(t, packedChild)
	err = commitOne(t, packedChildStore, func(tx *Transaction) error {
		return tx.Set(BranchName("main"), oidFrom(t, "22"))
	})
	if !errors.Is(err, ErrNameConflict) {
		t.Fatalf("Commit returned %v, want ErrNameConflict", err)
	}
}

func TestCommitRemovesEmptyDirectoryInTheWay(t *testing.T) {
	dir := newGitDir(t)
	mkdirAt(t, dir, "refs/heads/main/empty")
	store := openStore(t, dir)
	mustCommit(t, store, func(tx *Transaction) error {
		return tx.Set(BranchName("main"), oidFrom(t, "11"))
	})
	if got := readAt(t, dir, "refs/heads/main"); got != oidFrom(t, "11").String()+"\n" {
		t.Fatalf("reference holds %q", got)
	}
}

func TestCommitFailsWhenDirectoryInTheWayHoldsFiles(t *testing.T) {
	dir := newGitDir(t)
	writeAt(t, dir, "refs/heads/main/stale.lock", "")
	store := openStore(t, dir)
	err := commitOne(t, store, func(tx *Transaction) error {
		return tx.Set(BranchName("main"), oidFrom(t, "11"))
	})
	if !errors.Is(err, ErrNameConflict) {
		t.Fatalf("Commit returned %v, want ErrNameConflict", err)
	}
}

func TestCommitFailsWhenDirectoryInTheWayCannotBeRead(t *testing.T) {
	dir := newGitDir(t)
	writeAt(t, dir, "refs/heads/main/deep", oidFrom(t, "11").String()+"\n")
	store := openStore(t, dir)
	err := commitOne(t, store, func(tx *Transaction) error {
		return tx.Delete(BranchName("main/deep"), hash.Zero)
	})
	if err != nil {
		t.Fatalf("Commit returned error %v", err)
	}
	mkdirAt(t, dir, "refs/heads/main")
	swapLstat(t, func(name string) bool { return false })
	err = commitOne(t, store, func(tx *Transaction) error {
		return tx.Set(BranchName("main"), oidFrom(t, "22"))
	})
	if err != nil {
		t.Fatalf("Commit returned error %v", err)
	}
}

func TestCommitRejectsDeleteWithNestedCreateLikeGit(t *testing.T) {
	dir := newGitDir(t)
	writeAt(t, dir, "refs/heads/main", oidFrom(t, "11").String()+"\n")
	store := openStore(t, dir)
	err := commitOne(t, store, func(tx *Transaction) error {
		if err := tx.Delete(BranchName("main"), oidFrom(t, "11")); err != nil {
			return err
		}
		return tx.Set(BranchName("main/deep"), oidFrom(t, "22"))
	})
	if !errors.Is(err, ErrNameConflict) {
		t.Fatalf("Commit returned %v, want ErrNameConflict", err)
	}
	if existsAt(dir, "refs/heads/main.lock") {
		t.Fatal("failed commit left a lock file behind")
	}
}

func TestCommitRejectsConflictingNamesInOneTransaction(t *testing.T) {
	store := openStore(t, newGitDir(t))
	err := commitOne(t, store, func(tx *Transaction) error {
		if err := tx.Set(BranchName("main"), oidFrom(t, "11")); err != nil {
			return err
		}
		return tx.Set(BranchName("main/deep"), oidFrom(t, "22"))
	})
	if !errors.Is(err, ErrNameConflict) {
		t.Fatalf("Commit returned %v, want ErrNameConflict", err)
	}
}

func TestCommitReportsBrokenRepositoryState(t *testing.T) {
	loop := newGitDir(t)
	writeAt(t, loop, "HEAD", "ref: refs/heads/loop\n")
	writeAt(t, loop, "refs/heads/loop", "ref: refs/heads/loop\n")
	loopStore := openStore(t, loop)
	err := commitOne(t, loopStore, func(tx *Transaction) error {
		return tx.Set(HEAD, oidFrom(t, "11"))
	})
	if !errors.Is(err, ErrTooManySymlinks) {
		t.Fatalf("Commit returned %v, want ErrTooManySymlinks", err)
	}

	broken := newGitDir(t)
	writeAt(t, broken, "HEAD", "ref: refs/heads/bad\n")
	writeAt(t, broken, "refs/heads/bad", "not an object id\n")
	brokenStore := openStore(t, broken)
	err = commitOne(t, brokenStore, func(tx *Transaction) error {
		return tx.Set(BranchName("other"), oidFrom(t, "11"))
	})
	if !errors.Is(err, ErrMalformedRef) {
		t.Fatalf("Commit returned %v, want ErrMalformedRef", err)
	}

	packed := newGitDir(t)
	writeAt(t, packed, packedRefsFile, "garbage\n")
	packedStore := openStore(t, packed)
	err = commitOne(t, packedStore, func(tx *Transaction) error {
		return tx.Set(BranchName("main"), oidFrom(t, "11"))
	})
	if !errors.Is(err, ErrMalformedPacked) {
		t.Fatalf("Commit returned %v, want ErrMalformedPacked", err)
	}
}

func TestCommitReportsBrokenStateAfterLocking(t *testing.T) {
	packed := newGitDir(t)
	writeAt(t, packed, "HEAD", oidFrom(t, "11").String()+"\n")
	writeAt(t, packed, "refs/heads/main", oidFrom(t, "22").String()+"\n")
	writeAt(t, packed, packedRefsFile, "garbage\n")
	packedStore := openStore(t, packed)
	err := commitOne(t, packedStore, func(tx *Transaction) error {
		return tx.Detach(BranchName("main"), oidFrom(t, "33"))
	})
	if !errors.Is(err, ErrMalformedPacked) {
		t.Fatalf("Commit returned %v, want ErrMalformedPacked", err)
	}

	malformed := newGitDir(t)
	writeAt(t, malformed, "refs/heads/main", oidFrom(t, "11").String()+"\n")
	writeAt(t, malformed, "refs/heads/broken", "not an object id\n")
	malformedStore := openStore(t, malformed)
	err = commitOne(t, malformedStore, func(tx *Transaction) error {
		return tx.Detach(BranchName("broken"), oidFrom(t, "22"))
	})
	if !errors.Is(err, ErrMalformedRef) {
		t.Fatalf("Commit returned %v, want ErrMalformedRef", err)
	}
}

func TestCommitWithoutHeadFile(t *testing.T) {
	dir := t.TempDir()
	store := openStore(t, dir)
	mustCommit(t, store, func(tx *Transaction) error {
		return tx.Set(BranchName("main"), oidFrom(t, "11"))
	})
	if got := readAt(t, dir, "refs/heads/main"); got != oidFrom(t, "11").String()+"\n" {
		t.Fatalf("reference holds %q", got)
	}
	if existsAt(dir, "logs/HEAD") {
		t.Fatal("reflog of a missing HEAD was written")
	}
}

func TestCommitFailsWhenHeadIsMalformed(t *testing.T) {
	dir := newGitDir(t)
	writeAt(t, dir, "HEAD", "not an object id\n")
	store := openStore(t, dir)
	err := commitOne(t, store, func(tx *Transaction) error {
		return tx.Detach(BranchName("main"), oidFrom(t, "11"))
	})
	if !errors.Is(err, ErrMalformedRef) {
		t.Fatalf("Commit returned %v, want ErrMalformedRef", err)
	}
}

func TestCommitFailsWhenSymbolicTargetIsBroken(t *testing.T) {
	dir := newGitDir(t)
	writeAt(t, dir, "HEAD", oidFrom(t, "99").String()+"\n")
	writeAt(t, dir, "refs/heads/first", "ref: refs/heads/second\n")
	writeAt(t, dir, "refs/heads/second", "ref: refs/heads/first\n")
	store := openStore(t, dir)
	err := commitOne(t, store, func(tx *Transaction) error {
		return tx.Detach(BranchName("first"), oidFrom(t, "11"))
	})
	if !errors.Is(err, ErrTooManySymlinks) {
		t.Fatalf("Commit returned %v, want ErrTooManySymlinks", err)
	}
	err = commitOne(t, store, func(tx *Transaction) error {
		return tx.SetSymbolic(BranchName("third"), BranchName("first"))
	})
	if !errors.Is(err, ErrTooManySymlinks) {
		t.Fatalf("Commit of a symbolic reference returned %v, want ErrTooManySymlinks", err)
	}
}

func TestCommitFailsWhenDirectoryInTheWayHoldsNestedFiles(t *testing.T) {
	dir := newGitDir(t)
	writeAt(t, dir, "refs/heads/main/deep/stale.lock", "")
	store := openStore(t, dir)
	err := commitOne(t, store, func(tx *Transaction) error {
		return tx.Set(BranchName("main"), oidFrom(t, "11"))
	})
	if !errors.Is(err, ErrNameConflict) {
		t.Fatalf("Commit returned %v, want ErrNameConflict", err)
	}
}

func TestCommitFailsWhenWritingTheLockFails(t *testing.T) {
	dir := newGitDir(t)
	store := openStore(t, dir)
	swapWrite(t, 0, errors.New("disk is full"))
	err := commitOne(t, store, func(tx *Transaction) error {
		return tx.Set(BranchName("main"), oidFrom(t, "11"))
	})
	if !errors.Is(err, ErrWriteFailed) {
		t.Fatalf("Commit returned %v, want ErrWriteFailed", err)
	}
	if existsAt(dir, "refs/heads/main.lock") {
		t.Fatal("lock file survived the failure")
	}
}

func TestCommitFailsWhenRenameFails(t *testing.T) {
	dir := newGitDir(t)
	store := openStore(t, dir)
	swapRename(t, func(from string) bool { return from == "refs/heads/main.lock" }, errors.New("busy"))
	err := commitOne(t, store, func(tx *Transaction) error {
		return tx.Set(BranchName("main"), oidFrom(t, "11"))
	})
	if !errors.Is(err, ErrWriteFailed) {
		t.Fatalf("Commit returned %v, want ErrWriteFailed", err)
	}
}

func TestCommitFailsWhenPackedRefsCannotBeWritten(t *testing.T) {
	dir := newGitDir(t)
	writeAt(t, dir, packedRefsFile, packedHeaderPlain+oidFrom(t, "11").String()+" refs/heads/main\n")
	store := openStore(t, dir)
	swapRename(t, func(from string) bool { return from == packedRefsFile+lockSuffix }, errors.New("busy"))
	err := commitOne(t, store, func(tx *Transaction) error {
		return tx.Delete(BranchName("main"), hash.Zero)
	})
	if !errors.Is(err, ErrWriteFailed) {
		t.Fatalf("Commit returned %v, want ErrWriteFailed", err)
	}
	if existsAt(dir, packedRefsFile+lockSuffix) {
		t.Fatal("packed-refs lock survived the failure")
	}
}

func TestCommitFailsWhenPackedRefsContentCannotBeWritten(t *testing.T) {
	dir := newGitDir(t)
	writeAt(t, dir, packedRefsFile, packedHeaderPlain+oidFrom(t, "11").String()+" refs/heads/main\n")
	store := openStore(t, dir)
	swapWrite(t, 0, errors.New("disk is full"))
	err := commitOne(t, store, func(tx *Transaction) error {
		return tx.Delete(BranchName("main"), hash.Zero)
	})
	if !errors.Is(err, ErrWriteFailed) {
		t.Fatalf("Commit returned %v, want ErrWriteFailed", err)
	}
	if existsAt(dir, packedRefsFile+lockSuffix) {
		t.Fatal("packed-refs lock survived the failure")
	}
}

func TestDeleteFailsWhenLooseFileCannotBeRemoved(t *testing.T) {
	dir := newGitDir(t)
	writeAt(t, dir, packedRefsFile, packedHeaderPlain+oidFrom(t, "11").String()+" refs/heads/main\n")
	mkdirAt(t, dir, "refs/heads/main/deep")
	store := openStore(t, dir)
	err := commitOne(t, store, func(tx *Transaction) error {
		return tx.Delete(BranchName("main"), hash.Zero)
	})
	if !errors.Is(err, ErrWriteFailed) {
		t.Fatalf("Commit returned %v, want ErrWriteFailed", err)
	}
}

func TestDeleteFailsWhenReflogCannotBeRemoved(t *testing.T) {
	dir := newGitDir(t)
	writeAt(t, dir, "refs/heads/main", oidFrom(t, "11").String()+"\n")
	store := openStore(t, dir)
	swapRemove(t, func(name string) bool { return name == "logs/refs/heads/main" }, errors.New("busy"))
	err := commitOne(t, store, func(tx *Transaction) error {
		return tx.Delete(BranchName("main"), hash.Zero)
	})
	if !errors.Is(err, ErrWriteFailed) {
		t.Fatalf("Commit returned %v, want ErrWriteFailed", err)
	}
}

func TestConcurrentTransactionsKeepReferencesConsistent(t *testing.T) {
	dir := newGitDir(t)
	store := openStore(t, dir)
	var wait sync.WaitGroup
	for index := range 8 {
		wait.Go(func() {
			tx := store.Begin()
			tx.SetMessage("concurrent")
			if err := tx.Update(BranchName("shared"), oidFrom(t, "11"), hash.Zero); err != nil {
				return
			}
			_ = tx.Commit()
		})
		wait.Go(func() {
			name := BranchName("worker/" + string(rune('a'+index)))
			tx := store.Begin()
			tx.SetMessage("concurrent")
			if err := tx.Update(name, oidFrom(t, "22"), hash.Zero); err != nil {
				t.Errorf("Update returned error %v", err)
				return
			}
			if err := tx.Commit(); err != nil {
				t.Errorf("Commit returned error %v", err)
			}
		})
	}
	wait.Wait()

	shared, err := store.Lookup(BranchName("shared"))
	if err != nil || shared.Target != oidFrom(t, "11") {
		t.Fatalf("Lookup returned %+v, %v", shared, err)
	}
	for index := range 8 {
		name := BranchName("worker/" + string(rune('a'+index)))
		if _, err := store.Lookup(name); err != nil {
			t.Errorf("Lookup(%s) returned error %v", name, err)
		}
	}
	for ref, err := range store.All() {
		if err != nil {
			t.Fatalf("All returned error %v", err)
		}
		if ref.Target.IsZero() {
			t.Fatalf("%s has no value", ref.Name)
		}
	}
}
