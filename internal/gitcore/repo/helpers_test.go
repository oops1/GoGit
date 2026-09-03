package repo

import (
	"os"
	"path/filepath"
	"testing"
)

type env map[string]string

func (e env) get(key string) string { return e[key] }

func tempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("EvalSymlinks returned error %v", err)
	}
	return dir
}

func volumeRoot(path string) string {
	return filepath.VolumeName(path) + string(filepath.Separator)
}

func writeFile(t *testing.T, path, text string) string {
	t.Helper()
	makeDir(t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte(text), 0o666); err != nil {
		t.Fatalf("WriteFile(%q) returned error %v", path, err)
	}
	return path
}

func makeDir(t *testing.T, path string) string {
	t.Helper()
	if err := os.MkdirAll(path, 0o777); err != nil {
		t.Fatalf("MkdirAll(%q) returned error %v", path, err)
	}
	return path
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) returned error %v", path, err)
	}
	return string(data)
}

func emptyGlobalConfig(t *testing.T) string {
	t.Helper()
	return writeFile(t, filepath.Join(t.TempDir(), "gitconfig"), "")
}

func openOptions(t *testing.T, vars env) OpenOptions {
	t.Helper()
	return OpenOptions{Env: vars.get, NoSystem: true, GlobalFile: emptyGlobalConfig(t)}
}

func initOptions(t *testing.T, vars env) InitOptions {
	t.Helper()
	return InitOptions{Env: vars.get, NoSystem: true, GlobalFile: emptyGlobalConfig(t)}
}

func plainGitDir(t *testing.T, dir string) string {
	t.Helper()
	makeDir(t, filepath.Join(dir, objectsDirName))
	makeDir(t, filepath.Join(dir, refsDirName))
	writeFile(t, filepath.Join(dir, headFile), headRefPrefix+"master\n")
	return dir
}

func initRepo(t *testing.T, path string, opts InitOptions) *Repository {
	t.Helper()
	repository, err := Init(path, opts)
	if err != nil {
		t.Fatalf("Init(%q) returned error %v", path, err)
	}
	t.Cleanup(func() {
		if err := repository.Close(); err != nil {
			t.Errorf("Close returned error %v", err)
		}
	})
	return repository
}

func openRepo(t *testing.T, path string, opts OpenOptions) *Repository {
	t.Helper()
	repository, err := Open(path, opts)
	if err != nil {
		t.Fatalf("Open(%q) returned error %v", path, err)
	}
	t.Cleanup(func() {
		if err := repository.Close(); err != nil {
			t.Errorf("Close returned error %v", err)
		}
	})
	return repository
}

func mustDiscover(t *testing.T, start string, vars env) Layout {
	t.Helper()
	layout, err := Discover(start, DiscoverOptions{Env: vars.get})
	if err != nil {
		t.Fatalf("Discover(%q) returned error %v", start, err)
	}
	return layout
}

func linkedWorktree(t *testing.T, base, name string) (main, worktree, gitDir string) {
	t.Helper()
	main = plainGitDir(t, filepath.Join(base, "main", dotGit))
	worktree = makeDir(t, filepath.Join(base, name))
	gitDir = filepath.Join(main, worktreesDirName, name)
	writeFile(t, filepath.Join(gitDir, headFile), headRefPrefix+name+"\n")
	writeFile(t, filepath.Join(gitDir, commonDirFile), "../..\n")
	writeFile(t, filepath.Join(gitDir, gitDirFile), filepath.ToSlash(filepath.Join(worktree, dotGit))+"\n")
	writeFile(t, filepath.Join(worktree, dotGit), gitFilePrefix+" "+filepath.ToSlash(gitDir)+"\n")
	return main, worktree, gitDir
}
