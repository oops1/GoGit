//go:build oracle

package ops

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func (f *submoduleForge) listing(dir string, roots ...string) string {
	f.o.t.Helper()
	var lines []string
	for _, root := range roots {
		base := filepath.Join(dir, filepath.FromSlash(root))
		err := filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				lines = append(lines, "missing "+root)
				return filepath.SkipDir
			}
			rel, _ := filepath.Rel(dir, path)
			rel = filepath.ToSlash(rel)
			switch {
			case d.IsDir() && (d.Name() == "objects" || d.Name() == "logs" || d.Name() == "hooks" || d.Name() == "refs" || d.Name() == "info"):
				return filepath.SkipDir
			case d.IsDir():
				lines = append(lines, rel+"/")
			case d.Name() == "config" || d.Name() == "HEAD" || d.Name() == ".git" || strings.HasSuffix(rel, ".txt"):
				lines = append(lines, rel)
			}
			return nil
		})
		if err != nil {
			f.o.t.Fatal(err)
		}
	}
	slices.Sort(lines)
	return strings.Join(lines, "\n")
}

func (f *submoduleForge) compareRemoval(gitDir, ourDir string) {
	f.o.t.Helper()
	f.compareFiles(gitDir, ourDir, ".git/config", ".gitmodules", ".git/modules/lib/config", ".git/modules/lib/modules/inner/config")
	f.compareOutput(gitDir, ourDir, ".", "ls-files", "-s")
	f.compareOutput(gitDir, ourDir, ".", "status", "--porcelain=v2", "--untracked-files=all")
	if want, got := f.listing(gitDir, "libs", ".git/modules"), f.listing(ourDir, "libs", ".git/modules"); want != got {
		f.o.t.Fatalf("layout differs:\ngit:\n%s\nours:\n%s", want, got)
	}
}

type removalCase struct {
	name  string
	setup func(f *submoduleForge, p forgedProject, dir string)
	force bool
}

func embedLib(f *submoduleForge, p forgedProject, dir string) {
	lib := filepath.Join(dir, "libs", "lib")
	if err := os.RemoveAll(lib); err != nil {
		f.o.t.Fatal(err)
	}
	f.o.run(dir, "clone", "-q", fileURL(f.path("lib.git")), "libs/lib")
	f.o.run(lib, "checkout", "-q", p.libOld)
}

func embedInner(f *submoduleForge, p forgedProject, dir string) {
	inner := filepath.Join(dir, "libs", "lib", "inner")
	if err := os.RemoveAll(inner); err != nil {
		f.o.t.Fatal(err)
	}
	f.o.run(filepath.Join(dir, "libs", "lib"), "clone", "-q", fileURL(f.path("inner.git")), "inner")
	f.o.run(inner, "checkout", "-q", p.innerOld)
}

var removalCases = []removalCase{
	{"clean", nil, false},
	{"modified tracked file", func(f *submoduleForge, _ forgedProject, dir string) {
		f.o.write(dir, "libs/lib/lib.txt", "changed\n")
	}, false},
	{"modified tracked file forced", func(f *submoduleForge, _ forgedProject, dir string) {
		f.o.write(dir, "libs/lib/lib.txt", "changed\n")
	}, true},
	{"untracked file", func(f *submoduleForge, _ forgedProject, dir string) {
		f.o.write(dir, "libs/lib/new.txt", "new\n")
	}, false},
	{"ignored file", func(f *submoduleForge, _ forgedProject, dir string) {
		f.o.write(dir, ".git/modules/lib/info/exclude", "*.log\n")
		f.o.write(dir, "libs/lib/build.log", "log\n")
	}, false},
	{"moved head", func(f *submoduleForge, p forgedProject, dir string) {
		f.o.run(filepath.Join(dir, "libs", "lib"), "checkout", "-q", p.lib)
	}, false},
	{"staged move", func(f *submoduleForge, p forgedProject, dir string) {
		f.o.run(filepath.Join(dir, "libs", "lib"), "checkout", "-q", p.lib)
		f.o.run(dir, "add", "libs/lib")
	}, false},
	{"untracked file in a nested submodule", func(f *submoduleForge, _ forgedProject, dir string) {
		f.o.write(dir, "libs/lib/inner/new.txt", "new\n")
	}, false},
	{"unstaged .gitmodules", func(f *submoduleForge, _ forgedProject, dir string) {
		f.o.write(dir, ".gitmodules", f.o.read(dir, ".gitmodules")+"\n")
	}, true},
	{"embedded git directory", func(f *submoduleForge, p forgedProject, dir string) {
		embedLib(f, p, dir)
	}, false},
	{"embedded nested git directory", func(f *submoduleForge, p forgedProject, dir string) {
		embedInner(f, p, dir)
	}, true},
	{"not populated", func(f *submoduleForge, _ forgedProject, dir string) {
		f.o.run(dir, "submodule", "deinit", "-q", "-f", "--all")
	}, false},
}

func (f *submoduleForge) removalPair(tc removalCase) (string, string) {
	p := f.forge(defaultGitmodules)
	gitDir, ourDir := f.clonePair("removal")
	for _, dir := range []string{gitDir, ourDir} {
		if strings.HasPrefix(tc.name, "embedded nested") {
			f.o.run(dir, "submodule", "update", "--init")
		} else {
			f.o.run(dir, "submodule", "update", "--init", "--recursive")
		}
		if tc.setup != nil {
			tc.setup(f, p, dir)
		}
	}
	return gitDir, ourDir
}

func TestOracleSubmoduleDeinitMatchesGit(t *testing.T) {
	for _, tc := range removalCases {
		t.Run(tc.name, func(t *testing.T) {
			f := newSubmoduleForge(t)
			gitDir, ourDir := f.removalPair(tc)
			args := []string{"submodule", "deinit", "-q"}
			if tc.force {
				args = append(args, "-f")
			}
			_, gitErr := f.o.attempt(gitDir, append(args, "libs/lib")...)
			r := f.o.openRepo(ourDir)
			ourErr := SubmoduleDeinit(t.Context(), r, []string{"libs/lib"}, SubmoduleDeinitOptions{Force: tc.force})
			_ = r.Close()
			if (gitErr == nil) != (ourErr == nil) {
				t.Fatalf("git returned %v, SubmoduleDeinit returned %v", gitErr, ourErr)
			}
			f.compareRemoval(gitDir, ourDir)
		})
	}
}

func TestOracleSubmoduleDeinitOfEverySubmoduleMatchesGit(t *testing.T) {
	f := newSubmoduleForge(t)
	gitDir, ourDir := f.removalPair(removalCase{name: "all"})
	f.o.run(gitDir, "submodule", "deinit", "-q", "--all")
	r := f.o.openRepo(ourDir)
	if err := SubmoduleDeinit(t.Context(), r, nil, SubmoduleDeinitOptions{All: true}); err != nil {
		t.Fatalf("SubmoduleDeinit returned error %v", err)
	}
	_ = r.Close()
	f.compareRemoval(gitDir, ourDir)
}

func TestOracleSubmoduleRemoveMatchesGitRm(t *testing.T) {
	for _, tc := range removalCases {
		t.Run(tc.name, func(t *testing.T) {
			f := newSubmoduleForge(t)
			gitDir, ourDir := f.removalPair(tc)
			args := []string{"rm", "-q"}
			if tc.force {
				args = append(args, "-f")
			}
			_, gitErr := f.o.attempt(gitDir, append(args, "libs/lib")...)
			r := f.o.openRepo(ourDir)
			ourErr := SubmoduleRemove(t.Context(), r, []string{"libs/lib"}, SubmoduleRemoveOptions{Force: tc.force})
			_ = r.Close()
			if (gitErr == nil) != (ourErr == nil) {
				t.Fatalf("git returned %v, SubmoduleRemove returned %v", gitErr, ourErr)
			}
			f.compareRemoval(gitDir, ourDir)
		})
	}
}
