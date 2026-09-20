package odb

import (
	"errors"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/pack"
)

func TestGetAsksForAMissingObjectOnceAndReadsWhatArrives(t *testing.T) {
	dir := newObjectsDir(t)
	want := []byte("fetched on demand")
	id, err := hash.Sum(hash.SHA1, "blob", want)
	if err != nil {
		t.Fatalf("hash.Sum returned error %v", err)
	}
	calls := 0
	writer := openDB(t, dir, Options{})
	db := openDB(t, dir, Options{Missing: func(asked hash.ObjectID) error {
		calls++
		if asked != id {
			t.Fatalf("Missing was asked for %s, want %s", asked, id)
		}
		_, err := writer.Put(object.TypeBlob, want)
		return err
	}})
	kind, data, err := db.Get(id)
	if err != nil {
		t.Fatalf("Get returned error %v", err)
	}
	if kind != object.TypeBlob || string(data) != string(want) {
		t.Fatalf("Get = %s %q, want a blob holding %q", kind, data, want)
	}
	if calls != 1 {
		t.Fatalf("Missing was called %d times, want once", calls)
	}
}

func TestInfoAsksForAMissingObject(t *testing.T) {
	dir := newObjectsDir(t)
	want := []byte("sized on demand")
	id, err := hash.Sum(hash.SHA1, "blob", want)
	if err != nil {
		t.Fatalf("hash.Sum returned error %v", err)
	}
	writer := openDB(t, dir, Options{})
	db := openDB(t, dir, Options{Missing: func(hash.ObjectID) error {
		_, err := writer.Put(object.TypeBlob, want)
		return err
	}})
	kind, size, err := db.Info(id)
	if err != nil {
		t.Fatalf("Info returned error %v", err)
	}
	if kind != object.TypeBlob || size != int64(len(want)) {
		t.Fatalf("Info = %s %d, want a blob of %d bytes", kind, size, len(want))
	}
}

func TestLookupsReportWhatTheMissingObjectHookFailedWith(t *testing.T) {
	dir := newObjectsDir(t)
	failure := errors.New("the promisor remote is unreachable")
	db := openDB(t, dir, Options{Missing: func(hash.ObjectID) error { return failure }})
	id := hash.SumSHA1("blob", []byte("never stored anywhere"))
	if _, _, err := db.Get(id); !errors.Is(err, failure) {
		t.Fatalf("Get returned %v, want %v", err, failure)
	}
	if _, _, err := db.Info(id); !errors.Is(err, failure) {
		t.Fatalf("Info returned %v, want %v", err, failure)
	}
}

func TestLookupsReportAFailedReloadBeforeAskingForTheObject(t *testing.T) {
	objects := newObjectsDir(t)
	copyFixturePacks(t, objects)
	db := openDB(t, objects, Options{Missing: func(hash.ObjectID) error {
		t.Error("the missing object hook ran although the reload failed")
		return nil
	}})
	swapMaintain(t, &packReload, func(*pack.Store) (bool, error) { return false, errInjected })
	id := hash.SumSHA1("blob", []byte("never stored anywhere"))
	if _, _, err := db.Get(id); !errors.Is(err, errInjected) {
		t.Fatalf("Get returned %v, want %v", err, errInjected)
	}
	if _, _, err := db.Info(id); !errors.Is(err, errInjected) {
		t.Fatalf("Info returned %v, want %v", err, errInjected)
	}
}

func TestLookupsStayNotFoundWhenTheMissingObjectHookBringsNothing(t *testing.T) {
	dir := newObjectsDir(t)
	db := openDB(t, dir, Options{Missing: func(hash.ObjectID) error { return nil }})
	id := hash.SumSHA1("blob", []byte("never stored anywhere"))
	if _, _, err := db.Get(id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get returned %v, want ErrNotFound", err)
	}
	if _, _, err := db.Info(id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Info returned %v, want ErrNotFound", err)
	}
}

func TestLookupsSkipTheMissingObjectHookAfterAReloadFindsTheObject(t *testing.T) {
	dir := newObjectsDir(t)
	forGet := openDB(t, dir, Options{Missing: func(hash.ObjectID) error {
		t.Error("the missing object hook ran although a reload found the object")
		return nil
	}})
	forInfo := openDB(t, dir, Options{Missing: func(hash.ObjectID) error {
		t.Error("the missing object hook ran although a reload found the object")
		return nil
	}})
	packed := packFixtureObjects(t)[0]
	copyFixturePacks(t, dir)

	if _, _, err := forGet.Get(packed.id); err != nil {
		t.Fatalf("Get returned error %v", err)
	}
	if _, size, err := forInfo.Info(packed.id); err != nil || size != packed.size {
		t.Fatalf("Info gave (%d, %v), want %d", size, err, packed.size)
	}
}
