package odb

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/pack"
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

func TestPackedSinceListsObjectsOfPacksNoOlderThanTheMoment(t *testing.T) {
	dir := newObjectsDir(t)
	copyFixturePacks(t, dir)
	db := openDB(t, dir, Options{})
	packs, err := db.Packs()
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, p := range packs {
		total += p.Objects
	}

	recent := 0
	for range db.PackedSince(time.Now().Add(-time.Hour)) {
		recent++
	}
	future := 0
	for range db.PackedSince(time.Now().Add(time.Hour)) {
		future++
	}
	stopped := 0
	for range db.PackedSince(time.Time{}) {
		stopped++
		break
	}

	if recent != total || future != 0 || stopped != 1 {
		t.Fatalf("recent = %d of %d, future = %d, stopped = %d", recent, total, future, stopped)
	}
}

func TestPackedSinceWithoutAnyPackfile(t *testing.T) {
	dir := newObjectsDir(t)
	_ = os.RemoveAll(filepath.Join(dir, packDirName))
	db := openDB(t, dir, Options{})

	for id := range db.PackedSince(time.Time{}) {
		t.Fatalf("unexpected %s", id)
	}
}

func TestPacksMarkPackfilesKeptByAKeepFile(t *testing.T) {
	dir := newObjectsDir(t)
	copyFixturePacks(t, dir)
	db := openDB(t, dir, Options{})
	packs, err := db.Packs()
	if err != nil || len(packs) == 0 {
		t.Fatalf("packs = %v, err = %v", packs, err)
	}
	writeFile(t, filepath.Join(dir, packDirName, packs[0].Name+keepSuffix), []byte("fetching"))

	marked, err := db.Packs()
	if err != nil {
		t.Fatal(err)
	}

	for _, p := range marked {
		if p.Keep != (p.Name == packs[0].Name) {
			t.Fatalf("pack %s keep = %v", p.Name, p.Keep)
		}
	}
}

func TestPacksReportAKeepMarkerThatCannotBeExamined(t *testing.T) {
	dir := newObjectsDir(t)
	copyFixturePacks(t, dir)
	db := openDB(t, dir, Options{})
	boom := errors.New("boom")
	prev := rootStat
	swapMaintain(t, &rootStat, func(root *os.Root, name string) (fs.FileInfo, error) {
		if strings.HasSuffix(name, keepSuffix) {
			return nil, boom
		}
		return prev(root, name)
	})

	if _, err := db.Packs(); !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
}

func TestPackObjectsListsTheObjectsOfOneNamedPack(t *testing.T) {
	dir := newObjectsDir(t)
	copyFixturePacks(t, dir)
	db := openDB(t, dir, Options{})
	packs, err := db.Packs()
	if err != nil {
		t.Fatal(err)
	}

	count := 0
	for range db.PackObjects(packs[0].Name) {
		count++
	}
	unknown := 0
	for range db.PackObjects("pack-unknown") {
		unknown++
	}
	stopped := 0
	for range db.PackObjects(packs[0].Name) {
		stopped++
		break
	}

	if count != packs[0].Objects || unknown != 0 || stopped != 1 {
		t.Fatalf("count = %d of %d, unknown = %d, stopped = %d", count, packs[0].Objects, unknown, stopped)
	}
}

func TestPackObjectsWithoutAnyPackfile(t *testing.T) {
	dir := newObjectsDir(t)
	_ = os.RemoveAll(filepath.Join(dir, packDirName))
	db := openDB(t, dir, Options{})

	for id := range db.PackObjects("pack-anything") {
		t.Fatalf("unexpected %s", id)
	}
}

func TestPutLooseWritesALooseCopyEvenOfAPackedObject(t *testing.T) {
	dir := newObjectsDir(t)
	copyFixturePacks(t, dir)
	db := openDB(t, dir, Options{})
	id := packFixtureObjects(t)[0].id
	kind, data, err := db.Get(id)
	if err != nil {
		t.Fatal(err)
	}

	got, err := db.PutLoose(kind, data)
	if err != nil || got != id {
		t.Fatalf("id = %s, err = %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(dir, looseName(id))); err != nil {
		t.Fatalf("no loose copy: %v", err)
	}
	if again, err := db.PutLoose(kind, data); err != nil || again != id {
		t.Fatalf("second put = %s, %v", again, err)
	}
	if _, err := db.PutLoose(0, data); err == nil {
		t.Fatal("an invalid type was accepted")
	}
}

func TestPutLooseReportsFailures(t *testing.T) {
	boom := errors.New("boom")
	t.Run("checking for a loose copy", func(t *testing.T) {
		db, _ := looseDB(t)
		swapMaintain(t, &rootStat, func(*os.Root, string) (fs.FileInfo, error) { return nil, boom })
		if _, err := db.PutLoose(1, []byte("tree? no, a commit that is not")); err == nil {
			t.Fatal("a failed check was ignored")
		}
	})
	t.Run("writing the loose copy", func(t *testing.T) {
		db, _ := looseDB(t)
		swapMaintain(t, &rootCreate, func(*os.Root, string, fs.FileMode) (*os.File, error) { return nil, boom })
		if _, err := db.PutLoose(3, []byte("a blob that is not there yet")); !errors.Is(err, boom) {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestTouchSetsTheTimeOfALooseObject(t *testing.T) {
	db, dir := looseDB(t)
	id := looseFixtureObjects(t)[0].id
	when := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)

	if err := db.Touch(id, when); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(filepath.Join(dir, looseName(id)))
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(when) {
		t.Fatalf("mtime = %v, want %v", info.ModTime(), when)
	}
	if err := db.Touch(hash.Zero, when); err == nil {
		t.Fatal("touching a missing object passed")
	}
}

func TestWritePackStoresTheObjectsInANewPackfile(t *testing.T) {
	db, dir := looseDB(t)
	var ids []hash.ObjectID
	for _, obj := range looseFixtureObjects(t) {
		ids = append(ids, obj.id)
	}

	result, err := db.WritePack(t.Context(), ids, pack.WriteOptions{Window: 10, Depth: 50})
	if err != nil {
		t.Fatal(err)
	}

	if result.Objects != len(ids) || filepath.Dir(result.PackPath) != filepath.Join(dir, packDirName) {
		t.Fatalf("result = %+v", result)
	}
	index, err := pack.OpenIndex(result.IndexPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = index.Close() }()
	if index.Count() != len(ids) {
		t.Fatalf("index holds %d objects, want %d", index.Count(), len(ids))
	}
}

func TestWritePackReportsFailures(t *testing.T) {
	boom := errors.New("boom")
	t.Run("creating the pack directory", func(t *testing.T) {
		db, _ := looseDB(t)
		swapMaintain(t, &rootMkdirAll, func(*os.Root, string, fs.FileMode) error { return boom })
		if _, err := db.WritePack(t.Context(), nil, pack.WriteOptions{}); !errors.Is(err, boom) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("writing the packfile", func(t *testing.T) {
		db, _ := looseDB(t)
		swapMaintain(t, &writePackFile, func(context.Context, string, pack.ObjectSource, []hash.ObjectID, pack.WriteOptions) (pack.IndexResult, error) {
			return pack.IndexResult{}, boom
		})
		if _, err := db.WritePack(t.Context(), nil, pack.WriteOptions{}); !errors.Is(err, boom) {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestRemovePackFilesDeletesThePackfileAndItsCompanionsButKeepsTheKeepFile(t *testing.T) {
	dir := t.TempDir()
	for _, suffix := range []string{".pack", ".idx", ".rev", ".bitmap", keepSuffix} {
		if err := os.WriteFile(filepath.Join(dir, "pack-old"+suffix), []byte("x"), 0o444); err != nil {
			t.Fatal(err)
		}
	}

	if err := RemovePackFiles(dir, "pack-old"); err != nil {
		t.Fatal(err)
	}

	left, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 1 || left[0].Name() != "pack-old"+keepSuffix {
		t.Fatalf("left = %v", left)
	}
}

func TestRemovePackFilesReportsEveryFileItCouldNotRemove(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "pack-busy.pack"), []byte("x"))
	boom := errors.New("boom")
	swapMaintain(t, &pathRemove, func(string) error { return boom })

	if err := RemovePackFiles(dir, "pack-busy"); !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
}
