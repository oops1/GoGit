package refs

import (
	"errors"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestLockPackedWaitsWhileTheReleasedLockIsStillBeingDeleted(t *testing.T) {
	store := openStore(t, newGitDir(t))
	calls := swapOpenFile(t, func(_ string, call int) bool { return call <= 3 }, windows.ERROR_ACCESS_DENIED)

	lock, err := store.lockPacked()

	if err != nil {
		t.Fatalf("lockPacked returned %v while the old lock was only being deleted", err)
	}
	lock.release()
	if *calls != 4 {
		t.Fatalf("lockPacked tried %d times, want three refusals and one success", *calls)
	}
}

func TestLockPackedReportsAnAccessDeniedThatOutlastsTheTimeout(t *testing.T) {
	store := openStore(t, newGitDir(t))
	shortenPackedLockTimeout(t, 30*time.Millisecond)
	calls := swapOpenFile(t, func(string, int) bool { return true }, windows.ERROR_ACCESS_DENIED)

	_, err := store.lockPacked()

	if !errors.Is(err, ErrWriteFailed) || !errors.Is(err, windows.ERROR_ACCESS_DENIED) || errors.Is(err, ErrLocked) {
		t.Fatalf("lockPacked returned %v, want the write failure with access denied", err)
	}
	if *calls < 2 {
		t.Fatalf("lockPacked tried %d times, want it to retry before giving up", *calls)
	}
}

func shortenPackedLockTimeout(t *testing.T, d time.Duration) {
	t.Helper()
	previous := packedLockTimeout
	packedLockTimeout = d
	t.Cleanup(func() { packedLockTimeout = previous })
}
