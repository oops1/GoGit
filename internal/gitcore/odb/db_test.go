package odb

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/pack"
)

func newFixtureDB(t testing.TB) *DB {
	t.Helper()
	objects := newObjectsDir(t)
	copyFixtureLoose(t, objects)
	copyFixturePacks(t, objects)
	return openDB(t, objects, Options{})
}

func TestOpenReadsEveryPackedObject(t *testing.T) {
	db := newFixtureDB(t)
	for _, want := range packFixtureObjects(t) {
		kind, data, err := db.Get(want.id)
		if err != nil {
			t.Fatalf("Get(%s) returned error %v", want.id, err)
		}
		if kind != want.kind || int64(len(data)) != want.size {
			t.Fatalf("Get(%s) gave %s of %d bytes, want %s of %d", want.id, kind, len(data), want.kind, want.size)
		}
		if got := hash.SumSHA1(kind.String(), data); got != want.id {
			t.Fatalf("Get(%s) gave content named %s", want.id, got)
		}
	}
}

func TestOpenReadsEveryLooseObject(t *testing.T) {
	db := newFixtureDB(t)
	for _, want := range looseFixtureObjects(t) {
		kind, data, err := db.Get(want.id)
		if err != nil {
			t.Fatalf("Get(%s) returned error %v", want.id, err)
		}
		if kind != want.kind {
			t.Fatalf("Get(%s) gave %s, want %s", want.id, kind, want.kind)
		}
		if got := hash.SumSHA1(kind.String(), data); got != want.id {
			t.Fatalf("Get(%s) gave content named %s", want.id, got)
		}
	}
}

func TestOpenFailsWhenTheDirectoryIsMissing(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "objects")
	if _, err := Open(missing, Options{}); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Open returned %v, want a missing directory failure", err)
	}
}

func TestOpenRejectsUnsupportedObjectFormats(t *testing.T) {
	if _, err := Open(newObjectsDir(t), Options{Format: hash.SHA256}); !errors.Is(err, hash.ErrUnsupportedFormat) {
		t.Fatalf("Open returned %v, want %v", err, hash.ErrUnsupportedFormat)
	}
}

func TestOpenWorksWithoutAPackDirectory(t *testing.T) {
	db := openDB(t, newObjectsDir(t), Options{})
	if db.store() != nil {
		t.Fatal("Open created a pack store without a pack directory")
	}
	if _, _, err := db.Get(hash.Zero); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get returned %v, want %v", err, ErrNotFound)
	}
}

func TestOpenFailsOnADamagedPackDirectory(t *testing.T) {
	objects := newObjectsDir(t)
	writeFile(t, filepath.Join(objects, packDirName, "pack-broken.pack"), []byte("not a packfile"))
	writeFile(t, filepath.Join(objects, packDirName, "pack-broken.idx"), []byte("not an index"))
	if _, err := Open(objects, Options{}); !errors.Is(err, pack.ErrTruncated) {
		t.Fatalf("Open returned %v, want %v", err, pack.ErrTruncated)
	}
}

func TestOpenReportsTheConfiguredLayout(t *testing.T) {
	objects := newObjectsDir(t)
	db := openDB(t, objects, Options{})
	if db.Dir() != filepath.Clean(objects) {
		t.Fatalf("Dir gave %q, want %q", db.Dir(), objects)
	}
	if db.PackDir() != filepath.Join(objects, packDirName) {
		t.Fatalf("PackDir gave %q", db.PackDir())
	}
	if db.Format() != hash.SHA1 {
		t.Fatalf("Format gave %s, want %s", db.Format(), hash.SHA1)
	}
	if len(db.Alternates()) != 0 {
		t.Fatalf("Alternates gave %d entries", len(db.Alternates()))
	}
}

func TestGetServesRepeatedReadsFromTheCache(t *testing.T) {
	db := newFixtureDB(t)
	id := looseFixtureObjects(t)[0].id
	_, first, err := db.Get(id)
	if err != nil {
		t.Fatalf("Get returned error %v", err)
	}
	if db.cache.raw.len() != 1 {
		t.Fatalf("the cache holds %d entries after one read", db.cache.raw.len())
	}
	swapRootOpen(t, always, errInjected)
	_, second, err := db.Get(id)
	if err != nil {
		t.Fatalf("the second Get returned error %v", err)
	}
	if &first[0] != &second[0] {
		t.Fatal("the second Get rebuilt the object instead of using the cache")
	}
}

func TestGetFailsAfterClose(t *testing.T) {
	db := newFixtureDB(t)
	id := looseFixtureObjects(t)[0].id
	if err := db.Close(); err != nil {
		t.Fatalf("Close returned error %v", err)
	}
	if _, _, err := db.Get(id); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("Get returned %v, want %v", err, os.ErrClosed)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("the second Close returned error %v", err)
	}
}

func TestHasFindsLooseAndPackedObjects(t *testing.T) {
	db := newFixtureDB(t)
	for _, item := range []fixtureObject{looseFixtureObjects(t)[0], packFixtureObjects(t)[0]} {
		known, err := db.Has(item.id)
		if err != nil || !known {
			t.Fatalf("Has(%s) gave (%v, %v)", item.id, known, err)
		}
	}
	known, err := db.Has(hash.Zero)
	if err != nil || known {
		t.Fatalf("Has(zero) gave (%v, %v)", known, err)
	}
	warm := looseFixtureObjects(t)[1].id
	if _, _, err := db.Get(warm); err != nil {
		t.Fatalf("Get returned error %v", err)
	}
	if known, err := db.Has(warm); err != nil || !known {
		t.Fatalf("Has gave (%v, %v) for a cached object", known, err)
	}
}

func TestInfoReadsHeadersOfLooseAndPackedObjects(t *testing.T) {
	db := newFixtureDB(t)
	for _, want := range packFixtureObjects(t) {
		kind, size, err := db.Info(want.id)
		if err != nil {
			t.Fatalf("Info(%s) returned error %v", want.id, err)
		}
		if kind != want.kind || size != want.size {
			t.Fatalf("Info(%s) gave %s of %d bytes, want %s of %d", want.id, kind, size, want.kind, want.size)
		}
	}
	loose := looseFixtureObjects(t)[0]
	kind, err := db.Type(loose.id)
	if err != nil || kind != loose.kind {
		t.Fatalf("Type(%s) gave (%s, %v)", loose.id, kind, err)
	}
	size, err := db.Size(loose.id)
	if err != nil || size <= 0 {
		t.Fatalf("Size(%s) gave (%d, %v)", loose.id, size, err)
	}
}

func TestInfoUsesTheCacheWhenItIsWarm(t *testing.T) {
	db := newFixtureDB(t)
	want := packFixtureObjects(t)[0]
	if _, _, err := db.Get(want.id); err != nil {
		t.Fatalf("Get returned error %v", err)
	}
	kind, size, err := db.Info(want.id)
	if err != nil || kind != want.kind || size != want.size {
		t.Fatalf("Info gave (%s, %d, %v)", kind, size, err)
	}
}

func TestInfoReportsMissingObjects(t *testing.T) {
	db := newFixtureDB(t)
	if _, err := db.Type(hash.Zero); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Type returned %v, want %v", err, ErrNotFound)
	}
}

func TestTypedReadersParseEveryKind(t *testing.T) {
	db := newFixtureDB(t)
	commit := namedLooseFixture(t, "commit_root")
	if _, err := db.Commit(commit.id); err != nil {
		t.Fatalf("Commit returned error %v", err)
	}
	tree := namedLooseFixture(t, "tree_root")
	if _, err := db.Tree(tree.id); err != nil {
		t.Fatalf("Tree returned error %v", err)
	}
	blob := namedLooseFixture(t, "blob_hello")
	parsed, err := db.Blob(blob.id)
	if err != nil || string(parsed.Data) != "hello\n" {
		t.Fatalf("Blob gave (%q, %v)", parsed, err)
	}
	tag := namedLooseFixture(t, "tag_annotated")
	if _, err := db.Tag(tag.id); err != nil {
		t.Fatalf("Tag returned error %v", err)
	}
}

func TestTypedReadersRejectOtherKinds(t *testing.T) {
	db := newFixtureDB(t)
	blob := namedLooseFixture(t, "blob_hello").id
	commit := namedLooseFixture(t, "commit_root").id
	cases := []struct {
		name string
		call func() error
	}{
		{"commit", func() error { _, err := db.Commit(blob); return err }},
		{"tree", func() error { _, err := db.Tree(blob); return err }},
		{"blob", func() error { _, err := db.Blob(commit); return err }},
		{"tag", func() error { _, err := db.Tag(blob); return err }},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			if err := item.call(); !errors.Is(err, ErrWrongType) {
				t.Fatalf("the reader returned %v, want %v", err, ErrWrongType)
			}
		})
	}
}

func TestTypedReadersReportMissingObjects(t *testing.T) {
	db := newFixtureDB(t)
	cases := []struct {
		name string
		call func() error
	}{
		{"commit", func() error { _, err := db.Commit(hash.Zero); return err }},
		{"tree", func() error { _, err := db.Tree(hash.Zero); return err }},
		{"blob", func() error { _, err := db.Blob(hash.Zero); return err }},
		{"tag", func() error { _, err := db.Tag(hash.Zero); return err }},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			if err := item.call(); !errors.Is(err, ErrNotFound) {
				t.Fatalf("the reader returned %v, want %v", err, ErrNotFound)
			}
		})
	}
}

func TestTypedReadersSurfaceParseFailures(t *testing.T) {
	db := openDB(t, newObjectsDir(t), Options{})
	broken := []byte("this is not a valid object body")
	cases := []struct {
		name string
		kind object.Type
		call func(hash.ObjectID) error
	}{
		{"commit", object.TypeCommit, func(id hash.ObjectID) error { _, err := db.Commit(id); return err }},
		{"tree", object.TypeTree, func(id hash.ObjectID) error { _, err := db.Tree(id); return err }},
		{"tag", object.TypeTag, func(id hash.ObjectID) error { _, err := db.Tag(id); return err }},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			id, err := db.Put(item.kind, broken)
			if err != nil {
				t.Fatalf("Put returned error %v", err)
			}
			if err := item.call(id); err == nil {
				t.Fatal("the reader accepted a malformed object")
			}
		})
	}
}

func TestCommitAndTreeComeBackFromTheWeakCache(t *testing.T) {
	db := newFixtureDB(t)
	commitID := namedLooseFixture(t, "commit_root").id
	first, err := db.Commit(commitID)
	if err != nil {
		t.Fatalf("Commit returned error %v", err)
	}
	second, err := db.Commit(commitID)
	if err != nil || first != second {
		t.Fatalf("Commit gave a different value on the second call: %v", err)
	}
	treeID := namedLooseFixture(t, "tree_root").id
	firstTree, err := db.Tree(treeID)
	if err != nil {
		t.Fatalf("Tree returned error %v", err)
	}
	secondTree, err := db.Tree(treeID)
	if err != nil || firstTree != secondTree {
		t.Fatalf("Tree gave a different value on the second call: %v", err)
	}
}

func TestPeelWalksAChainOfTags(t *testing.T) {
	db := openDB(t, newObjectsDir(t), Options{})
	blob, err := db.Put(object.TypeBlob, []byte("peel me\n"))
	if err != nil {
		t.Fatalf("Put returned error %v", err)
	}
	inner := putTag(t, db, blob, object.TypeBlob)
	outer := putTag(t, db, inner, object.TypeTag)
	kind, target, err := db.Peel(outer)
	if err != nil || kind != object.TypeBlob || target != blob {
		t.Fatalf("Peel gave (%s, %s, %v), want (%s, %s)", kind, target, err, object.TypeBlob, blob)
	}
	peeled, isTag, err := db.PeelTag(outer)
	if err != nil || !isTag || peeled != blob {
		t.Fatalf("PeelTag gave (%s, %v, %v)", peeled, isTag, err)
	}
	plain, isTag, err := db.PeelTag(blob)
	if err != nil || isTag || !plain.IsZero() {
		t.Fatalf("PeelTag gave (%s, %v, %v) for a blob", plain, isTag, err)
	}
}

func TestPeelStopsOverlyLongTagChains(t *testing.T) {
	db := openDB(t, newObjectsDir(t), Options{})
	current, err := db.Put(object.TypeBlob, []byte("deep chain\n"))
	if err != nil {
		t.Fatalf("Put returned error %v", err)
	}
	kind := object.TypeBlob
	for range MaxTagChain + 1 {
		current = putTag(t, db, current, kind)
		kind = object.TypeTag
	}
	if _, _, err := db.Peel(current); !errors.Is(err, ErrTagChainTooDeep) {
		t.Fatalf("Peel returned %v, want %v", err, ErrTagChainTooDeep)
	}
	if _, _, err := db.PeelTag(current); !errors.Is(err, ErrTagChainTooDeep) {
		t.Fatalf("PeelTag returned %v, want %v", err, ErrTagChainTooDeep)
	}
}

func TestPeelReportsMissingObjects(t *testing.T) {
	db := openDB(t, newObjectsDir(t), Options{})
	if _, _, err := db.Peel(hash.Zero); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Peel returned %v, want %v", err, ErrNotFound)
	}
	if _, _, err := db.PeelTag(hash.Zero); !errors.Is(err, ErrNotFound) {
		t.Fatalf("PeelTag returned %v, want %v", err, ErrNotFound)
	}
	broken, err := db.Put(object.TypeTag, []byte("object nonsense\n"))
	if err != nil {
		t.Fatalf("Put returned error %v", err)
	}
	if _, _, err := db.Peel(broken); err == nil {
		t.Fatal("Peel accepted a malformed tag")
	}
}

func putTag(t testing.TB, db *DB, target hash.ObjectID, kind object.Type) hash.ObjectID {
	t.Helper()
	tag := &object.Tag{Object: target, ObjectType: kind, Name: "chain", Message: "link\n"}
	id, err := db.PutObject(tag)
	if err != nil {
		t.Fatalf("PutObject returned error %v", err)
	}
	return id
}

func TestResolveBaseServesStoredObjects(t *testing.T) {
	db := newFixtureDB(t)
	want := looseFixtureObjects(t)[0]
	kind, data, err := db.ResolveBase(want.id, 0)
	if err != nil || kind != want.kind {
		t.Fatalf("ResolveBase gave (%s, %d bytes, %v)", kind, len(data), err)
	}
}

func TestResolveBaseStopsTooDeepChains(t *testing.T) {
	db := openDB(t, newObjectsDir(t), Options{MaxDepth: 2})
	if _, _, err := db.ResolveBase(hash.Zero, 3); !errors.Is(err, pack.ErrDeltaChainTooDeep) {
		t.Fatalf("ResolveBase returned %v, want %v", err, pack.ErrDeltaChainTooDeep)
	}
}

func TestGetResolvesThinPackDeltasAgainstLooseBases(t *testing.T) {
	objects := newObjectsDir(t)
	db := openDB(t, objects, Options{})
	content := []byte("thin pack base payload\n")
	base, err := db.Put(object.TypeBlob, content)
	if err != nil {
		t.Fatalf("Put returned error %v", err)
	}
	suffix := []byte("appended\n")
	target := append(append([]byte{}, content...), suffix...)
	targetID := hash.SumSHA1(object.TypeBlob.String(), target)
	delta := append(deltaVarint(int64(len(content))), deltaVarint(int64(len(target)))...)
	delta = append(delta, copyWholeBase(int64(len(content)))...)
	delta = append(delta, insertOp(suffix)...)
	builder := newThinPack()
	builder.addRefDelta(t, base, delta)
	raw, index := builder.pair(t, []hash.ObjectID{targetID})
	writeFile(t, filepath.Join(objects, packDirName, "pack-thin.pack"), raw)
	writeFile(t, filepath.Join(objects, packDirName, "pack-thin.idx"), index)
	if _, err := db.Reload(); err != nil {
		t.Fatalf("Reload returned error %v", err)
	}
	kind, data, err := db.Get(targetID)
	if err != nil {
		t.Fatalf("Get returned error %v", err)
	}
	if kind != object.TypeBlob || string(data) != string(target) {
		t.Fatalf("Get gave %s %q", kind, data)
	}
	size, err := db.Size(targetID)
	if err != nil || size != int64(len(target)) {
		t.Fatalf("Size gave (%d, %v), want %d", size, err, len(target))
	}
}

func TestReloadNoticesNewAndRemovedPacks(t *testing.T) {
	objects := newObjectsDir(t)
	db := openDB(t, objects, Options{})
	copyFixturePacks(t, objects)
	changed, err := db.Reload()
	if err != nil || !changed {
		t.Fatalf("Reload gave (%v, %v) after packs appeared", changed, err)
	}
	if db.store() == nil {
		t.Fatal("Reload did not open the pack store")
	}
	packed := packFixtureObjects(t)[0]
	if known, err := db.Has(packed.id); err != nil || !known {
		t.Fatalf("Has gave (%v, %v)", known, err)
	}
	changed, err = db.Reload()
	if err != nil || changed {
		t.Fatalf("the second Reload gave (%v, %v)", changed, err)
	}
}

func TestReloadForgetsARemovedPackDirectory(t *testing.T) {
	objects := newObjectsDir(t)
	if err := os.MkdirAll(filepath.Join(objects, packDirName), 0o755); err != nil {
		t.Fatalf("MkdirAll returned error %v", err)
	}
	db := openDB(t, objects, Options{})
	if db.store() == nil {
		t.Fatal("Open skipped an empty pack directory")
	}
	if err := os.RemoveAll(db.PackDir()); err != nil {
		t.Fatalf("RemoveAll returned error %v", err)
	}
	changed, err := db.Reload()
	if err != nil || !changed {
		t.Fatalf("Reload gave (%v, %v) after the pack directory vanished", changed, err)
	}
	if db.store() != nil {
		t.Fatal("Reload kept the pack store of a removed directory")
	}
}

func TestReloadReportsBrokenPacks(t *testing.T) {
	objects := newObjectsDir(t)
	copyFixturePacks(t, objects)
	db := openDB(t, objects, Options{})
	writeFile(t, filepath.Join(objects, packDirName, "pack-broken.pack"), []byte("not a packfile"))
	writeFile(t, filepath.Join(objects, packDirName, "pack-broken.idx"), []byte("not an index"))
	if _, err := db.Reload(); !errors.Is(err, pack.ErrTruncated) {
		t.Fatalf("Reload returned %v, want %v", err, pack.ErrTruncated)
	}
}

func writeCustomPack(t testing.TB, objects, name string, builder *thinPack, ids []hash.ObjectID) {
	t.Helper()
	raw, index := builder.pair(t, ids)
	writeFile(t, filepath.Join(objects, packDirName, name+".pack"), raw)
	writeFile(t, filepath.Join(objects, packDirName, name+".idx"), index)
}

func TestInfoReportsPackIndexFailures(t *testing.T) {
	db := newFixtureDB(t)
	for _, file := range db.store().Files() {
		if err := file.Index.Close(); err != nil {
			t.Fatalf("Close returned error %v", err)
		}
	}
	packed := packFixtureObjects(t)[0]
	if _, err := db.Type(packed.id); err == nil {
		t.Fatal("Type accepted a closed pack index")
	}
}

func TestInfoReportsBadPackOffsets(t *testing.T) {
	objects := newObjectsDir(t)
	db := openDB(t, objects, Options{})
	id := hash.SumSHA1(object.TypeBlob.String(), []byte("bad offset"))
	builder := newThinPack()
	builder.addRefDelta(t, hash.Zero, []byte{0, 0})
	writeCustomPack(t, objects, "pack-badoffset", builder.at(0), []hash.ObjectID{id})
	if _, err := db.Reload(); err != nil {
		t.Fatalf("Reload returned error %v", err)
	}
	if _, err := db.Type(id); !errors.Is(err, pack.ErrBadOffset) {
		t.Fatalf("Type returned %v, want %v", err, pack.ErrBadOffset)
	}
}

func TestInfoReportsDeltasWithoutABase(t *testing.T) {
	objects := newObjectsDir(t)
	db := openDB(t, objects, Options{})
	id := hash.SumSHA1(object.TypeBlob.String(), []byte("missing base"))
	delta := append(deltaVarint(4), deltaVarint(4)...)
	delta = append(delta, insertOp([]byte("data"))...)
	builder := newThinPack()
	builder.addRefDelta(t, idFrom(t, 0xab), delta)
	writeCustomPack(t, objects, "pack-nobase", builder, []hash.ObjectID{id})
	if _, err := db.Reload(); err != nil {
		t.Fatalf("Reload returned error %v", err)
	}
	if _, err := db.Type(id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Type returned %v, want %v", err, ErrNotFound)
	}
}

func TestConcurrentReadersShareTheDatabase(t *testing.T) {
	db := newFixtureDB(t)
	packed := packFixtureObjects(t)
	loose := looseFixtureObjects(t)
	var readers sync.WaitGroup
	for worker := range 8 {
		readers.Go(func() {
			for step := range 32 {
				id := packed[(worker+step)%len(packed)].id
				if _, _, err := db.Get(id); err != nil {
					t.Errorf("Get(%s) returned error %v", id, err)
					return
				}
				name := loose[(worker+step)%len(loose)].id
				if _, err := db.Type(name); err != nil {
					t.Errorf("Type(%s) returned error %v", name, err)
					return
				}
				if _, err := db.Has(name); err != nil {
					t.Errorf("Has(%s) returned error %v", name, err)
					return
				}
				if _, err := db.Reload(); err != nil {
					t.Errorf("Reload returned error %v", err)
					return
				}
			}
		})
	}
	readers.Wait()
}

func TestConcurrentWritersStoreEveryObject(t *testing.T) {
	db := openDB(t, newObjectsDir(t), Options{})
	const workers = 8
	ids := make([]hash.ObjectID, workers)
	var writers sync.WaitGroup
	for worker := range workers {
		writers.Go(func() {
			id, err := db.Put(object.TypeBlob, fmt.Appendf(nil, "concurrent object %d\n", worker))
			if err != nil {
				t.Errorf("Put returned error %v", err)
				return
			}
			ids[worker] = id
		})
	}
	writers.Wait()
	for _, id := range ids {
		if _, _, err := db.Get(id); err != nil {
			t.Fatalf("Get(%s) returned error %v", id, err)
		}
	}
}
