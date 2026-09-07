package credential

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
)

func stubLookPath(t *testing.T, found map[string]string) {
	t.Helper()
	restore := lookPath
	lookPath = func(name string) (string, error) {
		if path, ok := found[name]; ok {
			return path, nil
		}
		return "", errors.New("not found")
	}
	helperDirectories = sync.OnceValue(gitHelperDirectories)
	t.Cleanup(func() {
		lookPath = restore
		helperDirectories = sync.OnceValue(gitHelperDirectories)
	})
}

func TestHelperOnThePathIsUsedAsIs(t *testing.T) {
	stubLookPath(t, map[string]string{"git-credential-store": "/usr/bin/git-credential-store"})

	path, ok := findHelperExecutable("git-credential-store")

	if !ok || path != "/usr/bin/git-credential-store" {
		t.Fatalf("findHelperExecutable = %q,%v", path, ok)
	}
}

func TestHelperShippedWithGitIsFoundNextToIt(t *testing.T) {
	root := t.TempDir()
	binDir := filepath.Join(root, filepath.FromSlash(helperDirsRelativeToGit[0]))
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(binDir, "git-credential-manager"+helperExeSuffix)
	if err := os.WriteFile(exe, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	gitExe := filepath.Join(root, "cmd", "git"+helperExeSuffix)
	stubLookPath(t, map[string]string{"git": gitExe})

	path, ok := findHelperExecutable("git-credential-manager")

	if !ok {
		t.Fatal("a helper shipped with git must be found even when it is off the PATH")
	}
	if path != exe {
		t.Fatalf("found %q, want %q", path, exe)
	}
}

func TestAHelperThatIsNowhereIsLeftToTheOperatingSystem(t *testing.T) {
	stubLookPath(t, nil)

	if _, ok := findHelperExecutable("git-credential-nothing"); ok {
		t.Fatal("a helper that exists nowhere must not be reported as found")
	}
}

func TestGitInstallRootStripsTheBinaryDirectory(t *testing.T) {
	cases := map[string]string{
		filepath.FromSlash("/opt/git/cmd"):          filepath.FromSlash("/opt/git"),
		filepath.FromSlash("/opt/git/mingw64/bin"):  filepath.FromSlash("/opt/git"),
		filepath.FromSlash("/opt/git/usr/bin"):      filepath.FromSlash("/opt/git"),
		filepath.FromSlash("/opt/git"):              filepath.FromSlash("/opt/git"),
		filepath.FromSlash("/opt/git/mingw32/bin"):  filepath.FromSlash("/opt/git"),
		filepath.FromSlash("/opt/somewhere/custom"): filepath.FromSlash("/opt/somewhere/custom"),
	}
	for in, want := range cases {
		if got := gitInstallRoot(in); got != want {
			t.Fatalf("gitInstallRoot(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestWellKnownDirectoriesAreSearchedWhenGitIsNotOnThePath(t *testing.T) {
	stubLookPath(t, nil)

	dirs := helperDirectories()

	if len(dirs) == 0 {
		t.Fatal("the standard install locations must still be searched")
	}
	if runtime.GOOS == "windows" && os.Getenv("ProgramFiles") == "" {
		t.Skip("ProgramFiles is not set in this environment")
	}
}

func TestFixedDirectoriesSkipRootsTheSystemDoesNotDeclare(t *testing.T) {
	for _, name := range []string{"ProgramFiles", "ProgramFiles(x86)", "LOCALAPPDATA"} {
		t.Setenv(name, "")
	}

	if dirs := fixedHelperDirs(); len(dirs) != 0 && runtime.GOOS == "windows" {
		t.Fatalf("dirs = %v, want none when no install root is declared", dirs)
	}
}
