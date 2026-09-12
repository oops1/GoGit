package odb

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
)

func swapMaintain[T any](t *testing.T, target *T, replacement T) {
	t.Helper()
	prev := *target
	*target = replacement
	t.Cleanup(func() { *target = prev })
}

func looseDB(t *testing.T) (*DB, string) {
	t.Helper()
	dir := newObjectsDir(t)
	copyFixtureLoose(t, dir)
	return openDB(t, dir, Options{}), dir
}

func TestLooseObjectsReportEveryLooseFileWithItsSizeAndTime(t *testing.T) {
	db, dir := looseDB(t)

	var got []LooseObject
	for loose, err := range db.LooseObjects() {
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, loose)
	}

	if want := len(looseFixtureObjects(t)); len(got) != want {
		t.Fatalf("loose objects = %d, want %d", len(got), want)
	}
	info, err := os.Stat(filepath.Join(dir, looseName(got[0].ID)))
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Size != info.Size() || !got[0].ModTime.Equal(info.ModTime()) {
		t.Fatalf("first = %+v, file = %d %v", got[0], info.Size(), info.ModTime())
	}
}

func TestLooseObjectsReportFailuresAndStopWhenAsked(t *testing.T) {
	boom := errors.New("boom")

	t.Run("an unreadable objects directory", func(t *testing.T) {
		db, _ := looseDB(t)
		swapMaintain(t, &rootOpen, func(*os.Root, string) (*os.File, error) { return nil, boom })
		for _, err := range db.LooseObjects() {
			if !errors.Is(err, boom) {
				t.Fatalf("err = %v", err)
			}
			break
		}
	})
	t.Run("an unreadable objects directory keeps going when told to", func(t *testing.T) {
		db, _ := looseDB(t)
		swapMaintain(t, &rootOpen, func(*os.Root, string) (*os.File, error) { return nil, boom })
		failures := 0
		for _, err := range db.LooseObjects() {
			if err != nil {
				failures++
			}
		}
		if failures != 1 {
			t.Fatalf("failures = %d", failures)
		}
	})
	t.Run("a file that cannot be examined", func(t *testing.T) {
		db, _ := looseDB(t)
		swapMaintain(t, &rootStat, func(*os.Root, string) (fs.FileInfo, error) { return nil, boom })
		failures := 0
		for _, err := range db.LooseObjects() {
			if !errors.Is(err, boom) {
				t.Fatalf("err = %v", err)
			}
			failures++
		}
		if failures != len(looseFixtureObjects(t)) {
			t.Fatalf("failures = %d", failures)
		}
	})
	t.Run("stopping at the first failed examination", func(t *testing.T) {
		db, _ := looseDB(t)
		swapMaintain(t, &rootStat, func(*os.Root, string) (fs.FileInfo, error) { return nil, boom })
		for range db.LooseObjects() {
			break
		}
	})
	t.Run("stopping at the first object", func(t *testing.T) {
		db, _ := looseDB(t)
		seen := 0
		for range db.LooseObjects() {
			seen++
			break
		}
		if seen != 1 {
			t.Fatalf("seen = %d", seen)
		}
	})
}

func TestRemovingLooseObjectsAndTheirEmptyFanouts(t *testing.T) {
	db, dir := looseDB(t)
	objects := looseFixtureObjects(t)
	for _, obj := range objects {
		if err := db.RemoveLoose(obj.id); err != nil {
			t.Fatal(err)
		}
	}

	if err := db.RemoveEmptyFanouts(); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if len(entry.Name()) == fanoutLength {
			t.Fatalf("fanout %s is still there", entry.Name())
		}
	}
	if err := db.RemoveLoose(objects[0].id); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("removing twice: err = %v", err)
	}
}

func TestAFanoutThatStillHoldsObjectsStays(t *testing.T) {
	db, dir := looseDB(t)
	id := looseFixtureObjects(t)[0].id
	if err := os.MkdirAll(filepath.Join(dir, "info"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "ab"), []byte("a file, not a fanout"))

	if err := db.RemoveEmptyFanouts(); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(dir, looseName(id))); err != nil {
		t.Fatalf("object gone: %v", err)
	}
}

func TestRemovingEmptyFanoutsReportsFailures(t *testing.T) {
	boom := errors.New("boom")
	t.Run("listing the objects directory", func(t *testing.T) {
		db, _ := looseDB(t)
		swapMaintain(t, &rootOpen, func(*os.Root, string) (*os.File, error) { return nil, boom })
		if err := db.RemoveEmptyFanouts(); !errors.Is(err, boom) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("listing a fanout", func(t *testing.T) {
		db, _ := looseDB(t)
		prev := rootOpen
		swapMaintain(t, &rootOpen, func(root *os.Root, name string) (*os.File, error) {
			if name != "." {
				return nil, boom
			}
			return prev(root, name)
		})
		if err := db.RemoveEmptyFanouts(); !errors.Is(err, boom) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("removing a fanout", func(t *testing.T) {
		db, dir := looseDB(t)
		if err := os.Mkdir(filepath.Join(dir, "ff"), 0o755); err != nil {
			t.Fatal(err)
		}
		swapMaintain(t, &rootRemove, func(*os.Root, string) error { return boom })
		if err := db.RemoveEmptyFanouts(); !errors.Is(err, boom) {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestTempFilesFindLeftoversOfInterruptedWrites(t *testing.T) {
	db, dir := looseDB(t)
	writeFile(t, filepath.Join(dir, tempPrefix+"abc"), []byte("12345"))
	writeFile(t, filepath.Join(dir, "unrelated"), []byte("x"))
	if err := os.Mkdir(filepath.Join(dir, tempPrefix+"dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, packDirName, packTempPrefix+"pack_1"), []byte("12"))
	writeFile(t, filepath.Join(dir, packDirName, incomingPrefix+"2.pack"), []byte("123"))
	writeFile(t, filepath.Join(dir, packDirName, "pack-kept.keep"), []byte("k"))

	temps, err := db.TempFiles()
	if err != nil {
		t.Fatal(err)
	}

	var paths []string
	var total int64
	for _, temp := range temps {
		paths = append(paths, temp.Path)
		total += temp.Size
	}
	slices.Sort(paths)
	want := []string{"pack/" + incomingPrefix + "2.pack", "pack/" + packTempPrefix + "pack_1", tempPrefix + "abc"}
	if !slices.Equal(paths, want) || total != 10 {
		t.Fatalf("temps = %v (%d bytes), want %v", paths, total, want)
	}

	for _, temp := range temps {
		if err := db.RemoveTemp(temp.Path); err != nil {
			t.Fatal(err)
		}
	}
	if left, _ := db.TempFiles(); len(left) != 0 {
		t.Fatalf("left = %v", left)
	}
	if err := db.RemoveTemp(tempPrefix + "abc"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("removing twice: err = %v", err)
	}
}

func TestTempFilesWithoutAPackDirectory(t *testing.T) {
	dir := newObjectsDir(t)
	_ = os.RemoveAll(filepath.Join(dir, packDirName))
	db := openDB(t, dir, Options{})

	if temps, err := db.TempFiles(); err != nil || len(temps) != 0 {
		t.Fatalf("temps = %v, err = %v", temps, err)
	}
}

func TestTempFilesReportFailures(t *testing.T) {
	boom := errors.New("boom")
	t.Run("listing a directory", func(t *testing.T) {
		db, _ := looseDB(t)
		swapMaintain(t, &rootOpen, func(*os.Root, string) (*os.File, error) { return nil, boom })
		if _, err := db.TempFiles(); !errors.Is(err, boom) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("examining a leftover", func(t *testing.T) {
		db, dir := looseDB(t)
		writeFile(t, filepath.Join(dir, tempPrefix+"abc"), []byte("1"))
		swapMaintain(t, &rootStat, func(*os.Root, string) (fs.FileInfo, error) { return nil, boom })
		if _, err := db.TempFiles(); !errors.Is(err, boom) {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestPacksDescribeEachPackfileWithItsIndex(t *testing.T) {
	dir := newObjectsDir(t)
	copyFixturePacks(t, dir)
	db := openDB(t, dir, Options{})

	packs, err := db.Packs()
	if err != nil {
		t.Fatal(err)
	}
	if len(packs) == 0 {
		t.Fatal("no packs reported")
	}
	for _, p := range packs {
		packInfo, err := os.Stat(filepath.Join(dir, packDirName, p.Name+".pack"))
		if err != nil {
			t.Fatal(err)
		}
		indexInfo, err := os.Stat(filepath.Join(dir, packDirName, p.Name+packIndexSuffix))
		if err != nil {
			t.Fatal(err)
		}
		if p.Size != packInfo.Size()+indexInfo.Size() || p.Objects == 0 || strings.Contains(p.Name, ".") {
			t.Fatalf("pack = %+v", p)
		}
	}
	packed, err := db.Packed(packFixtureObjects(t)[0].id)
	if err != nil || !packed {
		t.Fatalf("packed = %v, err = %v", packed, err)
	}
	if packed, err := db.Packed(hash.Zero); err != nil || packed {
		t.Fatalf("zero packed = %v, err = %v", packed, err)
	}
}

func TestPacksAndPackedWithoutAnyPackfile(t *testing.T) {
	dir := newObjectsDir(t)
	_ = os.RemoveAll(filepath.Join(dir, packDirName))
	db := openDB(t, dir, Options{})

	if packs, err := db.Packs(); err != nil || packs != nil {
		t.Fatalf("packs = %v, err = %v", packs, err)
	}
	if packed, err := db.Packed(hash.Zero); err != nil || packed {
		t.Fatalf("packed = %v, err = %v", packed, err)
	}
}

func TestPacksReportAnIndexThatCannotBeExamined(t *testing.T) {
	dir := newObjectsDir(t)
	copyFixturePacks(t, dir)
	db := openDB(t, dir, Options{})
	boom := errors.New("boom")
	swapMaintain(t, &rootStat, func(*os.Root, string) (fs.FileInfo, error) { return nil, boom })

	if _, err := db.Packs(); !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
}
