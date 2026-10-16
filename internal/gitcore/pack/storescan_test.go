package pack

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func countDirectoryScans(t *testing.T) *int {
	t.Helper()
	scans := new(int)
	previous := readDirPath
	readDirPath = func(name string) ([]fs.DirEntry, error) {
		*scans++
		return previous(name)
	}
	t.Cleanup(func() { readDirPath = previous })
	return scans
}

func ageDirectory(t *testing.T, dir string, age time.Duration) {
	t.Helper()
	when := time.Now().Add(-age)
	if err := os.Chtimes(dir, when, when); err != nil {
		t.Fatalf("Chtimes returned error %v", err)
	}
}

func addFixturePack(t *testing.T, dir, name string) {
	t.Helper()
	for _, suffix := range []string{packSuffix, indexSuffix} {
		writeTemp(t, filepath.Join(dir, name+suffix), readFixture(t, filepath.Join(packsDir, name+suffix)))
	}
}

func TestStoreReloadDoesNotScanAnUnchangedSettledDirectoryAgain(t *testing.T) {
	names := fixtureNames(t)
	dir := copyFixtureDir(t, names[0])
	ageDirectory(t, dir, time.Hour)
	scans := countDirectoryScans(t)
	store := openStore(t, dir)

	for range 100 {
		if changed, err := store.Reload(); changed || err != nil {
			t.Fatalf("Reload returned (%v, %v), want (false, nil)", changed, err)
		}
	}

	if *scans != 1 {
		t.Fatalf("the directory was scanned %d times, want once", *scans)
	}
}

func TestStoreReloadScansAgainOnceTheDirectoryChanges(t *testing.T) {
	names := fixtureNames(t)
	dir := copyFixtureDir(t, names[0])
	ageDirectory(t, dir, time.Hour)
	store := openStore(t, dir)
	if changed, err := store.Reload(); changed || err != nil {
		t.Fatalf("Reload returned (%v, %v), want (false, nil)", changed, err)
	}
	scans := countDirectoryScans(t)

	addFixturePack(t, dir, names[1])

	if changed, err := store.Reload(); !changed || err != nil {
		t.Fatalf("Reload returned (%v, %v), want (true, nil)", changed, err)
	}
	if *scans != 1 || len(store.Files()) != 2 {
		t.Fatalf("scans = %d, packfiles = %d, want one scan and two packfiles", *scans, len(store.Files()))
	}
}

func TestStoreReloadDoesNotTrustADirectoryThatChangedJustNow(t *testing.T) {
	dir := copyFixtureDir(t, fixtureNames(t)[0])
	store := openStore(t, dir)
	scans := countDirectoryScans(t)

	for range 3 {
		if _, err := store.Reload(); err != nil {
			t.Fatalf("Reload returned error %v", err)
		}
	}

	if *scans != 3 {
		t.Fatalf("the directory was scanned %d times, want every time while it is fresh", *scans)
	}
}

func TestStoreReloadScansWhenTheDirectoryCannotBeInspected(t *testing.T) {
	dir := copyFixtureDir(t, fixtureNames(t)[0])
	ageDirectory(t, dir, time.Hour)
	store := openStore(t, dir)
	scans := countDirectoryScans(t)
	previous := statPath
	statPath = func(name string) (os.FileInfo, error) {
		if name == dir {
			return nil, os.ErrPermission
		}
		return previous(name)
	}
	t.Cleanup(func() { statPath = previous })

	for range 2 {
		if _, err := store.Reload(); err != nil {
			t.Fatalf("Reload returned error %v", err)
		}
	}

	if *scans != 2 {
		t.Fatalf("the directory was scanned %d times, want every time without a stamp", *scans)
	}
}

func TestStoreReloadTreatsADirectoryAsSettledOnceItsStampIsOldEnough(t *testing.T) {
	dir := copyFixtureDir(t, fixtureNames(t)[0])
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("Stat returned error %v", err)
	}
	previous := scanClock
	scanClock = func() time.Time { return info.ModTime().Add(directorySettle) }
	t.Cleanup(func() { scanClock = previous })
	scans := countDirectoryScans(t)
	store := openStore(t, dir)

	for range 3 {
		if _, err := store.Reload(); err != nil {
			t.Fatalf("Reload returned error %v", err)
		}
	}

	if *scans != 1 {
		t.Fatalf("the directory was scanned %d times, want once", *scans)
	}
}
