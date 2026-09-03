package odb

import (
	"bytes"
	"crypto/rand"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
)

func countFiles(t testing.TB, dir string) int {
	t.Helper()
	total := 0
	err := filepath.WalkDir(dir, func(_ string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			total++
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WalkDir returned error %v", err)
	}
	return total
}

func TestPutStoresAndReadsBackEveryKind(t *testing.T) {
	db := openDB(t, newObjectsDir(t), Options{})
	cases := []struct {
		name string
		kind object.Type
		data []byte
	}{
		{"blob", object.TypeBlob, []byte("stored blob\n")},
		{"tree", object.TypeTree, nil},
		{"commit", object.TypeCommit, []byte("tree 4b825dc642cb6eb9a060e54bf8d69288fbee4904\n\nmessage\n")},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			id, err := db.Put(item.kind, item.data)
			if err != nil {
				t.Fatalf("Put returned error %v", err)
			}
			if id != hash.SumSHA1(item.kind.String(), item.data) {
				t.Fatalf("Put gave the name %s", id)
			}
			kind, data, err := db.Get(id)
			if err != nil || kind != item.kind || !bytes.Equal(data, item.data) {
				t.Fatalf("Get gave (%s, %q, %v)", kind, data, err)
			}
		})
	}
}

func TestPutWritesTheObjectOnlyOnce(t *testing.T) {
	objects := newObjectsDir(t)
	db := openDB(t, objects, Options{})
	first, err := db.Put(object.TypeBlob, []byte("only once\n"))
	if err != nil {
		t.Fatalf("Put returned error %v", err)
	}
	second, err := db.Put(object.TypeBlob, []byte("only once\n"))
	if err != nil || first != second {
		t.Fatalf("the second Put gave (%s, %v)", second, err)
	}
	if total := countFiles(t, objects); total != 1 {
		t.Fatalf("the object directory holds %d files, want 1", total)
	}
}

func TestPutSkipsObjectsThatArePacked(t *testing.T) {
	objects := newObjectsDir(t)
	copyFixturePacks(t, objects)
	db := openDB(t, objects, Options{})
	want := packFixtureObjects(t)[0]
	kind, data, err := db.Get(want.id)
	if err != nil {
		t.Fatalf("Get returned error %v", err)
	}
	db.cache.purge()
	id, err := db.Put(kind, data)
	if err != nil || id != want.id {
		t.Fatalf("Put gave (%s, %v)", id, err)
	}
	if _, err := os.Stat(filepath.Join(objects, looseName(want.id))); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Put wrote a loose copy of a packed object: %v", err)
	}
}

func TestPutObjectStoresTheEncodedForm(t *testing.T) {
	db := openDB(t, newObjectsDir(t), Options{})
	blob := &object.Blob{Data: []byte("encoded\n")}
	id, err := db.PutObject(blob)
	if err != nil || id != blob.ID() {
		t.Fatalf("PutObject gave (%s, %v), want %s", id, err, blob.ID())
	}
}

func TestPutRejectsUnknownTypes(t *testing.T) {
	db := openDB(t, newObjectsDir(t), Options{})
	if _, err := db.Put(object.Type(9), nil); !errors.Is(err, object.ErrUnknownType) {
		t.Fatalf("Put returned %v, want %v", err, object.ErrUnknownType)
	}
	if _, err := db.Writer(object.Type(9), 0); !errors.Is(err, object.ErrUnknownType) {
		t.Fatalf("Writer returned %v, want %v", err, object.ErrUnknownType)
	}
}

func TestPutReportsLookupFailures(t *testing.T) {
	db := openDB(t, newObjectsDir(t), Options{})
	swapRootStat(t, always, errInjected)
	if _, err := db.Put(object.TypeBlob, []byte("lookup fails\n")); !errors.Is(err, errInjected) {
		t.Fatalf("Put returned %v, want %v", err, errInjected)
	}
}

func TestPutReportsTemporaryFileFailures(t *testing.T) {
	db := openDB(t, newObjectsDir(t), Options{})
	swapRootCreate(t, errInjected)
	if _, err := db.Put(object.TypeBlob, []byte("no temp file\n")); !errors.Is(err, errInjected) {
		t.Fatalf("Put returned %v, want %v", err, errInjected)
	}
}

func TestPutReportsWriteFailures(t *testing.T) {
	objects := newObjectsDir(t)
	db := openDB(t, objects, Options{})
	swapFileWrite(t, errInjected)
	if _, err := db.Put(object.TypeBlob, []byte("no room\n")); !errors.Is(err, errInjected) {
		t.Fatalf("Put returned %v, want %v", err, errInjected)
	}
	if total := countFiles(t, objects); total != 0 {
		t.Fatalf("a failed Put left %d files behind", total)
	}
}

func TestPutReportsRenameFailures(t *testing.T) {
	objects := newObjectsDir(t)
	db := openDB(t, objects, Options{})
	swapRootRename(t, errInjected)
	if _, err := db.Put(object.TypeBlob, []byte("no rename\n")); !errors.Is(err, errInjected) {
		t.Fatalf("Put returned %v, want %v", err, errInjected)
	}
	if total := countFiles(t, objects); total != 0 {
		t.Fatalf("a failed Put left %d files behind", total)
	}
}

func TestPutReportsDirectoryFailures(t *testing.T) {
	db := openDB(t, newObjectsDir(t), Options{})
	swapRootMkdirAll(t, errInjected)
	if _, err := db.Put(object.TypeBlob, []byte("no fanout\n")); !errors.Is(err, errInjected) {
		t.Fatalf("Put returned %v, want %v", err, errInjected)
	}
}

func TestWriterStreamsALargeBlob(t *testing.T) {
	objects := newObjectsDir(t)
	db := openDB(t, objects, Options{})
	payload := bytes.Repeat([]byte("streamed payload\n"), 4096)
	writer, err := db.Writer(object.TypeBlob, int64(len(payload)))
	if err != nil {
		t.Fatalf("Writer returned error %v", err)
	}
	if _, err := io.Copy(writer, bytes.NewReader(payload)); err != nil {
		t.Fatalf("Copy returned error %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close returned error %v", err)
	}
	want := hash.SumSHA1(object.TypeBlob.String(), payload)
	if writer.ID() != want {
		t.Fatalf("ID gave %s, want %s", writer.ID(), want)
	}
	kind, data, err := db.Get(want)
	if err != nil || kind != object.TypeBlob || !bytes.Equal(data, payload) {
		t.Fatalf("Get gave (%s, %d bytes, %v)", kind, len(data), err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("the second Close returned error %v", err)
	}
	if _, err := writer.Write([]byte("more")); !errors.Is(err, ErrWriterClosed) {
		t.Fatalf("Write returned %v, want %v", err, ErrWriterClosed)
	}
}

func TestWriterRejectsNegativeSizes(t *testing.T) {
	db := openDB(t, newObjectsDir(t), Options{})
	if _, err := db.Writer(object.TypeBlob, -1); !errors.Is(err, hash.ErrNegativeSize) {
		t.Fatalf("Writer returned %v, want %v", err, hash.ErrNegativeSize)
	}
}

func TestWriterRejectsMoreBytesThanDeclared(t *testing.T) {
	db := openDB(t, newObjectsDir(t), Options{})
	writer, err := db.Writer(object.TypeBlob, 4)
	if err != nil {
		t.Fatalf("Writer returned error %v", err)
	}
	if _, err := writer.Write([]byte("far too long")); !errors.Is(err, hash.ErrSizeMismatch) {
		t.Fatalf("Write returned %v, want %v", err, hash.ErrSizeMismatch)
	}
	if err := writer.Close(); !errors.Is(err, hash.ErrSizeMismatch) {
		t.Fatalf("Close returned %v, want %v", err, hash.ErrSizeMismatch)
	}
}

func TestWriterRejectsShortStreams(t *testing.T) {
	objects := newObjectsDir(t)
	db := openDB(t, objects, Options{})
	writer, err := db.Writer(object.TypeBlob, 16)
	if err != nil {
		t.Fatalf("Writer returned error %v", err)
	}
	if _, err := writer.Write([]byte("short")); err != nil {
		t.Fatalf("Write returned error %v", err)
	}
	if err := writer.Close(); !errors.Is(err, hash.ErrSizeMismatch) {
		t.Fatalf("Close returned %v, want %v", err, hash.ErrSizeMismatch)
	}
	if total := countFiles(t, objects); total != 0 {
		t.Fatalf("a failed writer left %d files behind", total)
	}
}

func TestWriterDropsObjectsThatAreAlreadyStored(t *testing.T) {
	objects := newObjectsDir(t)
	db := openDB(t, objects, Options{})
	payload := []byte("already stored\n")
	id, err := db.Put(object.TypeBlob, payload)
	if err != nil {
		t.Fatalf("Put returned error %v", err)
	}
	writer, err := db.Writer(object.TypeBlob, int64(len(payload)))
	if err != nil {
		t.Fatalf("Writer returned error %v", err)
	}
	if _, err := writer.Write(payload); err != nil {
		t.Fatalf("Write returned error %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close returned error %v", err)
	}
	if writer.ID() != id {
		t.Fatalf("ID gave %s, want %s", writer.ID(), id)
	}
	if total := countFiles(t, objects); total != 1 {
		t.Fatalf("the object directory holds %d files, want 1", total)
	}
}

func TestWriterReportsTemporaryFileFailures(t *testing.T) {
	db := openDB(t, newObjectsDir(t), Options{})
	swapRootCreate(t, errInjected)
	if _, err := db.Writer(object.TypeBlob, 4); !errors.Is(err, errInjected) {
		t.Fatalf("Writer returned %v, want %v", err, errInjected)
	}
}

func TestWriterReportsStreamFailures(t *testing.T) {
	objects := newObjectsDir(t)
	db := openDB(t, objects, Options{})
	payload := make([]byte, 1<<20)
	if _, err := rand.Read(payload); err != nil {
		t.Fatalf("Read returned error %v", err)
	}
	writer, err := db.Writer(object.TypeBlob, int64(2*len(payload)))
	if err != nil {
		t.Fatalf("Writer returned error %v", err)
	}
	swapFileWrite(t, errInjected)
	var failure error
	for range 2 {
		if _, failure = writer.Write(payload); failure != nil {
			break
		}
	}
	if !errors.Is(failure, errInjected) {
		t.Fatalf("Write returned %v, want %v", failure, errInjected)
	}
	if err := writer.Close(); err == nil {
		t.Fatal("Close accepted a broken stream")
	}
	if total := countFiles(t, objects); total != 0 {
		t.Fatalf("a broken writer left %d files behind", total)
	}
}

func TestWriterReportsLookupFailures(t *testing.T) {
	db := openDB(t, newObjectsDir(t), Options{})
	writer, err := db.Writer(object.TypeBlob, 4)
	if err != nil {
		t.Fatalf("Writer returned error %v", err)
	}
	if _, err := writer.Write([]byte("data")); err != nil {
		t.Fatalf("Write returned error %v", err)
	}
	swapRootStat(t, always, errInjected)
	if err := writer.Close(); !errors.Is(err, errInjected) {
		t.Fatalf("Close returned %v, want %v", err, errInjected)
	}
}

func TestWrittenObjectsMatchTheLooseFormat(t *testing.T) {
	objects := newObjectsDir(t)
	db := openDB(t, objects, Options{})
	payload := []byte("loose format\n")
	id, err := db.Put(object.TypeBlob, payload)
	if err != nil {
		t.Fatalf("Put returned error %v", err)
	}
	stored := readFile(t, filepath.Join(objects, looseName(id)))
	kind, data, err := decodeLoose(bytes.NewReader(stored))
	if err != nil || kind != object.TypeBlob || !bytes.Equal(data, payload) {
		t.Fatalf("decodeLoose gave (%s, %q, %v)", kind, data, err)
	}
	if !slices.Equal(stored, compressLoose(object.TypeBlob, payload)) {
		t.Fatal("the stored bytes differ from the compressed form")
	}
}
