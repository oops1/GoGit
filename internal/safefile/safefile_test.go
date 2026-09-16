package safefile

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func shortLockTimeout(t *testing.T) {
	t.Helper()
	prevTimeout, prevInterval := lockTimeout, lockInterval
	lockTimeout, lockInterval = 60*time.Millisecond, 5*time.Millisecond
	t.Cleanup(func() { lockTimeout, lockInterval = prevTimeout, prevInterval })
}

func TestAcquireCreatesMissingDirectoryAndLockFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "data.bin")
	lock, err := Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lock.Release() }()
	if _, err := os.Stat(LockPath(path)); err != nil {
		t.Fatalf("lock file missing: %v", err)
	}
}

func TestAcquireFailsWhenParentIsAFile(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Acquire(filepath.Join(blocker, "data.bin")); err == nil {
		t.Fatal("expected an error")
	}
}

func TestAcquireFailsWhenLockPathIsADirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.bin")
	if err := os.Mkdir(LockPath(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := Acquire(path); err == nil {
		t.Fatal("expected an error")
	}
}

func TestAcquireTimesOutWhileAnotherHolderKeepsTheLock(t *testing.T) {
	shortLockTimeout(t)
	path := filepath.Join(t.TempDir(), "data.bin")
	first, err := Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = first.Release() }()
	if _, err := Acquire(path); !errors.Is(err, ErrLockTimeout) {
		t.Fatalf("err = %v, want ErrLockTimeout", err)
	}
}

func TestAcquireWaitsForTheHolderToRelease(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.bin")
	first, err := Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	released := make(chan struct{})
	go func() {
		time.Sleep(30 * time.Millisecond)
		_ = first.Release()
		close(released)
	}()
	second, err := Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	<-released
	if err := second.Release(); err != nil {
		t.Fatal(err)
	}
}

func TestReadReturnsFileContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.bin")
	if err := os.WriteFile(path, []byte("content"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Read(path)
	if err != nil || string(got) != "content" {
		t.Fatalf("Read = %q, %v", got, err)
	}
}

func TestReadFailsWhenLockCannotBeTaken(t *testing.T) {
	shortLockTimeout(t)
	path := filepath.Join(t.TempDir(), "data.bin")
	lock, err := Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lock.Release() }()
	if _, err := Read(path); !errors.Is(err, ErrLockTimeout) {
		t.Fatalf("err = %v, want ErrLockTimeout", err)
	}
}

func TestUpdatePassesNilForMissingFileAndWritesResult(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.bin")
	err := Update(path, func(current []byte) ([]byte, error) {
		if current != nil {
			t.Fatalf("current = %q, want nil", current)
		}
		return []byte("first"), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	err = Update(path, func(current []byte) ([]byte, error) {
		return append(current, "+second"...), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "first+second" {
		t.Fatalf("got %q", got)
	}
}

func TestUpdateSkipsWritingWhenChangeReturnsNil(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.bin")
	if err := Update(path, func([]byte) ([]byte, error) { return nil, nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("file must not exist, stat err = %v", err)
	}
}

func TestUpdatePropagatesChangeError(t *testing.T) {
	want := errors.New("boom")
	path := filepath.Join(t.TempDir(), "data.bin")
	if err := Update(path, func([]byte) ([]byte, error) { return []byte("x"), want }); !errors.Is(err, want) {
		t.Fatalf("err = %v, want %v", err, want)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("file must not be written, stat err = %v", err)
	}
}

func TestUpdateFailsWhenLockCannotBeTaken(t *testing.T) {
	shortLockTimeout(t)
	path := filepath.Join(t.TempDir(), "data.bin")
	lock, err := Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lock.Release() }()
	if err := Update(path, func([]byte) ([]byte, error) { return nil, nil }); !errors.Is(err, ErrLockTimeout) {
		t.Fatalf("err = %v, want ErrLockTimeout", err)
	}
}

func TestUpdateFailsWhenTargetCannotBeRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.bin")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	called := false
	err := Update(path, func([]byte) ([]byte, error) {
		called = true
		return nil, nil
	})
	if err == nil || called {
		t.Fatalf("err = %v, called = %v; want a read error before change", err, called)
	}
}

func TestUpdateSerialisesConcurrentWriters(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.bin")
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			err := Update(path, func(current []byte) ([]byte, error) {
				return append(current, 'x'), nil
			})
			if err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	got, _ := os.ReadFile(path)
	if string(got) != strings.Repeat("x", 8) {
		t.Fatalf("got %q, want every writer's byte", got)
	}
}

func TestWriteFileReplacesContentAndLeavesNoTemporaryFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data.bin")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(path, []byte("new")); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "new" {
		t.Fatalf("got %q", got)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %v, want only the target", entries)
	}
}

func TestWriteFileFailsWhenDirectoryIsMissing(t *testing.T) {
	if err := WriteFile(filepath.Join(t.TempDir(), "missing", "data.bin"), []byte("x")); err == nil {
		t.Fatal("expected an error")
	}
}

func TestWriteFileFailsAndCleansUpWhenTargetIsANonEmptyDirectory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data.bin")
	if err := os.MkdirAll(filepath.Join(path, "child"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(path, []byte("x")); err == nil {
		t.Fatal("expected an error")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %v, the temporary file must be removed", entries)
	}
}

func TestWrapKeepsNilAndWrapsErrors(t *testing.T) {
	if wrap(nil) != nil {
		t.Fatal("nil must stay nil")
	}
	want := errors.New("boom")
	if err := wrap(want); !errors.Is(err, want) {
		t.Fatalf("err = %v", err)
	}
}
