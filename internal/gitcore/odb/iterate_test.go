package odb

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
)

func collectIDs(t testing.TB, sequence func(func(hash.ObjectID, error) bool)) []hash.ObjectID {
	t.Helper()
	var found []hash.ObjectID
	for id, err := range sequence {
		if err != nil {
			t.Fatalf("the iterator returned error %v", err)
		}
		found = append(found, id)
	}
	return found
}

func TestLooseListsStoredObjectsInOrder(t *testing.T) {
	objects := newObjectsDir(t)
	copyFixtureLoose(t, objects)
	db := openDB(t, objects, Options{})
	found := collectIDs(t, db.Loose())
	want := make([]hash.ObjectID, 0, len(looseFixtureObjects(t)))
	for _, item := range looseFixtureObjects(t) {
		want = append(want, item.id)
	}
	slices.SortFunc(want, func(a, b hash.ObjectID) int { return a.Compare(b) })
	if !slices.Equal(found, want) {
		t.Fatalf("Loose listed %d objects, want %d", len(found), len(want))
	}
}

func TestLooseSkipsEntriesThatAreNotObjects(t *testing.T) {
	objects := newObjectsDir(t)
	id := storeBlob(t, objects, []byte("the only object\n"))
	writeFile(t, filepath.Join(objects, "info", "alternates"), nil)
	writeFile(t, filepath.Join(objects, "packed-refs"), nil)
	writeFile(t, filepath.Join(objects, fanoutOf(id), "not-an-object"), nil)
	if err := os.MkdirAll(filepath.Join(objects, fanoutOf(id), "0123456789012345678901234567890123456a"), 0o755); err != nil {
		t.Fatalf("MkdirAll returned error %v", err)
	}
	db := openDB(t, objects, Options{})
	found := collectIDs(t, db.Loose())
	if !slices.Equal(found, []hash.ObjectID{id}) {
		t.Fatalf("Loose listed %v, want only %s", found, id)
	}
}

func TestLooseReportsDirectoryFailures(t *testing.T) {
	objects := newObjectsDir(t)
	id := storeBlob(t, objects, []byte("unreadable fanout\n"))
	db := openDB(t, objects, Options{})
	swapRootOpen(t, func(_ *os.Root, name string) bool { return name == fanoutOf(id) }, errInjected)
	var failures int
	for _, err := range db.Loose() {
		if err != nil {
			failures++
		}
	}
	if failures != 1 {
		t.Fatalf("Loose reported %d failures, want 1", failures)
	}
}

func TestLooseReportsRootFailures(t *testing.T) {
	db := openDB(t, newObjectsDir(t), Options{})
	swapRootOpen(t, always, errInjected)
	for _, err := range db.Loose() {
		if !errors.Is(err, errInjected) {
			t.Fatalf("Loose returned %v, want %v", err, errInjected)
		}
	}
}

func TestLooseStopsWhenTheConsumerStops(t *testing.T) {
	objects := newObjectsDir(t)
	copyFixtureLoose(t, objects)
	db := openDB(t, objects, Options{})
	seen := 0
	for range db.Loose() {
		seen++
		break
	}
	if seen != 1 {
		t.Fatalf("Loose yielded %d objects after the consumer stopped", seen)
	}
}

func TestAllMergesLooseAndPackedObjects(t *testing.T) {
	db := newFixtureDB(t)
	found := collectIDs(t, db.All())
	unique := make(map[hash.ObjectID]struct{}, len(found))
	for _, id := range found {
		if _, repeated := unique[id]; repeated {
			t.Fatalf("All listed %s twice", id)
		}
		unique[id] = struct{}{}
	}
	for _, item := range slices.Concat(looseFixtureObjects(t), packFixtureObjects(t)) {
		if _, ok := unique[item.id]; !ok {
			t.Fatalf("All skipped %s", item.id)
		}
	}
}

func TestAllListsAnObjectStoredTwiceOnlyOnce(t *testing.T) {
	objects := newObjectsDir(t)
	copyFixturePacks(t, objects)
	db := openDB(t, objects, Options{})
	packed := packFixtureObjects(t)[0]
	kind, data, err := db.Get(packed.id)
	if err != nil {
		t.Fatalf("Get returned error %v", err)
	}
	writeLooseFile(t, objects, packed.id, compressLoose(kind, data))
	seen := 0
	for id := range db.All() {
		if id == packed.id {
			seen++
		}
	}
	if seen != 1 {
		t.Fatalf("All listed %s %d times", packed.id, seen)
	}
}

func TestAllCoversAlternates(t *testing.T) {
	root := t.TempDir()
	main := makeObjectsDir(t, root, "main")
	shared := makeObjectsDir(t, root, "shared")
	borrowed := storeBlob(t, shared, []byte("borrowed object\n"))
	writeAlternates(t, main, shared)
	own := storeBlob(t, main, []byte("own object\n"))
	db := openDB(t, main, Options{})
	found := collectIDs(t, db.All())
	for _, id := range []hash.ObjectID{own, borrowed} {
		if !slices.Contains(found, id) {
			t.Fatalf("All skipped %s", id)
		}
	}
}

func TestAllStopsWhenTheConsumerStops(t *testing.T) {
	root := t.TempDir()
	main := makeObjectsDir(t, root, "main")
	shared := makeObjectsDir(t, root, "shared")
	copyFixturePacks(t, main)
	storeBlob(t, shared, []byte("borrowed object\n"))
	writeAlternates(t, main, shared)
	storeBlob(t, main, []byte("own object\n"))
	db := openDB(t, main, Options{})
	total := len(collectIDs(t, db.All()))
	for limit := 1; limit <= total; limit++ {
		seen := 0
		for range db.All() {
			seen++
			if seen == limit {
				break
			}
		}
		if seen != limit {
			t.Fatalf("All yielded %d objects for a limit of %d", seen, limit)
		}
	}
}

func TestAllStopsOnTheFirstReportedFailure(t *testing.T) {
	db := openDB(t, newObjectsDir(t), Options{})
	swapRootOpen(t, always, errInjected)
	seen := 0
	for _, err := range db.All() {
		if !errors.Is(err, errInjected) {
			t.Fatalf("All returned %v, want %v", err, errInjected)
		}
		seen++
		break
	}
	if seen != 1 {
		t.Fatalf("All yielded %d entries", seen)
	}
}

func TestAllContinuesAfterAReportedFailure(t *testing.T) {
	objects := newObjectsDir(t)
	copyFixturePacks(t, objects)
	db := openDB(t, objects, Options{})
	swapRootOpen(t, func(_ *os.Root, name string) bool { return name == "." }, errInjected)
	failures, objectsSeen := 0, 0
	for _, err := range db.All() {
		if err != nil {
			failures++
			continue
		}
		objectsSeen++
	}
	if failures != 1 || objectsSeen == 0 {
		t.Fatalf("All reported %d failures and %d objects", failures, objectsSeen)
	}
}

func TestLooseFindsObjectsWrittenByPut(t *testing.T) {
	db := openDB(t, newObjectsDir(t), Options{})
	id, err := db.Put(object.TypeBlob, []byte("written object\n"))
	if err != nil {
		t.Fatalf("Put returned error %v", err)
	}
	if !slices.Equal(collectIDs(t, db.Loose()), []hash.ObjectID{id}) {
		t.Fatal("Loose did not list the object written by Put")
	}
}
