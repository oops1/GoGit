package odb

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/pack"
)

func TestVerifyLooseAcceptsIntactObjects(t *testing.T) {
	db, _ := looseDB(t)

	for _, obj := range looseFixtureObjects(t) {
		if err := db.VerifyLoose(obj.id); err != nil {
			t.Fatalf("VerifyLoose(%s) returned error %v", obj.id, err)
		}
	}
}

func TestVerifyLooseCatchesContentThatDoesNotMatchItsName(t *testing.T) {
	db, dir := looseDB(t)
	id := looseFixtureObjects(t)[0].id
	path := filepath.Join(dir, looseName(id))
	_ = os.Chmod(path, 0o644)
	writeFile(t, path, compressLoose(object.TypeBlob, []byte("something else entirely\n")))

	if err := db.VerifyLoose(id); !errors.Is(err, object.ErrCorrupt) {
		t.Fatalf("err = %v, want object.ErrCorrupt", err)
	}
}

func TestVerifyLooseReportsMissingAndUnreadableObjects(t *testing.T) {
	db, _ := looseDB(t)
	if err := db.VerifyLoose(hash.SumSHA1("blob", []byte("absent"))); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	boom := errors.New("boom")
	swapMaintain(t, &rootOpen, func(*os.Root, string) (*os.File, error) { return nil, boom })
	if err := db.VerifyLoose(looseFixtureObjects(t)[0].id); err == nil {
		t.Fatal("an unreadable object passed verification")
	}
}

func fixturePackDB(t *testing.T) (*DB, string, string) {
	t.Helper()
	dir := newObjectsDir(t)
	copyFixturePacks(t, dir)
	db := openDB(t, dir, Options{})
	packs, err := db.Packs()
	if err != nil || len(packs) == 0 {
		t.Fatalf("packs = %v, err = %v", packs, err)
	}
	return db, dir, packs[0].Name
}

func TestVerifyPackFindsNothingWrongWithAnIntactPack(t *testing.T) {
	db, _, name := fixturePackDB(t)

	for id, err := range db.VerifyPack(t.Context(), name) {
		t.Fatalf("problem at %s: %v", id, err)
	}
}

func TestVerifyPackReportsAnUnknownPack(t *testing.T) {
	db, _, _ := fixturePackDB(t)
	dir := newObjectsDir(t)
	_ = os.RemoveAll(filepath.Join(dir, packDirName))
	empty := openDB(t, dir, Options{})

	for _, candidate := range []*DB{db, empty} {
		problems := 0
		for _, err := range candidate.VerifyPack(t.Context(), "pack-unknown") {
			if !errors.Is(err, ErrNotFound) {
				t.Fatalf("err = %v", err)
			}
			problems++
		}
		if problems != 1 {
			t.Fatalf("problems = %d", problems)
		}
	}
}

func corruptFile(t *testing.T, path string, at func(size int) int) {
	t.Helper()
	_ = os.Chmod(path, 0o644)
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	offset := int64(at(int(info.Size())))
	var one [1]byte
	if _, err := file.ReadAt(one[:], offset); err != nil {
		t.Fatal(err)
	}
	one[0] ^= 0xFF
	if _, err := file.WriteAt(one[:], offset); err != nil {
		t.Fatal(err)
	}
}

func corruptedPackDB(t *testing.T, suffix string, at func(size int) int) (*DB, string) {
	t.Helper()
	dir := newObjectsDir(t)
	copyFixturePacks(t, dir)
	probe := openDB(t, dir, Options{})
	packs, err := probe.Packs()
	if err != nil {
		t.Fatal(err)
	}
	name := packs[0].Name
	_ = probe.Close()
	corruptFile(t, filepath.Join(dir, packDirName, name+suffix), at)
	return openDB(t, dir, Options{}), name
}

func TestVerifyPackReportsADamagedPackfileAndItsObjects(t *testing.T) {
	db, name := corruptedPackDB(t, ".pack", func(size int) int { return size / 2 })

	fileLevel, objectLevel := 0, 0
	for id, err := range db.VerifyPack(t.Context(), name) {
		if err == nil {
			t.Fatal("a problem without an error")
		}
		if id.IsZero() {
			fileLevel++
		} else {
			objectLevel++
		}
	}

	if fileLevel == 0 || objectLevel == 0 {
		t.Fatalf("file problems = %d, object problems = %d", fileLevel, objectLevel)
	}
}

func TestVerifyPackReportsADamagedIndex(t *testing.T) {
	db, name := corruptedPackDB(t, packIndexSuffix, func(int) int { return 8 + 256*4 + 1 })

	var first error
	for _, err := range db.VerifyPack(t.Context(), name) {
		first = err
		break
	}

	if first == nil {
		t.Fatal("a damaged index passed verification")
	}
}

func TestVerifyPackCatchesAnIndexThatNamesTheWrongObject(t *testing.T) {
	source, _ := looseDB(t)
	held := looseFixtureObjects(t)[0].id
	var packData bytes.Buffer
	written, err := pack.WritePack(t.Context(), &packData, source, []hash.ObjectID{held}, pack.WriteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	lie := hash.SumSHA1("blob", []byte("a name the pack does not hold"))
	written.Entries[0].ID = lie
	var indexData bytes.Buffer
	if err := pack.WriteIndex(&indexData, written.Entries, written.Checksum); err != nil {
		t.Fatal(err)
	}
	dir := newObjectsDir(t)
	name := "pack-" + written.Checksum.String()
	writeFile(t, filepath.Join(dir, packDirName, name+".pack"), packData.Bytes())
	writeFile(t, filepath.Join(dir, packDirName, name+packIndexSuffix), indexData.Bytes())
	db := openDB(t, dir, Options{})

	var problems []hash.ObjectID
	for id, err := range db.VerifyPack(t.Context(), name) {
		if !errors.Is(err, ErrCorrupt) {
			t.Fatalf("err = %v, want ErrCorrupt", err)
		}
		problems = append(problems, id)
	}

	if !slices.Equal(problems, []hash.ObjectID{lie}) {
		t.Fatalf("problems = %v, want only the misnamed object", problems)
	}
}

func TestVerifyPackStopsWhenTheCallerHasSeenEnough(t *testing.T) {
	t.Run("after the packfile checksum", func(t *testing.T) {
		db, name := corruptedPackDB(t, ".pack", func(size int) int { return size - hash.Size - 1 })
		seen := 0
		for range db.VerifyPack(t.Context(), name) {
			seen++
			break
		}
		if seen != 1 {
			t.Fatalf("seen = %d", seen)
		}
	})
	t.Run("after the first damaged object", func(t *testing.T) {
		db, name := corruptedPackDB(t, ".pack", func(size int) int { return size / 2 })
		objects := 0
		for id := range db.VerifyPack(t.Context(), name) {
			if id.IsZero() {
				continue
			}
			objects++
			break
		}
		if objects != 1 {
			t.Fatalf("objects = %d", objects)
		}
	})
	t.Run("when the context is cancelled", func(t *testing.T) {
		db, name := corruptedPackDB(t, ".pack", func(size int) int { return size / 2 })
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		for id, err := range db.VerifyPack(ctx, name) {
			if !id.IsZero() {
				t.Fatalf("an object was checked after cancellation: %s %v", id, err)
			}
		}
	})
}

func TestVerifyLooseWrapsTheFailureWithTheObjectAndDirectory(t *testing.T) {
	db, dir := looseDB(t)
	id := looseFixtureObjects(t)[0].id
	path := filepath.Join(dir, looseName(id))
	_ = os.Chmod(path, 0o644)
	writeFile(t, path, []byte("not zlib"))

	err := db.VerifyLoose(id)
	if err == nil || !strings.Contains(err.Error(), id.String()) {
		t.Fatalf("err = %v", err)
	}
}
