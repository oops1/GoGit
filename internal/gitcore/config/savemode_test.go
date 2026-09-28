package config

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func modeOfANewFile(t *testing.T, dir string) fs.FileMode {
	t.Helper()
	probe := filepath.Join(dir, "probe")
	fh, err := os.OpenFile(probe, os.O_WRONLY|os.O_CREATE|os.O_EXCL, newFileMode)
	if err != nil {
		t.Fatalf("OpenFile returned error %v", err)
	}
	if err := fh.Close(); err != nil {
		t.Fatalf("Close returned error %v", err)
	}
	info, err := os.Stat(probe)
	if err != nil {
		t.Fatalf("Stat returned error %v", err)
	}
	return info.Mode().Perm()
}

func modeOf(t *testing.T, path string) fs.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat returned error %v", err)
	}
	return info.Mode().Perm()
}

func TestSaveGivesANewFileTheSamePermissionsAsTheProcessMask(t *testing.T) {
	dir := t.TempDir()
	want := modeOfANewFile(t, dir)
	path := filepath.Join(dir, "config")
	if err := mustParse(t, "[a]\n\tb = 1\n").Save(path); err != nil {
		t.Fatalf("Save returned error %v", err)
	}
	if got := modeOf(t, path); got != want {
		t.Fatalf("saved config has mode %v, want %v", got, want)
	}
}

func TestSaveKeepsThePermissionsOfAFileThatIsAlreadyThere(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows keeps only the read-only bit, so group and other bits cannot be checked")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	writeFile(t, path, "[a]\n\tb = 1\n")
	if err := os.Chmod(path, 0o664); err != nil {
		t.Fatalf("Chmod returned error %v", err)
	}
	if err := mustParse(t, "[a]\n\tb = 2\n").Save(path); err != nil {
		t.Fatalf("Save returned error %v", err)
	}
	if got := modeOf(t, path); got != 0o664 {
		t.Fatalf("saved config has mode %v, want %v", got, fs.FileMode(0o664))
	}
}

func TestSaveKeepsAConfigThatOnlyItsOwnerMayRead(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows keeps only the read-only bit, so group and other bits cannot be checked")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	writeFile(t, path, "[a]\n\tb = 1\n")
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatalf("Chmod returned error %v", err)
	}
	if err := mustParse(t, "[a]\n\tb = 2\n").Save(path); err != nil {
		t.Fatalf("Save returned error %v", err)
	}
	if got := modeOf(t, path); got != 0o600 {
		t.Fatalf("saved config has mode %v, want %v", got, fs.FileMode(0o600))
	}
}
