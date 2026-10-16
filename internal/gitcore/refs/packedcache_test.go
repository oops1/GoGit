package refs

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/oops1/gogit/internal/gitcore/hash"
)

const packedCacheFile = packedHeaderPlain +
	"1111111111111111111111111111111111111111 refs/heads/main\n" +
	"2222222222222222222222222222222222222222 refs/heads/feature\n"

func countPackedParses(t *testing.T) *int {
	t.Helper()
	parses := new(int)
	previous := parsePackedSnapshot
	parsePackedSnapshot = func(data []byte) (*packedSnapshot, error) {
		*parses++
		return previous(data)
	}
	t.Cleanup(func() { parsePackedSnapshot = previous })
	return parses
}

func agePackedFile(t *testing.T, dir string, age time.Duration) {
	t.Helper()
	when := time.Now().Add(-age)
	if err := os.Chtimes(filepath.Join(dir, packedRefsFile), when, when); err != nil {
		t.Fatalf("Chtimes returned error %v", err)
	}
}

func TestLookupParsesAnUnchangedPackedRefsFileOnce(t *testing.T) {
	dir := newGitDir(t)
	writeAt(t, dir, packedRefsFile, packedCacheFile)
	agePackedFile(t, dir, time.Hour)
	store := openStore(t, dir)
	parses := countPackedParses(t)

	for range 100 {
		if _, err := store.Lookup(BranchName("main")); err != nil {
			t.Fatalf("Lookup returned error %v", err)
		}
		if _, err := store.Lookup(BranchName("missing")); !errors.Is(err, ErrNotFound) {
			t.Fatalf("Lookup of a missing reference returned %v", err)
		}
	}

	if *parses != 1 {
		t.Fatalf("packed-refs was parsed %d times, want once", *parses)
	}
}

func TestLookupReadsAChangedPackedRefsFileAgain(t *testing.T) {
	dir := newGitDir(t)
	writeAt(t, dir, packedRefsFile, packedCacheFile)
	agePackedFile(t, dir, time.Hour)
	store := openStore(t, dir)
	parses := countPackedParses(t)
	if _, err := store.Lookup(BranchName("main")); err != nil {
		t.Fatalf("Lookup returned error %v", err)
	}

	writeAt(t, dir, packedRefsFile, packedCacheFile+"3333333333333333333333333333333333333333 refs/heads/extra\n")
	agePackedFile(t, dir, time.Hour)
	ref, err := store.Lookup(BranchName("extra"))

	if err != nil || ref.Target != oidFrom(t, "3333333333333333333333333333333333333333") {
		t.Fatalf("Lookup of a reference added to packed-refs returned %v, %v", ref, err)
	}
	if *parses != 2 {
		t.Fatalf("packed-refs was parsed %d times, want twice", *parses)
	}
}

func TestLookupReadsAChangedPackedRefsFileOfTheSameSizeAgain(t *testing.T) {
	dir := newGitDir(t)
	writeAt(t, dir, packedRefsFile, packedCacheFile)
	agePackedFile(t, dir, time.Hour)
	store := openStore(t, dir)
	if _, err := store.Lookup(BranchName("main")); err != nil {
		t.Fatalf("Lookup returned error %v", err)
	}

	writeAt(t, dir, packedRefsFile, packedHeaderPlain+
		"4444444444444444444444444444444444444444 refs/heads/main\n"+
		"2222222222222222222222222222222222222222 refs/heads/feature\n")
	agePackedFile(t, dir, 30*time.Minute)
	ref, err := store.Lookup(BranchName("main"))

	if err != nil || ref.Target != oidFrom(t, "4444444444444444444444444444444444444444") {
		t.Fatalf("Lookup after a same-size rewrite returned %v, %v", ref, err)
	}
}

func TestLookupDoesNotTrustAPackedRefsFileWrittenJustNow(t *testing.T) {
	dir := newGitDir(t)
	writeAt(t, dir, packedRefsFile, packedCacheFile)
	store := openStore(t, dir)
	parses := countPackedParses(t)

	for range 3 {
		if _, err := store.Lookup(BranchName("main")); err != nil {
			t.Fatalf("Lookup returned error %v", err)
		}
	}

	if *parses != 3 {
		t.Fatalf("packed-refs was parsed %d times, want every time while the file is fresh", *parses)
	}
}

func TestLookupTrustsAPackedRefsFileOnceItHasSettled(t *testing.T) {
	dir := newGitDir(t)
	writeAt(t, dir, packedRefsFile, packedCacheFile)
	store := openStore(t, dir)
	parses := countPackedParses(t)
	info, err := os.Lstat(filepath.Join(dir, packedRefsFile))
	if err != nil {
		t.Fatalf("Lstat returned error %v", err)
	}
	previous := packedClock
	packedClock = func() time.Time { return info.ModTime().Add(packedCacheSettle) }
	t.Cleanup(func() { packedClock = previous })

	for range 3 {
		if _, err := store.Lookup(BranchName("main")); err != nil {
			t.Fatalf("Lookup returned error %v", err)
		}
	}

	if *parses != 1 {
		t.Fatalf("packed-refs was parsed %d times, want once after it settled", *parses)
	}
}

func TestLookupForgetsAPackedRefsFileThatWasRemoved(t *testing.T) {
	dir := newGitDir(t)
	writeAt(t, dir, packedRefsFile, packedCacheFile)
	agePackedFile(t, dir, time.Hour)
	store := openStore(t, dir)
	if _, err := store.Lookup(BranchName("main")); err != nil {
		t.Fatalf("Lookup returned error %v", err)
	}

	if err := os.Remove(filepath.Join(dir, packedRefsFile)); err != nil {
		t.Fatalf("Remove returned error %v", err)
	}

	if _, err := store.Lookup(BranchName("main")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Lookup after removing packed-refs returned %v, want ErrNotFound", err)
	}
}

func TestLookupForgetsAPackedRefsFileThatVanishesBeforeItIsRead(t *testing.T) {
	dir := newGitDir(t)
	writeAt(t, dir, packedRefsFile, packedCacheFile)
	agePackedFile(t, dir, time.Hour)
	store := openStore(t, dir)
	if _, err := store.Lookup(BranchName("main")); err != nil {
		t.Fatalf("Lookup returned error %v", err)
	}
	writeAt(t, dir, packedRefsFile, packedCacheFile+"3333333333333333333333333333333333333333 refs/heads/extra\n")
	statCalls := 0
	previous := fsLstat
	fsLstat = func(root *os.Root, name string) (fs.FileInfo, error) {
		if filepath.ToSlash(name) == packedRefsFile {
			statCalls++
			if statCalls > 1 {
				return nil, os.ErrNotExist
			}
		}
		return previous(root, name)
	}
	t.Cleanup(func() { fsLstat = previous })

	snapshot, err := store.loadPacked()

	if err != nil || len(snapshot.refs) != 0 {
		t.Fatalf("loadPacked of a file that vanished returned %v, %v", snapshot, err)
	}
	if store.packed.lookup(packedStamp{}) != nil {
		t.Fatal("the cache kept a snapshot of a file that vanished")
	}
}

func TestLookupReportsAMalformedPackedRefsFileWithoutCachingIt(t *testing.T) {
	dir := newGitDir(t)
	writeAt(t, dir, packedRefsFile, "not a packed-refs file\n")
	agePackedFile(t, dir, time.Hour)
	store := openStore(t, dir)
	parses := countPackedParses(t)

	for range 2 {
		if _, err := store.Lookup(BranchName("main")); !errors.Is(err, ErrMalformedPacked) {
			t.Fatalf("Lookup returned %v, want ErrMalformedPacked", err)
		}
	}

	if *parses != 2 {
		t.Fatalf("a malformed file was parsed %d times, want every time", *parses)
	}
}

func TestLookupTreatsAPackedRefsDirectoryAsAbsent(t *testing.T) {
	dir := newGitDir(t)
	mkdirAt(t, dir, packedRefsFile)
	store := openStore(t, dir)

	if _, err := store.Lookup(BranchName("main")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Lookup returned %v, want ErrNotFound", err)
	}
}

func TestTransactionSeesPackedRefsWrittenByAnEarlierTransaction(t *testing.T) {
	dir := newGitDir(t)
	writeAt(t, dir, packedRefsFile, packedCacheFile)
	agePackedFile(t, dir, time.Hour)
	store := openStore(t, dir)
	if _, err := store.Lookup(BranchName("feature")); err != nil {
		t.Fatalf("Lookup returned error %v", err)
	}

	tx := store.Begin()
	if err := tx.Delete(BranchName("feature"), hash.Zero); err != nil {
		t.Fatalf("Delete returned error %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit returned error %v", err)
	}

	if _, err := store.Lookup(BranchName("feature")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Lookup of a deleted packed reference returned %v, want ErrNotFound", err)
	}
	if _, err := store.Lookup(BranchName("main")); err != nil {
		t.Fatalf("Lookup of a surviving packed reference returned %v", err)
	}
}
