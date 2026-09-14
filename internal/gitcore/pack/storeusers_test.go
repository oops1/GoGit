package pack

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestAPackfileInUseStaysOpenUntilItsLastReaderIsDone(t *testing.T) {
	store, err := Open(copyFixtureDir(t, fixtureNames(t)[0]))
	if err != nil {
		t.Fatalf("Open returned error %v", err)
	}
	id := objectTypesFirstID(t)
	var held *PackFile
	for file := range store.Acquire() {
		held = file
		if err := store.Close(); err != nil {
			t.Fatalf("Close returned error %v", err)
		}
		offset, ok, err := file.Index.Lookup(id)
		if !ok || err != nil {
			t.Fatalf("Lookup after Close returned (%v, %v)", ok, err)
		}
		if _, _, err := file.Pack.ObjectAt(offset); err != nil {
			t.Fatalf("ObjectAt after Close returned error %v", err)
		}
	}
	if err := held.Pack.Close(); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("closing the packfile again returned %v, want %v", err, os.ErrClosed)
	}
	if found, err := store.Contains(id); found || err != nil {
		t.Fatalf("Contains after Close returned (%v, %v), want (false, nil)", found, err)
	}
}

func TestARetiredPackfileClosesOnceWhenEveryUserLeaves(t *testing.T) {
	store := openStore(t, copyFixtureDir(t, fixtureNames(t)[0]))
	file := store.Files()[0]
	file.acquire()
	file.acquire()
	if err := file.retire(); err != nil {
		t.Fatalf("retire returned error %v", err)
	}
	file.release()
	if _, err := file.Index.EntryAt(0); err != nil {
		t.Fatalf("the index closed while a user remained: %v", err)
	}
	file.release()
	if err := file.close(); err != nil {
		t.Fatalf("a second close reported %v instead of the first result", err)
	}
	if _, err := file.Index.EntryAt(0); err == nil {
		t.Fatal("the index stayed open after its last user left")
	}
}

func TestStoreHolderNamesThePackfileWithTheObject(t *testing.T) {
	store := openFixtureStore(t)
	name, ok, err := store.Holder(objectTypesFirstID(t))
	if !ok || err != nil || !slices.Contains(fixtureNames(t), name) {
		t.Fatalf("Holder = (%q, %v, %v), want one of %v", name, ok, err, fixtureNames(t))
	}
	if name, ok, err := store.Holder(idOfByte(0xfe)); name != "" || ok || err != nil {
		t.Fatalf("Holder of an unknown object = (%q, %v, %v)", name, ok, err)
	}
}

func TestStoreSkipsAPackfileThatVanishesWhileScanning(t *testing.T) {
	names := fixtureNames(t)
	dir := copyFixtureDir(t, names...)
	original := statPath
	statPath = func(path string) (os.FileInfo, error) {
		if filepath.Base(path) == names[1]+packSuffix {
			return nil, os.ErrNotExist
		}
		return original(path)
	}
	t.Cleanup(func() { statPath = original })

	files := openStore(t, dir).Files()

	if len(files) != 1 || files[0].Name != names[0] {
		t.Fatalf("the store lists %d packfiles, want only %s", len(files), names[0])
	}
}

func TestStoreReadersSurviveConcurrentReloads(t *testing.T) {
	names := fixtureNames(t)
	dir := copyFixtureDir(t, names...)
	store := openStore(t, dir, WithCache(NewCache(1)))
	ids := slices.Collect(store.Objects())
	failures := make(chan error, 8)
	var stop atomic.Bool
	var readers sync.WaitGroup
	for reader := range 4 {
		readers.Go(func() {
			for round := 0; !stop.Load() || round < len(ids); round++ {
				id := ids[(round+reader)%len(ids)]
				if _, _, ok, err := store.Get(id); !ok || err != nil {
					failures <- fmt.Errorf("Get(%s) returned (%v, %v)", id, ok, err)
					return
				}
			}
		})
	}
	for round := range 40 {
		stamp := time.Now().Add(time.Duration(round+1) * time.Second)
		for _, name := range names {
			if err := os.Chtimes(filepath.Join(dir, name+packSuffix), stamp, stamp); err != nil {
				t.Fatalf("Chtimes of a packfile in use returned error %v", err)
			}
		}
		if changed, err := store.Reload(); !changed || err != nil {
			t.Fatalf("Reload returned (%v, %v), want (true, nil)", changed, err)
		}
	}
	stop.Store(true)
	readers.Wait()
	close(failures)
	for err := range failures {
		t.Fatal(err)
	}
}
