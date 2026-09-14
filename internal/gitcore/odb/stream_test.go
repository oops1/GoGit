package odb

import (
	"errors"
	"io"
	"os"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/pack"
)

func readStream(t *testing.T, db *DB, id hash.ObjectID) (object.Type, []byte, error) {
	t.Helper()
	kind, size, reader, err := db.Stream(id)
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = reader.Close() }()
	data, err := io.ReadAll(reader)
	if err == nil && int64(len(data)) != size {
		t.Fatalf("Stream declared %d bytes and gave %d", size, len(data))
	}
	return kind, data, err
}

func TestStreamReadsLooseCachedAndPackedObjects(t *testing.T) {
	objects := newObjectsDir(t)
	copyFixturePacks(t, objects)
	db := openDB(t, objects, Options{})
	loose, err := db.Put(object.TypeBlob, []byte("streamed loose\n"))
	if err != nil {
		t.Fatalf("Put returned error %v", err)
	}

	if kind, data, err := readStream(t, db, loose); err != nil || kind != object.TypeBlob || string(data) != "streamed loose\n" {
		t.Fatalf("stream of a loose object = (%v, %q, %v)", kind, data, err)
	}
	for _, fixture := range packFixtureObjects(t) {
		kind, data, err := readStream(t, db, fixture.id)
		if err != nil || kind != fixture.kind || int64(len(data)) != fixture.size {
			t.Fatalf("stream of packed %s = (%v, %d bytes, %v)", fixture.id, kind, len(data), err)
		}
		if _, _, err := db.Get(fixture.id); err != nil {
			t.Fatalf("Get returned error %v", err)
		}
		if kind, cached, err := readStream(t, db, fixture.id); err != nil || kind != fixture.kind || string(cached) != string(data) {
			t.Fatalf("stream of cached %s = (%v, %d bytes, %v)", fixture.id, kind, len(cached), err)
		}
	}
}

func TestStreamFindsPacksThatAppearLaterAndReportsMissingObjects(t *testing.T) {
	objects := newObjectsDir(t)
	db := openDB(t, objects, Options{})
	fixture := packFixtureObjects(t)[0]

	if _, _, err := readStream(t, db, fixture.id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("stream of a missing object returned %v, want %v", err, ErrNotFound)
	}
	copyFixturePacks(t, objects)
	if kind, _, err := readStream(t, db, fixture.id); err != nil || kind != fixture.kind {
		t.Fatalf("stream after the pack appeared = (%v, %v)", kind, err)
	}
}

func TestStreamReportsAFailedReload(t *testing.T) {
	db := openDB(t, newObjectsDir(t), Options{})
	boom := errors.New("boom")
	original := packOpen
	packOpen = func(string, ...pack.Option) (*pack.Store, error) { return nil, boom }
	t.Cleanup(func() { packOpen = original })

	if _, _, _, err := db.Stream(hash.ObjectID{1}); !errors.Is(err, boom) {
		t.Fatalf("Stream returned %v, want %v", err, boom)
	}
}

func TestStreamVerifiesLooseObjects(t *testing.T) {
	name := []byte("the content the name promises\n")
	id := hash.SumSHA1("blob", name)
	for _, tt := range []struct {
		name    string
		payload func(t *testing.T) []byte
		atOpen  bool
		sticky  bool
		want    error
	}{
		{"data that is not zlib", func(*testing.T) []byte { return []byte("not zlib") }, true, false, object.ErrMalformed},
		{"a header that is not a header", func(t *testing.T) []byte { return deflate(t, []byte("blob x\x00abc")) }, true, false, nil},
		{"content of another object", func(t *testing.T) []byte { return looseBytes(t, object.TypeBlob, []byte("someone else entirely\n")) }, false, true, ErrCorrupt},
		{"content longer than declared", func(t *testing.T) []byte { return deflate(t, []byte("blob 3\x00abcdef")) }, false, true, ErrCorrupt},
		{"content shorter than declared", func(t *testing.T) []byte { return deflate(t, []byte("blob 30\x00abc")) }, false, false, io.ErrUnexpectedEOF},
	} {
		t.Run(tt.name, func(t *testing.T) {
			objects := newObjectsDir(t)
			writeLooseFile(t, objects, id, tt.payload(t))
			db := openDB(t, objects, Options{})

			_, _, reader, err := db.Stream(id)
			if tt.atOpen {
				if err == nil || tt.want != nil && !errors.Is(err, tt.want) {
					t.Fatalf("Stream returned %v, want %v", err, tt.want)
				}
				return
			}
			if err != nil {
				t.Fatalf("Stream returned error %v", err)
			}
			defer func() { _ = reader.Close() }()
			if _, err := io.ReadAll(reader); !errors.Is(err, tt.want) {
				t.Fatalf("ReadAll returned %v, want %v", err, tt.want)
			}
			if tt.sticky {
				if _, err := reader.Read(make([]byte, 1)); !errors.Is(err, ErrCorrupt) {
					t.Fatalf("a second Read returned %v, want %v", err, ErrCorrupt)
				}
			}
		})
	}
}

func TestStreamReportsLooseFilesItCannotOpen(t *testing.T) {
	objects := newObjectsDir(t)
	db := openDB(t, objects, Options{})
	id, err := db.Put(object.TypeBlob, []byte("unreadable\n"))
	if err != nil {
		t.Fatalf("Put returned error %v", err)
	}
	boom := errors.New("boom")
	swapRootOpen(t, always, boom)

	if _, _, _, err := db.Stream(id); !errors.Is(err, boom) {
		t.Fatalf("Stream returned %v, want %v", err, boom)
	}
}

func TestStreamReportsABrokenPackedObject(t *testing.T) {
	objects := newObjectsDir(t)
	writer := openDB(t, objects, Options{})
	id, err := writer.Put(object.TypeBlob, []byte("abc"))
	if err != nil {
		t.Fatalf("Put returned error %v", err)
	}
	written, err := writer.WritePack(t.Context(), []hash.ObjectID{id}, pack.WriteOptions{})
	if err != nil {
		t.Fatalf("WritePack returned error %v", err)
	}
	if err := writer.RemoveLoose(id); err != nil {
		t.Fatalf("RemoveLoose returned error %v", err)
	}
	if err := os.Chmod(written.PackPath, 0o644); err != nil {
		t.Fatalf("Chmod returned error %v", err)
	}
	raw := readFile(t, written.PackPath)
	raw[13] = 0
	writeFile(t, written.PackPath, raw)
	db := openDB(t, objects, Options{})

	if _, _, _, err := db.Stream(id); !errors.Is(err, pack.ErrDecompress) {
		t.Fatalf("Stream returned %v, want %v", err, pack.ErrDecompress)
	}
}

func TestStreamReadsAndReportsObjectsOfAlternates(t *testing.T) {
	shared := newObjectsDir(t)
	good, err := openDB(t, shared, Options{}).Put(object.TypeBlob, []byte("shared\n"))
	if err != nil {
		t.Fatalf("Put returned error %v", err)
	}
	bad := hash.SumSHA1("blob", []byte("broken shared\n"))
	writeLooseFile(t, shared, bad, []byte("not zlib"))
	db := openDB(t, newObjectsDir(t), Options{Alternates: []string{shared}})

	if kind, data, err := readStream(t, db, good); err != nil || kind != object.TypeBlob || string(data) != "shared\n" {
		t.Fatalf("stream of a shared object = (%v, %q, %v)", kind, data, err)
	}
	if _, _, _, err := db.Stream(bad); !errors.Is(err, object.ErrMalformed) {
		t.Fatalf("Stream of a broken shared object returned %v", err)
	}
}

func TestPackReuseCoversTheDatabaseAndItsAlternates(t *testing.T) {
	shared := newObjectsDir(t)
	copyFixturePacks(t, shared)
	objects := newObjectsDir(t)
	copyFixturePacks(t, objects)
	db := openDB(t, objects, Options{Alternates: []string{shared}})

	if stores := db.packStores(); len(stores) != 2 {
		t.Fatalf("packStores = %d stores, want 2", len(stores))
	}
	db.PackReuse().Close()
	if stores := openDB(t, newObjectsDir(t), Options{}).packStores(); len(stores) != 0 {
		t.Fatalf("a database without packs has %d stores", len(stores))
	}
}

func TestForgetPacksDropsPacksUntilTheNextReload(t *testing.T) {
	if err := openDB(t, newObjectsDir(t), Options{}).ForgetPacks("pack-none"); err != nil {
		t.Fatalf("ForgetPacks without packs returned error %v", err)
	}
	objects := newObjectsDir(t)
	copyFixturePacks(t, objects)
	db := openDB(t, objects, Options{})
	before, err := db.Packs()
	if err != nil || len(before) == 0 {
		t.Fatalf("Packs = (%v, %v)", before, err)
	}

	if err := db.ForgetPacks(before[0].Name); err != nil {
		t.Fatalf("ForgetPacks returned error %v", err)
	}

	if after, err := db.Packs(); err != nil || len(after) != len(before)-1 {
		t.Fatalf("Packs after forgetting = (%v, %v)", after, err)
	}
	if _, err := db.Reload(); err != nil {
		t.Fatalf("Reload returned error %v", err)
	}
	if again, err := db.Packs(); err != nil || len(again) != len(before) {
		t.Fatalf("Packs after reloading = (%v, %v)", again, err)
	}
}
