package hooks

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"golang.org/x/sys/windows/registry"
)

func TestRootsOfGitCoverTheGitForWindowsLayouts(t *testing.T) {
	cases := map[string][]string{
		`C:\Git\cmd\git.exe`:         {`C:\Git`},
		`C:\Git\bin\git.exe`:         {`C:\Git`, `C:\`},
		`C:\Git\mingw64\bin\git.exe`: {`C:\Git\mingw64`, `C:\Git`},
	}
	for git, want := range cases {
		if got := rootsOfGit(git); !slices.Equal(got, want) {
			t.Fatalf("%s: roots = %q, want %q", git, got, want)
		}
	}
}

func TestHasShellLooksInUsrBinAndBin(t *testing.T) {
	usr := fakeGitRoot(t, `usr\bin\sh.exe`)
	bin := fakeGitRoot(t, `bin\sh.exe`)
	empty := t.TempDir()
	directory := t.TempDir()
	if err := os.MkdirAll(filepath.Join(directory, "bin", "sh.exe"), 0o777); err != nil {
		t.Fatalf("MkdirAll returned error %v", err)
	}

	if !hasShell(usr) || !hasShell(bin) || hasShell(empty) || hasShell(directory) {
		t.Fatalf("hasShell = %v %v %v %v", hasShell(usr), hasShell(bin), hasShell(empty), hasShell(directory))
	}
}

func TestFindGitForWindowsPrefersTheRegistryAndFallsBackToGitOnPath(t *testing.T) {
	withoutShell := t.TempDir()
	registered := fakeGitRoot(t, `usr\bin\sh.exe`)
	onPath := fakeGitRoot(t, `bin\sh.exe`)

	swap(t, &registryInstallPath, func(key registry.Key) string {
		if key == registry.LOCAL_MACHINE {
			return withoutShell
		}
		return registered
	})
	swap(t, &lookPath, func(string) (string, error) { return filepath.Join(onPath, "cmd", "git.exe"), nil })
	if got := findGitForWindows(); got != registered {
		t.Fatalf("root = %q, want the registered installation %q", got, registered)
	}

	swap(t, &registryInstallPath, func(registry.Key) string { return "" })
	if got := findGitForWindows(); got != onPath {
		t.Fatalf("root = %q, want the installation of git on PATH %q", got, onPath)
	}

	swap(t, &lookPath, func(string) (string, error) { return "", errors.New("not found") })
	if got := findGitForWindows(); got != "" {
		t.Fatalf("root = %q, want none", got)
	}
}

func TestRegistryStringReadsOnlyExistingValues(t *testing.T) {
	const currentVersion = `SOFTWARE\Microsoft\Windows NT\CurrentVersion`
	if got := registryString(registry.LOCAL_MACHINE, currentVersion, "ProductName"); got == "" {
		t.Fatal("ProductName must be readable")
	}
	if got := registryString(registry.LOCAL_MACHINE, currentVersion, "GoGitMissingValue"); got != "" {
		t.Fatalf("missing value = %q", got)
	}
	if got := registryString(registry.CURRENT_USER, `SOFTWARE\GoGitMissingKey`, installPathValue); got != "" {
		t.Fatalf("missing key = %q", got)
	}
	_ = registryInstallPath(registry.CURRENT_USER)
}
