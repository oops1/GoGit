package pack

import (
	"errors"
	"math"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
)

func openStore(t *testing.T, dir string, opts ...Option) *Store {
	t.Helper()
	store, err := Open(dir, opts...)
	if err != nil {
		t.Fatalf("Open(%q) returned error %v", dir, err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func openFixtureStore(t *testing.T, opts ...Option) *Store {
	t.Helper()
	return openStore(t, packsDir, opts...)
}

func TestStoreServesEveryObjectGitPacked(t *testing.T) {
	store := openFixtureStore(t)
	table := objectTypes(t)
	for id, want := range table {
		kind, data, ok, err := store.Get(id)
		if err != nil || !ok {
			t.Fatalf("Get(%s) returned (%v, %v)", id, ok, err)
		}
		if kind != want.kind || int64(len(data)) != want.size {
			t.Errorf("Get(%s) gave %d bytes of %s, git says %d bytes of %s",
				id, len(data), kind, want.size, want.kind)
		}
		if got := hash.SumSHA1(kind.String(), data); got != id {
			t.Errorf("Get(%s) rebuilt %s", id, got)
		}
		found, err := store.Contains(id)
		if err != nil || !found {
			t.Fatalf("Contains(%s) returned (%v, %v)", id, found, err)
		}
	}
	if got := len(slices.Collect(store.Objects())); got != len(table) {
		t.Fatalf("Objects yielded %d names, the object table lists %d", got, len(table))
	}
	if store.Count() != 2*len(table) {
		t.Fatalf("Count = %d, want %d", store.Count(), 2*len(table))
	}
	if store.Dir() != packsDir {
		t.Fatalf("Dir = %q, want %q", store.Dir(), packsDir)
	}
	if len(store.Files()) != 2 {
		t.Fatalf("Files listed %d packfiles, want 2", len(store.Files()))
	}
	if err := store.Verify(); err != nil {
		t.Fatalf("Verify returned error %v", err)
	}
}

func TestStoreMissesUnknownObject(t *testing.T) {
	store := openFixtureStore(t)
	unknown := idOfByte(0xfe)
	if _, _, ok, err := store.Get(unknown); ok || err != nil {
		t.Fatalf("Get returned (%v, %v), want (false, nil)", ok, err)
	}
	if found, err := store.Contains(unknown); found || err != nil {
		t.Fatalf("Contains returned (%v, %v), want (false, nil)", found, err)
	}
	if _, _, err := store.ResolveBase(unknown, 0); !errors.Is(err, ErrBaseNotFound) {
		t.Fatalf("ResolveBase returned %v, want %v", err, ErrBaseNotFound)
	}
}

func TestStoreObjectsStopsWhenTheCallerStops(t *testing.T) {
	store := openFixtureStore(t)
	seen := 0
	for range store.Objects() {
		seen++
		break
	}
	if seen != 1 {
		t.Fatalf("Objects yielded %d names after a break, want 1", seen)
	}
}

func TestStoreSearchesTheNewestPackfileFirst(t *testing.T) {
	names := fixtureNames(t)
	dir := copyFixtureDir(t, names...)
	oldest := time.Now().Add(-time.Hour)
	if err := os.Chtimes(filepath.Join(dir, names[0]+indexSuffix), oldest, oldest); err != nil {
		t.Fatalf("Chtimes returned error %v", err)
	}
	store := openStore(t, dir)
	files := store.Files()
	if len(files) != 2 {
		t.Fatalf("Files listed %d packfiles, want 2", len(files))
	}
	if files[0].Name != names[1] {
		t.Fatalf("the first packfile is %q, want the newest %q", files[0].Name, names[1])
	}
}

func TestStoreReloadNoticesNewAndRemovedPackfiles(t *testing.T) {
	names := fixtureNames(t)
	dir := copyFixtureDir(t, names[0])
	store := openStore(t, dir)
	if changed, err := store.Reload(); changed || err != nil {
		t.Fatalf("Reload returned (%v, %v), want (false, nil)", changed, err)
	}
	for _, suffix := range []string{packSuffix, indexSuffix} {
		writeTemp(t, filepath.Join(dir, names[1]+suffix),
			readFixture(t, filepath.Join(packsDir, names[1]+suffix)))
	}
	if changed, err := store.Reload(); !changed || err != nil {
		t.Fatalf("Reload returned (%v, %v), want (true, nil)", changed, err)
	}
	if len(store.Files()) != 2 {
		t.Fatalf("Files listed %d packfiles after the copy, want 2", len(store.Files()))
	}
	for _, file := range store.Files() {
		if file.Name != names[1] {
			continue
		}
		if err := file.Index.Close(); err != nil {
			t.Fatalf("Close returned error %v", err)
		}
		if err := file.Pack.Close(); err != nil {
			t.Fatalf("Close returned error %v", err)
		}
	}
	for _, suffix := range []string{packSuffix, indexSuffix} {
		if err := os.Remove(filepath.Join(dir, names[1]+suffix)); err != nil {
			t.Fatalf("Remove returned error %v", err)
		}
	}
	if changed, err := store.Reload(); !changed || err != nil {
		t.Fatalf("Reload returned (%v, %v), want (true, nil)", changed, err)
	}
	if len(store.Files()) != 1 {
		t.Fatalf("Files listed %d packfiles after the removal, want 1", len(store.Files()))
	}
}

func TestStoreReloadReopensRewrittenPackfiles(t *testing.T) {
	name := fixtureNames(t)[0]
	dir := copyFixtureDir(t, name)
	store := openStore(t, dir)
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(filepath.Join(dir, name+indexSuffix), later, later); err != nil {
		t.Fatalf("Chtimes returned error %v", err)
	}
	if changed, err := store.Reload(); !changed || err != nil {
		t.Fatalf("Reload returned (%v, %v), want (true, nil)", changed, err)
	}
}

func TestStoreSkipsPackfilesWithoutAnIndex(t *testing.T) {
	name := fixtureNames(t)[0]
	dir := copyFixtureDir(t)
	writeTemp(t, filepath.Join(dir, name+packSuffix), readFixture(t, filepath.Join(packsDir, name+packSuffix)))
	writeTemp(t, filepath.Join(dir, "notes.txt"), []byte("ignored"))
	store := openStore(t, dir)
	if len(store.Files()) != 0 {
		t.Fatalf("Files listed %d packfiles, want none", len(store.Files()))
	}
}

func TestOpenStoreReportsMissingDirectory(t *testing.T) {
	if _, err := Open(filepath.Join(t.TempDir(), "absent")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Open returned %v, want %v", err, os.ErrNotExist)
	}
}

func TestOpenStoreReportsBrokenPairs(t *testing.T) {
	names := fixtureNames(t)
	t.Run("brokenIndex", func(t *testing.T) {
		dir := copyFixtureDir(t, names[0])
		writeTemp(t, filepath.Join(dir, names[0]+indexSuffix), []byte("not an index"))
		if _, err := Open(dir); !errors.Is(err, ErrTruncated) {
			t.Fatalf("Open returned %v, want %v", err, ErrTruncated)
		}
	})
	t.Run("brokenPack", func(t *testing.T) {
		dir := copyFixtureDir(t, names[0])
		writeTemp(t, filepath.Join(dir, names[0]+packSuffix), []byte("not a packfile"))
		if _, err := Open(dir); !errors.Is(err, ErrTruncated) {
			t.Fatalf("Open returned %v, want %v", err, ErrTruncated)
		}
	})
	t.Run("mismatchedPair", func(t *testing.T) {
		dir := copyFixtureDir(t, names[0])
		writeTemp(t, filepath.Join(dir, names[0]+packSuffix),
			readFixture(t, filepath.Join(packsDir, names[1]+packSuffix)))
		if _, err := Open(dir); !errors.Is(err, ErrPackMismatch) {
			t.Fatalf("Open returned %v, want %v", err, ErrPackMismatch)
		}
	})
}

func TestStoreReloadKeepsTheOldStateWhenAPackfileIsBroken(t *testing.T) {
	names := fixtureNames(t)
	dir := copyFixtureDir(t, names[0])
	store := openStore(t, dir)
	for _, suffix := range []string{packSuffix, indexSuffix} {
		writeTemp(t, filepath.Join(dir, names[1]+suffix), []byte("broken"))
	}
	if _, err := store.Reload(); !errors.Is(err, ErrTruncated) {
		t.Fatalf("Reload returned %v, want %v", err, ErrTruncated)
	}
	if len(store.Files()) != 1 {
		t.Fatalf("Files listed %d packfiles after a failed reload, want 1", len(store.Files()))
	}
}

func TestStoreReloadClosesFilesItOpenedBeforeAFailure(t *testing.T) {
	names := fixtureNames(t)
	dir := copyFixtureDir(t, names...)
	writeTemp(t, filepath.Join(dir, "zzz"+packSuffix), []byte("broken"))
	broken := writeTemp(t, filepath.Join(dir, "zzz"+indexSuffix), []byte("broken"))
	oldest := time.Now().Add(-time.Hour)
	if err := os.Chtimes(broken, oldest, oldest); err != nil {
		t.Fatalf("Chtimes returned error %v", err)
	}
	if _, err := Open(dir); !errors.Is(err, ErrTruncated) {
		t.Fatalf("Open returned %v, want %v", err, ErrTruncated)
	}
}

func TestStoreSortsPackfilesOfTheSameAgeByName(t *testing.T) {
	names := fixtureNames(t)
	dir := copyFixtureDir(t, names...)
	stamp := time.Now().Add(-time.Minute)
	for _, name := range names {
		if err := os.Chtimes(filepath.Join(dir, name+indexSuffix), stamp, stamp); err != nil {
			t.Fatalf("Chtimes returned error %v", err)
		}
	}
	files := openStore(t, dir).Files()
	if len(files) != 2 || files[0].Name != names[0] || files[1].Name != names[1] {
		t.Fatalf("the store lists %v, want %v in name order", files, names)
	}
}

func TestStoreResolveBasePropagatesIndexFailures(t *testing.T) {
	store := openFixtureStore(t)
	files := store.Files()
	raw := readFixture(t, fixtureIndexPath(t, files[0].Name))
	files[0].Index.source = brokenReaderAt{data: raw, from: files[0].Index.names, to: math.MaxInt64}
	if _, _, err := store.ResolveBase(objectTypesFirstID(t), 0); !errors.Is(err, errRead) {
		t.Fatalf("ResolveBase returned %v, want %v", err, errRead)
	}
}

func TestStoreCloseReportsFailures(t *testing.T) {
	dir := copyFixtureDir(t, fixtureNames(t)[0])
	store, err := Open(dir)
	if err != nil {
		t.Fatalf("Open returned error %v", err)
	}
	file := store.Files()[0]
	if err := file.Index.Close(); err != nil {
		t.Fatalf("Close returned error %v", err)
	}
	if err := store.Close(); err == nil {
		t.Fatal("Close reported no failure after the index was closed twice")
	}
}

func TestStoreClosePackFailureIsReported(t *testing.T) {
	dir := copyFixtureDir(t, fixtureNames(t)[0])
	store, err := Open(dir)
	if err != nil {
		t.Fatalf("Open returned error %v", err)
	}
	file := store.Files()[0]
	if err := file.Pack.Close(); err != nil {
		t.Fatalf("Close returned error %v", err)
	}
	if err := store.Close(); err == nil {
		t.Fatal("Close reported no failure after the packfile was closed twice")
	}
}

func TestStoreResolvesBasesForThinPackfiles(t *testing.T) {
	store := openFixtureStore(t)
	raw := readFixture(t, thinPackPath)
	offset, head := firstRefDelta(t, raw)
	kind, data, err := store.ResolveBase(head.BaseID, 0)
	if err != nil {
		t.Fatalf("ResolveBase returned error %v", err)
	}
	if kind == 0 || len(data) == 0 {
		t.Fatalf("ResolveBase gave %s and %d bytes", kind, len(data))
	}
	thin := packOf(t, raw, WithBaseResolver(store))
	if _, _, err := thin.ObjectAt(offset); err != nil {
		t.Fatalf("ObjectAt returned error %v", err)
	}
}

func TestStoreResolveBaseStopsTooDeepChains(t *testing.T) {
	store := openFixtureStore(t, WithMaxDeltaDepth(2))
	id := objectTypesFirstID(t)
	if _, _, err := store.ResolveBase(id, 3); !errors.Is(err, ErrDeltaChainTooDeep) {
		t.Fatalf("ResolveBase returned %v, want %v", err, ErrDeltaChainTooDeep)
	}
}

func TestStoreResolveBaseAsksTheOuterResolver(t *testing.T) {
	content := []byte("object kept outside every packfile")
	id := hash.SumSHA1(object.TypeBlob.String(), content)
	outer := mapResolver{id: {kind: object.TypeBlob, data: content}}
	store := openFixtureStore(t, WithBaseResolver(outer))
	kind, data, err := store.ResolveBase(id, 0)
	if err != nil {
		t.Fatalf("ResolveBase returned error %v", err)
	}
	if kind != object.TypeBlob || string(data) != string(content) {
		t.Fatalf("ResolveBase gave %s %q", kind, data)
	}
}

func TestStorePropagatesIndexFailures(t *testing.T) {
	store := openFixtureStore(t)
	files := store.Files()
	raw := readFixture(t, fixtureIndexPath(t, files[0].Name))
	files[0].Index.source = brokenReaderAt{data: raw, from: files[0].Index.names, to: math.MaxInt64}
	id := objectTypesFirstID(t)
	if _, _, _, err := store.Get(id); !errors.Is(err, errRead) {
		t.Fatalf("Get returned %v, want %v", err, errRead)
	}
	if _, err := store.Contains(id); !errors.Is(err, errRead) {
		t.Fatalf("Contains returned %v, want %v", err, errRead)
	}
	if err := store.Verify(); !errors.Is(err, errRead) {
		t.Fatalf("Verify returned %v, want %v", err, errRead)
	}
}

func TestStorePropagatesPackFailures(t *testing.T) {
	store := openFixtureStore(t)
	files := store.Files()
	raw := readFixture(t, fixturePackPath(t, files[0].Name))
	files[0].Pack.source = brokenReaderAt{data: raw, from: 0, to: math.MaxInt64}
	record := verifyRecords(t, files[0].Name)[0]
	if _, _, _, err := store.Get(record.id); !errors.Is(err, errRead) {
		t.Fatalf("Get returned %v, want %v", err, errRead)
	}
	if err := store.Verify(); err == nil {
		t.Fatal("Verify returned no error on a broken packfile")
	}
}

func TestStoreSharesOneCacheAcrossPackfiles(t *testing.T) {
	cache := NewCache(1 << 20)
	store := openFixtureStore(t, WithCache(cache))
	id := objectTypesFirstID(t)
	if _, _, _, err := store.Get(id); err != nil {
		t.Fatalf("Get returned error %v", err)
	}
	if cache.Len() == 0 {
		t.Fatal("the shared cache stayed empty")
	}
	if _, err := store.Reload(); err != nil {
		t.Fatalf("Reload returned error %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close returned error %v", err)
	}
	if cache.Len() != 0 {
		t.Fatalf("the cache still holds %d objects after the store was closed", cache.Len())
	}
}

func objectTypesFirstID(t *testing.T) hash.ObjectID {
	t.Helper()
	return verifyRecords(t, fixtureName(t, offsetPack))[0].id
}
