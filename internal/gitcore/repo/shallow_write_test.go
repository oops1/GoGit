package repo

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
)

func TestWriteShallowCreatesFileWithGivenIDsInOrder(t *testing.T) {
	base := tempDir(t)
	work := makeDir(t, filepath.Join(base, "work"))
	gitDir := plainGitDir(t, filepath.Join(work, dotGit))
	one := mustParseHex(t, "1111111111111111111111111111111111111111")
	two := mustParseHex(t, "2222222222222222222222222222222222222222")

	repository := openRepo(t, work, openOptions(t, env{}))
	if err := repository.WriteShallow([]hash.ObjectID{one, two}); err != nil {
		t.Fatalf("WriteShallow returned error %v", err)
	}

	want := one.String() + "\n" + two.String() + "\n"
	if got := readFile(t, filepath.Join(gitDir, shallowFileName)); got != want {
		t.Fatalf("shallow file contains %q, want %q", got, want)
	}
	if _, err := repository.Root().Lstat(shallowFileName + shallowLockSuffix); err == nil {
		t.Fatal("WriteShallow left a lock file behind")
	}
}

func TestWriteShallowRoundTripsThroughShallow(t *testing.T) {
	base := tempDir(t)
	work := makeDir(t, filepath.Join(base, "work"))
	plainGitDir(t, filepath.Join(work, dotGit))
	one := mustParseHex(t, "3333333333333333333333333333333333333333")

	repository := openRepo(t, work, openOptions(t, env{}))
	if err := repository.WriteShallow([]hash.ObjectID{one}); err != nil {
		t.Fatalf("WriteShallow returned error %v", err)
	}

	shallow, err := repository.Shallow()
	if err != nil {
		t.Fatalf("Shallow returned error %v", err)
	}
	if _, ok := shallow[one]; !ok || len(shallow) != 1 {
		t.Fatalf("Shallow returned %v, want only %s", shallow, one)
	}
}

func TestWriteShallowWithEmptyListRemovesTheFile(t *testing.T) {
	base := tempDir(t)
	work := makeDir(t, filepath.Join(base, "work"))
	gitDir := plainGitDir(t, filepath.Join(work, dotGit))
	one := mustParseHex(t, "4444444444444444444444444444444444444444")

	repository := openRepo(t, work, openOptions(t, env{}))
	if err := repository.WriteShallow([]hash.ObjectID{one}); err != nil {
		t.Fatalf("WriteShallow returned error %v", err)
	}
	if err := repository.WriteShallow(nil); err != nil {
		t.Fatalf("WriteShallow(nil) returned error %v", err)
	}
	if _, err := repository.Root().Lstat(shallowFileName); err == nil {
		t.Fatalf("shallow file %s still exists after WriteShallow(nil)", filepath.Join(gitDir, shallowFileName))
	}
}

func TestWriteShallowWithEmptyListIsNoopWhenFileIsMissing(t *testing.T) {
	base := tempDir(t)
	work := makeDir(t, filepath.Join(base, "work"))
	plainGitDir(t, filepath.Join(work, dotGit))

	repository := openRepo(t, work, openOptions(t, env{}))
	if err := repository.WriteShallow(nil); err != nil {
		t.Fatalf("WriteShallow(nil) returned error %v", err)
	}
}

func TestWriteShallowFailsWhenLockPathIsADirectory(t *testing.T) {
	base := tempDir(t)
	work := makeDir(t, filepath.Join(base, "work"))
	gitDir := plainGitDir(t, filepath.Join(work, dotGit))
	makeDir(t, filepath.Join(gitDir, shallowFileName+shallowLockSuffix))
	one := mustParseHex(t, "5555555555555555555555555555555555555555")

	repository := openRepo(t, work, openOptions(t, env{}))
	if err := repository.WriteShallow([]hash.ObjectID{one}); err == nil {
		t.Fatal("WriteShallow returned no error for a directory named shallow.lock")
	}
}

func TestWriteShallowFailsAndCleansUpWhenTargetIsADirectory(t *testing.T) {
	base := tempDir(t)
	work := makeDir(t, filepath.Join(base, "work"))
	gitDir := plainGitDir(t, filepath.Join(work, dotGit))
	makeDir(t, filepath.Join(gitDir, shallowFileName))
	one := mustParseHex(t, "6666666666666666666666666666666666666666")

	repository := openRepo(t, work, openOptions(t, env{}))
	if err := repository.WriteShallow([]hash.ObjectID{one}); err == nil {
		t.Fatal("WriteShallow returned no error for a directory named shallow")
	}
	if _, err := repository.Root().Lstat(shallowFileName + shallowLockSuffix); err == nil {
		t.Fatal("WriteShallow left a lock file behind after a failed rename")
	}
}

func TestWriteShallowWithEmptyListFailsWhenTargetIsANonEmptyDirectory(t *testing.T) {
	base := tempDir(t)
	work := makeDir(t, filepath.Join(base, "work"))
	gitDir := plainGitDir(t, filepath.Join(work, dotGit))
	writeFile(t, filepath.Join(gitDir, shallowFileName, "entry"), "x")

	repository := openRepo(t, work, openOptions(t, env{}))
	if err := repository.WriteShallow(nil); err == nil {
		t.Fatal("WriteShallow(nil) returned no error for a non-empty directory named shallow")
	}
}

func TestIsShallowFalseWhenFileIsMissing(t *testing.T) {
	base := tempDir(t)
	work := makeDir(t, filepath.Join(base, "work"))
	plainGitDir(t, filepath.Join(work, dotGit))

	repository := openRepo(t, work, openOptions(t, env{}))
	shallow, err := repository.IsShallow()
	if err != nil {
		t.Fatalf("IsShallow returned error %v", err)
	}
	if shallow {
		t.Fatal("IsShallow returned true for a repository without a shallow file")
	}
}

func TestIsShallowTrueAfterWriteShallow(t *testing.T) {
	base := tempDir(t)
	work := makeDir(t, filepath.Join(base, "work"))
	plainGitDir(t, filepath.Join(work, dotGit))
	one := mustParseHex(t, "7777777777777777777777777777777777777777")

	repository := openRepo(t, work, openOptions(t, env{}))
	if err := repository.WriteShallow([]hash.ObjectID{one}); err != nil {
		t.Fatalf("WriteShallow returned error %v", err)
	}
	shallow, err := repository.IsShallow()
	if err != nil {
		t.Fatalf("IsShallow returned error %v", err)
	}
	if !shallow {
		t.Fatal("IsShallow returned false after WriteShallow with an id")
	}
}

func TestIsShallowPropagatesParseErrors(t *testing.T) {
	base := tempDir(t)
	work := makeDir(t, filepath.Join(base, "work"))
	gitDir := plainGitDir(t, filepath.Join(work, dotGit))
	writeFile(t, filepath.Join(gitDir, shallowFileName), "not-a-hex-id\n")

	repository := openRepo(t, work, openOptions(t, env{}))
	if _, err := repository.IsShallow(); !errors.Is(err, ErrInvalidShallowFile) {
		t.Fatalf("IsShallow returned %v, want %v", err, ErrInvalidShallowFile)
	}
}
