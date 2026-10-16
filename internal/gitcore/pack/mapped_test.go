package pack

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func mappedFileWith(t *testing.T, content []byte) (*mappedFile, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "data")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("WriteFile returned error %v", err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("Open returned error %v", err)
	}
	mapped, err := newMappedFile(file, int64(len(content)))
	if err != nil {
		_ = file.Close()
		t.Fatalf("newMappedFile returned error %v", err)
	}
	t.Cleanup(func() { _ = mapped.Close() })
	return mapped, path
}

func TestMappedFileReadsLikeAFile(t *testing.T) {
	mapped, _ := mappedFileWith(t, []byte("0123456789"))

	whole := make([]byte, 4)
	if n, err := mapped.ReadAt(whole, 3); n != 4 || err != nil || string(whole) != "3456" {
		t.Fatalf("ReadAt inside the file = %d, %v, %q", n, err, whole)
	}
	tail := make([]byte, 8)
	if n, err := mapped.ReadAt(tail, 6); n != 4 || !errors.Is(err, io.EOF) || string(tail[:n]) != "6789" {
		t.Fatalf("ReadAt across the end = %d, %v, %q", n, err, tail[:n])
	}
	if n, err := mapped.ReadAt(tail, 10); n != 0 || !errors.Is(err, io.EOF) {
		t.Fatalf("ReadAt at the end = %d, %v, want EOF", n, err)
	}
	if n, err := mapped.ReadAt(tail, 99); n != 0 || !errors.Is(err, io.EOF) {
		t.Fatalf("ReadAt past the end = %d, %v, want EOF", n, err)
	}
	if n, err := mapped.ReadAt(nil, 10); n != 0 || err != nil {
		t.Fatalf("an empty ReadAt at the end = %d, %v, want nothing", n, err)
	}
	if _, err := mapped.ReadAt(tail, -1); err == nil {
		t.Fatal("ReadAt accepted a negative offset")
	}
}

func TestMappedFileServesManyReadersAtOnce(t *testing.T) {
	content := bytes.Repeat([]byte("abcdefghij"), 1000)
	mapped, _ := mappedFileWith(t, content)
	var wg sync.WaitGroup
	for worker := range 8 {
		wg.Go(func() {
			buffer := make([]byte, 100)
			for round := range 200 {
				offset := int64((worker*37 + round*91) % (len(content) - len(buffer)))
				if n, err := mapped.ReadAt(buffer, offset); n != len(buffer) || err != nil || !bytes.Equal(buffer, content[offset:offset+100]) {
					t.Errorf("ReadAt(%d) = %d, %v", offset, n, err)
					return
				}
			}
		})
	}
	wg.Wait()
}

func TestMappedFileCloseUnmapsOnceAndReportsAClosedFile(t *testing.T) {
	mapped, _ := mappedFileWith(t, []byte("data"))

	if err := mapped.Close(); err != nil {
		t.Fatalf("Close returned error %v", err)
	}
	if err := mapped.Close(); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("a second Close returned %v, want os.ErrClosed", err)
	}
	if _, err := mapped.ReadAt(make([]byte, 1), 0); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("ReadAt after Close returned %v, want os.ErrClosed", err)
	}
}

func TestMappedFileCloseReportsWhatTheUnmappingAndTheFileReport(t *testing.T) {
	boom := errors.New("unmapping failed")
	path := filepath.Join(t.TempDir(), "data")
	if err := os.WriteFile(path, []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	previous := mapFile
	mapFile = func(file *os.File, size int64) ([]byte, func() error, error) {
		data, release, err := previous(file, size)
		if err != nil {
			return nil, nil, err
		}
		return data, func() error { return errors.Join(release(), boom) }, nil
	}
	t.Cleanup(func() { mapFile = previous })
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	mapped, err := newMappedFile(file, 4)
	if err != nil {
		t.Fatal(err)
	}
	if err := mapped.Close(); !errors.Is(err, boom) {
		t.Fatalf("Close returned %v, want the unmapping failure", err)
	}

	other, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	again, err := newMappedFile(other, 4)
	if err != nil {
		t.Fatal(err)
	}
	if err := other.Close(); err != nil {
		t.Fatal(err)
	}
	if err := again.Close(); err == nil {
		t.Fatalf("Close of a mapping whose file is already closed returned %v, want the file failure", err)
	}
}

func TestAFileThatCannotBeMappedIsReadThroughTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data")
	if err := os.WriteFile(path, []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })
	previous := mapFile
	mapFile = func(*os.File, int64) ([]byte, func() error, error) { return nil, nil, errors.New("no mapping here") }
	t.Cleanup(func() { mapFile = previous })

	source, closer := sourceOf(file, 4)

	if source != io.ReaderAt(file) || closer != io.Closer(file) {
		t.Fatalf("sourceOf returned %T and %T, want the file itself", source, closer)
	}
}

func TestAnEmptyFileCannotBeMapped(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })

	if _, err := newMappedFile(file, 0); err == nil {
		t.Fatal("an empty file was mapped")
	}
}

func TestPackfilesAndIndexesAreMappedWhenThePlatformAllows(t *testing.T) {
	name := fixtureNames(t)[0]
	dir := copyFixtureDir(t, name)

	packfile, err := OpenPack(filepath.Join(dir, name+packSuffix))
	if err != nil {
		t.Fatalf("OpenPack returned error %v", err)
	}
	t.Cleanup(func() { _ = packfile.Close() })
	index, err := OpenIndex(filepath.Join(dir, name+indexSuffix))
	if err != nil {
		t.Fatalf("OpenIndex returned error %v", err)
	}
	t.Cleanup(func() { _ = index.Close() })

	if _, ok := packfile.source.(*mappedFile); !ok {
		t.Errorf("the packfile is read through %T, want a mapping", packfile.source)
	}
	if _, ok := index.source.(*mappedFile); !ok {
		t.Errorf("the index is read through %T, want a mapping", index.source)
	}
}

func TestPackfilesStillOpenWhenMappingFails(t *testing.T) {
	name := fixtureNames(t)[0]
	dir := copyFixtureDir(t, name)
	previous := mapFile
	mapFile = func(*os.File, int64) ([]byte, func() error, error) { return nil, nil, errors.New("no mapping here") }
	t.Cleanup(func() { mapFile = previous })

	store := openStore(t, dir)

	if _, _, ok, err := store.Get(objectTypesFirstID(t)); !ok || err != nil {
		t.Fatalf("Get returned (%v, %v) from a packfile read through the file", ok, err)
	}
}
