package pack

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
)

func writeFileFixture() (*fakeSource, []hash.ObjectID) {
	src := newFakeSource()
	ids := []hash.ObjectID{
		src.add(object.TypeBlob, similarBlob(1, 40)),
		src.add(object.TypeBlob, similarBlob(3, 40)),
		src.add(object.TypeBlob, []byte("a small blob\n")),
	}
	return src, ids
}

func dirNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir returned error %v", err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

func TestWritePackFileLeavesAPackAndItsIndexUnderTheChecksumName(t *testing.T) {
	src, ids := writeFileFixture()
	dir := t.TempDir()

	result, err := WritePackFile(t.Context(), dir, src, ids, WriteOptions{Window: 10, Depth: 50})
	if err != nil {
		t.Fatalf("WritePackFile returned error %v", err)
	}

	base := filepath.Join(dir, "pack-"+result.Checksum.String())
	if result.PackPath != base+packSuffix || result.IndexPath != base+indexSuffix || result.Objects != len(ids) || result.Bytes == 0 {
		t.Fatalf("result = %+v", result)
	}
	if names := dirNames(t, dir); len(names) != 2 {
		t.Fatalf("directory holds %v, want only the pack and its index", names)
	}
	store, err := Open(dir)
	if err != nil {
		t.Fatalf("Open returned error %v", err)
	}
	defer func() { _ = store.Close() }()
	for _, id := range ids {
		_, data, ok, err := store.Get(id)
		if err != nil || !ok || !bytes.Equal(data, src.objects[id].data) {
			t.Fatalf("object %s: ok = %v, err = %v", id, ok, err)
		}
	}
}

func TestWritePackFileReusesAnIdenticalPackAlreadyInPlace(t *testing.T) {
	src, ids := writeFileFixture()
	dir := t.TempDir()
	first, err := WritePackFile(t.Context(), dir, src, ids, WriteOptions{})
	if err != nil {
		t.Fatalf("WritePackFile returned error %v", err)
	}

	second, err := WritePackFile(t.Context(), dir, src, ids, WriteOptions{})
	if err != nil {
		t.Fatalf("second WritePackFile returned error %v", err)
	}

	if second.Checksum != first.Checksum || len(dirNames(t, dir)) != 2 {
		t.Fatalf("first = %+v, second = %+v, directory = %v", first, second, dirNames(t, dir))
	}
}

func TestWritePackFileReportsAMissingDirectory(t *testing.T) {
	src, ids := writeFileFixture()

	if _, err := WritePackFile(t.Context(), filepath.Join(t.TempDir(), "absent"), src, ids, WriteOptions{}); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("err = %v, want os.ErrNotExist", err)
	}
}

func TestWritePackFileReportsAnObjectItCannotReadAndLeavesNothingBehind(t *testing.T) {
	src, ids := writeFileFixture()
	boom := errors.New("boom")
	src.failNextGet(ids[1], boom)
	dir := t.TempDir()

	if _, err := WritePackFile(t.Context(), dir, src, ids, WriteOptions{}); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom", err)
	}
	if names := dirNames(t, dir); len(names) != 0 {
		t.Fatalf("left behind %v", names)
	}
}

func TestWritePackFileReportsAFailedCloseAndLeavesNothingBehind(t *testing.T) {
	src, ids := writeFileFixture()
	boom := errors.New("boom")
	prev := fileClose
	fileClose = func(file *os.File) error { _ = file.Close(); return boom }
	t.Cleanup(func() { fileClose = prev })
	dir := t.TempDir()

	if _, err := WritePackFile(t.Context(), dir, src, ids, WriteOptions{}); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom", err)
	}
	if names := dirNames(t, dir); len(names) != 0 {
		t.Fatalf("left behind %v", names)
	}
}

func TestWritePackFileReportsBlockedDestinations(t *testing.T) {
	src, ids := writeFileFixture()
	probe, err := WritePackFile(t.Context(), t.TempDir(), src, ids, WriteOptions{})
	if err != nil {
		t.Fatalf("WritePackFile returned error %v", err)
	}
	for _, suffix := range []string{packSuffix, indexSuffix} {
		t.Run(suffix, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.Mkdir(filepath.Join(dir, "pack-"+probe.Checksum.String()+suffix), 0o755); err != nil {
				t.Fatalf("Mkdir returned error %v", err)
			}

			if _, err := WritePackFile(t.Context(), dir, src, ids, WriteOptions{}); err == nil {
				t.Fatalf("WritePackFile succeeded with a directory in place of the %s file", suffix)
			}
		})
	}
}
