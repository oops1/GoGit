package attributes

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

var errLoader = errors.New("loader failed")

func failingLoader(names ...string) Loader {
	return LoaderFunc(func(path string) ([]byte, error) {
		if slices.Contains(names, path) {
			return nil, errLoader
		}
		return nil, fs.ErrNotExist
	})
}

func TestRootLoaderReadsInsideRootOnly(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o700); err != nil {
		t.Fatalf("MkdirAll returned error %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", ".gitignore"), []byte("x\n"), 0o600); err != nil {
		t.Fatalf("WriteFile returned error %v", err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatalf("OpenRoot returned error %v", err)
	}
	defer root.Close()
	loader := RootLoader(root)
	data, err := loader.ReadFile("sub/.gitignore")
	if err != nil || string(data) != "x\n" {
		t.Fatalf("ReadFile returned (%q, %v), want (%q, nil)", data, err, "x\n")
	}
	if _, err := loader.ReadFile("../outside"); err == nil {
		t.Fatal("ReadFile escaped the root")
	}
}

func TestOSLoaderResolvesRelativePathsAgainstBase(t *testing.T) {
	dir := t.TempDir()
	absolute := filepath.Join(dir, "absolute")
	for name, text := range map[string]string{"relative": "r", "absolute": "a"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o600); err != nil {
			t.Fatalf("WriteFile(%q) returned error %v", name, err)
		}
	}
	if data, err := OSLoader(dir).ReadFile("relative"); err != nil || string(data) != "r" {
		t.Fatalf("relative read returned (%q, %v), want (%q, nil)", data, err, "r")
	}
	if data, err := OSLoader(dir).ReadFile(filepath.ToSlash(absolute)); err != nil || string(data) != "a" {
		t.Fatalf("absolute read returned (%q, %v), want (%q, nil)", data, err, "a")
	}
	if _, err := OSLoader("").ReadFile(filepath.ToSlash(absolute)); err != nil {
		t.Fatalf("read without a base returned error %v", err)
	}
}

func TestMemoryLoaderReportsMissingFiles(t *testing.T) {
	loader := MemoryLoader(map[string]string{"a": "1"})
	if data, err := loader.ReadFile("a"); err != nil || string(data) != "1" {
		t.Fatalf("ReadFile(a) returned (%q, %v), want (%q, nil)", data, err, "1")
	}
	if _, err := loader.ReadFile("b"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("ReadFile(b) returned %v, want fs.ErrNotExist", err)
	}
}

func TestFileCacheReadsEachFileOnce(t *testing.T) {
	reads := 0
	cache := newFileCache(LoaderFunc(func(string) ([]byte, error) {
		reads++
		return []byte("a\nb\n"), nil
	}), func(source string, data []byte) []Rule {
		return parseIgnoreFile(source, "", data)
	})
	for range 3 {
		rules, err := cache.get(".gitignore")
		if err != nil || len(rules) != 2 {
			t.Fatalf("get returned (%d rules, %v), want (2 rules, nil)", len(rules), err)
		}
	}
	if reads != 1 {
		t.Fatalf("loader was called %d times, want 1", reads)
	}
}

func TestFileCacheTreatsMissingFilesAsEmpty(t *testing.T) {
	cache := newFileCache(MemoryLoader(nil), func(source string, data []byte) []Rule {
		return parseIgnoreFile(source, "", data)
	})
	rules, err := cache.get("absent")
	if err != nil || rules != nil {
		t.Fatalf("get returned (%v, %v), want (nil, nil)", rules, err)
	}
}

func TestFileCacheReportsUnexpectedErrors(t *testing.T) {
	cache := newFileCache(failingLoader("broken"), func(source string, data []byte) []Rule {
		return parseIgnoreFile(source, "", data)
	})
	if _, err := cache.get("broken"); !errors.Is(err, errLoader) {
		t.Fatalf("get returned %v, want %v", err, errLoader)
	}
	if _, err := cache.get("broken"); !errors.Is(err, errLoader) {
		t.Fatalf("cached get returned %v, want %v", err, errLoader)
	}
}

func TestFileCacheSkipsEmptyNamesAndMissingLoaders(t *testing.T) {
	parse := func(source string, data []byte) []Rule { return parseIgnoreFile(source, "", data) }
	cache := newFileCache(nil, parse)
	if rules, err := cache.get("anything"); err != nil || rules != nil {
		t.Fatalf("get without a loader returned (%v, %v), want (nil, nil)", rules, err)
	}
	cache = newFileCache(MemoryLoader(map[string]string{"": "x\n"}), parse)
	if rules, err := cache.get(""); err != nil || rules != nil {
		t.Fatalf("get of an empty name returned (%v, %v), want (nil, nil)", rules, err)
	}
}
