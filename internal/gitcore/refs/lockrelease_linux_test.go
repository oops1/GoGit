package refs

import (
	"errors"
	"syscall"
	"testing"
)

func TestLockPackedGivesUpAtOnceWhenAccessIsDenied(t *testing.T) {
	store := openStore(t, newGitDir(t))
	calls := swapOpenFile(t, func(string, int) bool { return true }, syscall.EACCES)

	_, err := store.lockPacked()

	if !errors.Is(err, ErrWriteFailed) || !errors.Is(err, syscall.EACCES) {
		t.Fatalf("lockPacked returned %v, want the write failure with access denied", err)
	}
	if *calls != 1 {
		t.Fatalf("lockPacked tried %d times, want a single attempt", *calls)
	}
}
