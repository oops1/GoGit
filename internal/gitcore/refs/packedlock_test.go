package refs

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/oops1/gogit/internal/gitcore/hash"
)

func TestCommitWaitsForABriefPackedRefsLock(t *testing.T) {
	dir := newGitDir(t)
	writeAt(t, dir, packedRefsFile, packedHeaderPlain+
		oidFrom(t, "11").String()+" refs/heads/main\n"+
		oidFrom(t, "22").String()+" refs/heads/other\n")
	writeAt(t, dir, packedRefsFile+lockSuffix, "")
	store := openStore(t, dir)
	released := make(chan struct{})
	go func() {
		defer close(released)
		time.Sleep(100 * time.Millisecond)
		_ = os.Remove(filepath.Join(dir, packedRefsFile+lockSuffix))
	}()

	err := commitOne(t, store, func(tx *Transaction) error {
		return tx.Delete(BranchName("main"), hash.Zero)
	})
	<-released
	if err != nil {
		t.Fatalf("Commit returned %v after the lock went away", err)
	}
	packed := readAt(t, dir, packedRefsFile)
	if strings.Contains(packed, "refs/heads/main") || !strings.Contains(packed, "refs/heads/other") {
		t.Fatalf("packed-refs holds %q", packed)
	}
}

func TestADeletionOfALooseRefStillHoldsPackedRefsBack(t *testing.T) {
	dir := newGitDir(t)
	writeAt(t, dir, packedRefsFile, packedHeaderPlain+oidFrom(t, "22").String()+" refs/heads/other\n")
	writeAt(t, dir, "refs/heads/loose", oidFrom(t, "11").String()+"\n")
	store := openStore(t, dir)
	if err := commitOne(t, store, func(tx *Transaction) error {
		return tx.Delete(BranchName("loose"), hash.Zero)
	}); err != nil {
		t.Fatal(err)
	}
	if existsAt(dir, "refs/heads/loose") || existsAt(dir, packedRefsFile+lockSuffix) {
		t.Fatal("the loose ref or the packed-refs lock survived")
	}
	if !strings.Contains(readAt(t, dir, packedRefsFile), "refs/heads/other") {
		t.Fatal("packed-refs lost a reference")
	}
}

func TestPackRefsKeepsLooseRefsWhenPackedRefsCannotBeWritten(t *testing.T) {
	dir := newGitDir(t)
	writeAt(t, dir, "refs/heads/main", oidFrom(t, "11").String()+"\n")
	store := openStore(t, dir)
	swapRename(t, func(from string) bool { return from == packedRefsFile+lockSuffix }, errors.New("busy"))
	if err := store.PackRefs(true); !errors.Is(err, ErrWriteFailed) {
		t.Fatalf("PackRefs returned %v, want ErrWriteFailed", err)
	}
	if !existsAt(dir, "refs/heads/main") || existsAt(dir, packedRefsFile+lockSuffix) {
		t.Fatal("a failed pack pruned the loose ref or left its lock")
	}
}
