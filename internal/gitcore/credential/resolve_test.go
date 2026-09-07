package credential

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

type fakeHelperFileInfo struct {
	isDir bool
}

func (f fakeHelperFileInfo) Name() string       { return "" }
func (f fakeHelperFileInfo) Size() int64        { return 0 }
func (f fakeHelperFileInfo) Mode() fs.FileMode  { return 0 }
func (f fakeHelperFileInfo) ModTime() time.Time { return time.Time{} }
func (f fakeHelperFileInfo) IsDir() bool        { return f.isDir }
func (f fakeHelperFileInfo) Sys() any           { return nil }

func stubStatHelperCandidate(t *testing.T, fn func(string) (os.FileInfo, error)) {
	t.Helper()
	restore := statHelperCandidate
	statHelperCandidate = fn
	t.Cleanup(func() { statHelperCandidate = restore })
}

func TestResolveHelperEmptyValueIsUnsupported(t *testing.T) {
	if _, err := resolveHelper(""); !errors.Is(err, ErrUnsupportedHelper) {
		t.Fatalf("resolveHelper(\"\") returned %v, want ErrUnsupportedHelper", err)
	}
}

func TestResolveHelperShellCommandIsUnsupported(t *testing.T) {
	if _, err := resolveHelper("!true"); !errors.Is(err, ErrUnsupportedHelper) {
		t.Fatalf("resolveHelper returned %v, want ErrUnsupportedHelper", err)
	}
}

func TestResolveHelperValueWithSpacesIsUnsupported(t *testing.T) {
	if _, err := resolveHelper("foo --with-arg"); !errors.Is(err, ErrUnsupportedHelper) {
		t.Fatalf("resolveHelper returned %v, want ErrUnsupportedHelper", err)
	}
}

func TestResolveHelperPathWithSpacesRunsDirectlyWhenFileExists(t *testing.T) {
	const path = `C:\Program Files\Git\mingw64\libexec\git-core\git-credential-manager.exe`
	stubStatHelperCandidate(t, func(name string) (os.FileInfo, error) {
		if name != path {
			t.Fatalf("statHelperCandidate called with %q, want %q", name, path)
		}
		return fakeHelperFileInfo{}, nil
	})
	h, err := resolveHelper(path)
	if err != nil {
		t.Fatalf("resolveHelper returned %v", err)
	}
	eh, ok := h.(*execHelper)
	if !ok {
		t.Fatalf("resolveHelper returned %T, want *execHelper", h)
	}
	if eh.exe != path {
		t.Fatalf("exe = %q, want %q", eh.exe, path)
	}
	if eh.Name() != path {
		t.Fatalf("Name() = %q, want %q", eh.Name(), path)
	}
}

func TestResolveHelperPathWithSpacesIsUnsupportedWhenFileMissing(t *testing.T) {
	stubStatHelperCandidate(t, func(string) (os.FileInfo, error) {
		return nil, os.ErrNotExist
	})
	_, err := resolveHelper(`C:\Program Files\Missing\git-credential-manager.exe`)
	if !errors.Is(err, ErrUnsupportedHelper) {
		t.Fatalf("resolveHelper returned %v, want ErrUnsupportedHelper", err)
	}
}

func TestResolveHelperPathWithSpacesIsUnsupportedWhenPathIsDirectory(t *testing.T) {
	stubStatHelperCandidate(t, func(string) (os.FileInfo, error) {
		return fakeHelperFileInfo{isDir: true}, nil
	})
	_, err := resolveHelper(`C:\Program Files\Git`)
	if !errors.Is(err, ErrUnsupportedHelper) {
		t.Fatalf("resolveHelper returned %v, want ErrUnsupportedHelper", err)
	}
}

func TestResolveHelperShellCommandIsUnsupportedEvenWhenFileExists(t *testing.T) {
	stubStatHelperCandidate(t, func(string) (os.FileInfo, error) {
		return fakeHelperFileInfo{}, nil
	})
	_, err := resolveHelper(`!C:\Program Files\Git\git-credential-manager.exe`)
	if !errors.Is(err, ErrUnsupportedHelper) {
		t.Fatalf("resolveHelper returned %v, want ErrUnsupportedHelper", err)
	}
}

func TestResolveHelperCacheIsUnsupported(t *testing.T) {
	if _, err := resolveHelper("cache"); !errors.Is(err, ErrUnsupportedHelper) {
		t.Fatalf("resolveHelper(cache) returned %v, want ErrUnsupportedHelper", err)
	}
	if _, err := resolveHelper("cache --timeout=60"); !errors.Is(err, ErrUnsupportedHelper) {
		t.Fatalf("resolveHelper(cache --timeout) returned %v, want ErrUnsupportedHelper", err)
	}
}

func TestResolveHelperNameResolvesViaPath(t *testing.T) {
	stubLookPath(t, nil)
	stubStatHelperCandidate(t, func(string) (os.FileInfo, error) { return nil, os.ErrNotExist })
	h, err := resolveHelper("manager")
	if err != nil {
		t.Fatalf("resolveHelper returned %v", err)
	}
	eh, ok := h.(*execHelper)
	if !ok {
		t.Fatalf("resolveHelper returned %T, want *execHelper", h)
	}
	if eh.Name() != "manager" {
		t.Fatalf("Name() = %q, want %q", eh.Name(), "manager")
	}
	if eh.exe != "git-credential-manager" {
		t.Fatalf("exe = %q, want %q", eh.exe, "git-credential-manager")
	}
}

func TestResolveHelperAbsolutePathIsUsedDirectly(t *testing.T) {
	abs := "/usr/local/bin/foo"
	if runtime.GOOS == "windows" {
		abs = `C:\Tools\foo.exe`
	}
	h, err := resolveHelper(abs)
	if err != nil {
		t.Fatalf("resolveHelper returned %v", err)
	}
	eh := h.(*execHelper)
	if eh.exe != abs {
		t.Fatalf("exe = %q, want %q", eh.exe, abs)
	}
}

func TestResolveHelperStoreDefaultPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	h, err := resolveHelper("store")
	if err != nil {
		t.Fatalf("resolveHelper returned %v", err)
	}
	sh := h.(*storeHelper)
	want := filepath.Join(home, ".git-credentials")
	if sh.path != want {
		t.Fatalf("path = %q, want %q", sh.path, want)
	}
	if sh.Name() != "store" {
		t.Fatalf("Name() = %q, want %q", sh.Name(), "store")
	}
}

func TestResolveHelperStoreExplicitFile(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "creds.txt")
	h, err := resolveHelper("store --file=" + target)
	if err != nil {
		t.Fatalf("resolveHelper returned %v", err)
	}
	sh := h.(*storeHelper)
	if sh.path != target {
		t.Fatalf("path = %q, want %q", sh.path, target)
	}
}

func TestResolveHelperStoreDefaultPathFailsWithoutHome(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
	t.Setenv("HOMEDRIVE", "")
	t.Setenv("HOMEPATH", "")
	if _, err := resolveHelper("store"); err == nil {
		t.Fatalf("resolveHelper(store) succeeded despite no home directory being available")
	}
}

func TestResolveHelperStoreInvalidFileExpansionFails(t *testing.T) {
	if _, err := resolveHelper("store --file=~badname"); err == nil {
		t.Fatalf("resolveHelper succeeded despite an unexpandable ~ prefix")
	}
}

func TestResolveHelperStoreExpandsHomeInFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	h, err := resolveHelper(`store --file=~/creds.txt`)
	if err != nil {
		t.Fatalf("resolveHelper returned %v", err)
	}
	sh := h.(*storeHelper)
	want := filepath.Join(home, "creds.txt")
	if sh.path != want {
		t.Fatalf("path = %q, want %q", sh.path, want)
	}
}
