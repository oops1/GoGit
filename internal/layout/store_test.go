package layout

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadReturnsNilWhenFileMissing(t *testing.T) {
	var s Store
	data, err := s.Load(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil {
		t.Fatal(err)
	}
	if data != nil {
		t.Fatalf("data = %v, want nil", data)
	}
}

func TestLoadReturnsStoredBytes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "layout.json")
	if err := os.WriteFile(path, []byte(`{"sizes":[1,2,3,4]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var s Store
	data, err := s.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"sizes":[1,2,3,4]}` {
		t.Fatalf("data = %s", data)
	}
}

func TestLoadFailsWhenPathIsDirectory(t *testing.T) {
	var s Store
	if _, err := s.Load(t.TempDir()); err == nil {
		t.Fatal("expected error")
	}
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "layout.json")
	var s Store
	if err := s.Save(path, []byte("data")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + ".tmp"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("tmp file left behind: %v", err)
	}
	data, err := s.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "data" {
		t.Fatalf("data = %s", data)
	}
}

func TestSaveFailsWhenDirIsFile(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "file")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	var s Store
	if err := s.Save(filepath.Join(blocker, "layout.json"), []byte("x")); err == nil {
		t.Fatal("expected error")
	}
}

func TestSaveFailsWhenTargetIsDirectory(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "layout.json")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	var s Store
	if err := s.Save(target, []byte("x")); err == nil {
		t.Fatal("expected rename error")
	}
}

func TestSaveFailsWhenTempIsDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "layout.json.tmp"), 0o700); err != nil {
		t.Fatal(err)
	}
	var s Store
	if err := s.Save(filepath.Join(dir, "layout.json"), []byte("x")); err == nil {
		t.Fatal("expected write error")
	}
}
