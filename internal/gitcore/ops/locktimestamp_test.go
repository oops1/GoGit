package ops

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestIndexLockCommitTakesTheTimestampOfTheWrittenIndex(t *testing.T) {
	tr := newTestRepo(t)
	lock, err := lockIndex(tr.repo)
	if err != nil {
		t.Fatalf("lockIndex returned error %v", err)
	}
	lock.idx.Timestamp = time.Unix(1, 0)

	if err := lock.commit(); err != nil {
		t.Fatalf("commit returned error %v", err)
	}

	info, err := os.Stat(tr.repo.IndexFile())
	if err != nil {
		t.Fatalf("Stat returned error %v", err)
	}
	if !lock.idx.Timestamp.Equal(info.ModTime()) {
		t.Fatalf("Timestamp = %v, want the time %v the index was written", lock.idx.Timestamp, info.ModTime())
	}
}

func TestIndexLockCommitFailsWhenTheWrittenLockCannotBeInspected(t *testing.T) {
	tr := newTestRepo(t)
	lock, err := lockIndex(tr.repo)
	if err != nil {
		t.Fatalf("lockIndex returned error %v", err)
	}
	original := fsRootLstat
	fsRootLstat = func(root *os.Root, name string) (fs.FileInfo, error) {
		if name == indexLockName {
			return nil, errInjected
		}
		return original(root, name)
	}
	t.Cleanup(func() { fsRootLstat = original })

	if err := lock.commit(); !errors.Is(err, errInjected) {
		t.Fatalf("commit returned %v, want the injected failure", err)
	}
	if _, err := os.Lstat(filepath.Join(tr.repo.GitDir(), indexLockName)); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("the lock survived the failed commit: %v", err)
	}
}
