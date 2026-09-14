package odb

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/oops1/gogit/internal/gitcore/object"
)

var longAgo = time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)

func ageObjectFile(t *testing.T, path string) {
	t.Helper()
	if err := os.Chtimes(path, longAgo, longAgo); err != nil {
		t.Fatalf("Chtimes returned error %v", err)
	}
}

func requireFreshFile(t *testing.T, path string, since time.Time) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat returned error %v", err)
	}
	if info.ModTime().Before(since) {
		t.Fatalf("%s is dated %v, want no earlier than %v", path, info.ModTime(), since)
	}
}

func requireNoLooseCopy(t *testing.T, objects string, name string) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(objects, name)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a loose copy %s was written: %v", name, err)
	}
}

func closePackIndexes(t *testing.T, db *DB) {
	t.Helper()
	for _, file := range db.store().Files() {
		if err := file.Index.Close(); err != nil {
			t.Fatalf("Close returned error %v", err)
		}
	}
}

func TestPutFreshensAnOldLooseCopy(t *testing.T) {
	objects := newObjectsDir(t)
	db := openDB(t, objects, Options{})
	payload := []byte("written long ago and again today\n")
	id, err := db.Put(object.TypeBlob, payload)
	if err != nil {
		t.Fatalf("Put returned error %v", err)
	}
	path := filepath.Join(objects, looseName(id))
	ageObjectFile(t, path)
	since := time.Now().Add(-time.Second)

	if again, err := db.Put(object.TypeBlob, payload); err != nil || again != id {
		t.Fatalf("the second Put gave (%s, %v)", again, err)
	}

	requireFreshFile(t, path, since)
}

func TestWriterFreshensAnOldLooseCopy(t *testing.T) {
	objects := newObjectsDir(t)
	db := openDB(t, objects, Options{})
	payload := []byte("streamed long ago and again today\n")
	id, err := db.Put(object.TypeBlob, payload)
	if err != nil {
		t.Fatalf("Put returned error %v", err)
	}
	path := filepath.Join(objects, looseName(id))
	ageObjectFile(t, path)
	since := time.Now().Add(-time.Second)
	writer, err := db.Writer(object.TypeBlob, int64(len(payload)))
	if err != nil {
		t.Fatalf("Writer returned error %v", err)
	}
	if _, err := writer.Write(payload); err != nil {
		t.Fatalf("Write returned error %v", err)
	}

	if err := writer.Close(); err != nil || writer.ID() != id {
		t.Fatalf("Close gave (%s, %v)", writer.ID(), err)
	}

	requireFreshFile(t, path, since)
	if total := countFiles(t, objects); total != 1 {
		t.Fatalf("the object directory holds %d files, want 1", total)
	}
}

func TestPutFreshensThePackHoldingTheObject(t *testing.T) {
	objects := newObjectsDir(t)
	copyFixturePacks(t, objects)
	db := openDB(t, objects, Options{})
	id := packFixtureObjects(t)[0].id
	kind, data, err := db.Get(id)
	if err != nil {
		t.Fatalf("Get returned error %v", err)
	}
	name, ok, err := db.store().Holder(id)
	if !ok || err != nil {
		t.Fatalf("Holder returned (%v, %v)", ok, err)
	}
	packPath := filepath.Join(objects, packDirName, name+packDataSuffix)
	ageObjectFile(t, packPath)
	since := time.Now().Add(-time.Second)

	if got, err := db.Put(kind, data); err != nil || got != id {
		t.Fatalf("Put gave (%s, %v)", got, err)
	}

	requireFreshFile(t, packPath, since)
	requireNoLooseCopy(t, objects, looseName(id))
}

func TestPutWritesALooseCopyWhenThePackCannotBeFreshened(t *testing.T) {
	objects := newObjectsDir(t)
	copyFixturePacks(t, objects)
	db := openDB(t, objects, Options{})
	id := packFixtureObjects(t)[0].id
	kind, data, err := db.Get(id)
	if err != nil {
		t.Fatalf("Get returned error %v", err)
	}
	swapMaintain(t, &rootChtimes, func(root *os.Root, name string, atime, mtime time.Time) error {
		if strings.HasSuffix(name, packDataSuffix) {
			return errInjected
		}
		return root.Chtimes(name, atime, mtime)
	})

	if got, err := db.Put(kind, data); err != nil || got != id {
		t.Fatalf("Put gave (%s, %v)", got, err)
	}

	if _, err := os.Stat(filepath.Join(objects, looseName(id))); err != nil {
		t.Fatalf("no loose copy was written for a pack that stayed old: %v", err)
	}
}

func TestPutFreshensAnObjectKeptInAnAlternate(t *testing.T) {
	root := t.TempDir()
	main := makeObjectsDir(t, root, "main")
	shared := makeObjectsDir(t, root, "shared")
	payload := []byte("shared long ago\n")
	id := storeBlob(t, shared, payload)
	sharedPath := filepath.Join(shared, looseName(id))
	ageObjectFile(t, sharedPath)
	writeAlternates(t, main, shared)
	db := openDB(t, main, Options{})
	since := time.Now().Add(-time.Second)

	if got, err := db.Put(object.TypeBlob, payload); err != nil || got != id {
		t.Fatalf("Put gave (%s, %v)", got, err)
	}

	requireFreshFile(t, sharedPath, since)
	requireNoLooseCopy(t, main, looseName(id))
}

func TestPutReportsAPackLookupFailureInAnAlternate(t *testing.T) {
	root := t.TempDir()
	main := makeObjectsDir(t, root, "main")
	shared := makeObjectsDir(t, root, "shared")
	copyFixturePacks(t, shared)
	writeAlternates(t, main, shared)
	db := openDB(t, main, Options{})
	kind, data, err := db.Get(packFixtureObjects(t)[0].id)
	if err != nil {
		t.Fatalf("Get returned error %v", err)
	}
	closePackIndexes(t, db.Alternates()[0])

	if _, err := db.Put(kind, data); err == nil {
		t.Fatal("Put ignored a pack index of an alternate it could not read")
	}
}

func TestContainsLooksWithoutReloadingPacks(t *testing.T) {
	objects := newObjectsDir(t)
	db := openDB(t, objects, Options{})
	id, err := db.Put(object.TypeBlob, []byte("contained\n"))
	if err != nil {
		t.Fatalf("Put returned error %v", err)
	}
	if found, err := db.Contains(id); !found || err != nil {
		t.Fatalf("Contains of a loose object returned (%v, %v)", found, err)
	}
	if _, _, err := db.Get(id); err != nil {
		t.Fatalf("Get returned error %v", err)
	}
	if err := db.RemoveLoose(id); err != nil {
		t.Fatalf("RemoveLoose returned error %v", err)
	}
	if found, err := db.Contains(id); !found || err != nil {
		t.Fatalf("Contains of a cached object returned (%v, %v)", found, err)
	}

	copyFixturePacks(t, objects)
	packed := packFixtureObjects(t)[0].id

	if found, err := db.Contains(packed); found || err != nil {
		t.Fatalf("Contains returned (%v, %v) for a pack it was never told about", found, err)
	}
	if found, err := db.Has(packed); !found || err != nil {
		t.Fatalf("Has returned (%v, %v) after the pack appeared", found, err)
	}
}
