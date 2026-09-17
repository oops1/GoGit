//go:build oracle

package ops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func (f *submoduleForge) addBoth(gitDir, ourDir string, gitArgs []string, url string, opts SubmoduleAddOptions) {
	f.o.t.Helper()
	_, gitErr := f.o.attempt(gitDir, append([]string{"submodule", "add"}, gitArgs...)...)
	r := f.o.openRepo(ourDir)
	_, ourErr := SubmoduleAdd(f.o.t.Context(), r, url, opts)
	if (gitErr == nil) != (ourErr == nil) {
		f.o.t.Fatalf("git returned %v, SubmoduleAdd returned %v", gitErr, ourErr)
	}
	if err := r.Close(); err != nil {
		f.o.t.Fatal(err)
	}
}

func (f *submoduleForge) compareAdded(gitDir, ourDir string, paths ...string) {
	f.o.t.Helper()
	f.compareFiles(gitDir, ourDir, ".gitmodules")
	f.compareOutput(gitDir, ourDir, ".", "ls-files", "-s")
	f.compareSubmoduleState(gitDir, ourDir, paths...)
}

func TestOracleSubmoduleAddMatchesGit(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(f *submoduleForge, dir string)
		gitArgs func(f *submoduleForge) []string
		url     func(f *submoduleForge) string
		opts    SubmoduleAddOptions
		paths   []string
	}{
		{"relative url with a guessed path", nil,
			func(*submoduleForge) []string { return []string{"../inner.git"} },
			func(*submoduleForge) string { return "../inner.git" },
			SubmoduleAddOptions{}, []string{"inner"}},
		{"branch, name and depth", nil,
			func(f *submoduleForge) []string {
				return []string{"-b", "main", "--name", "third", "--depth", "1", fileURL(f.path("inner.git")), "deps/inner/"}
			},
			func(f *submoduleForge) string { return fileURL(f.path("inner.git")) },
			SubmoduleAddOptions{Branch: "main", Name: "third", Depth: 1, Path: "deps/inner/"}, []string{"deps/inner"}},
		{"existing repository in the work tree", func(f *submoduleForge, dir string) {
			f.o.run(dir, "clone", "-q", fileURL(f.path("inner.git")), "vendored")
		},
			func(*submoduleForge) []string { return []string{"../inner.git", "vendored"} },
			func(*submoduleForge) string { return "../inner.git" },
			SubmoduleAddOptions{Path: "vendored"}, []string{"vendored"}},
		{"path covered by submodule.active", func(f *submoduleForge, dir string) {
			f.o.run(dir, "config", "submodule.active", "deps/*")
		},
			func(*submoduleForge) []string { return []string{"../inner.git", "deps/one"} },
			func(*submoduleForge) string { return "../inner.git" },
			SubmoduleAddOptions{Path: "deps/one"}, []string{"deps/one"}},
		{"path outside submodule.active", func(f *submoduleForge, dir string) {
			f.o.run(dir, "config", "submodule.active", "deps/*")
		},
			func(*submoduleForge) []string { return []string{"../inner.git", "other"} },
			func(*submoduleForge) string { return "../inner.git" },
			SubmoduleAddOptions{Path: "other"}, []string{"other"}},
		{"file in the index", nil,
			func(*submoduleForge) []string { return []string{"../inner.git", "super.txt"} },
			func(*submoduleForge) string { return "../inner.git" },
			SubmoduleAddOptions{Path: "super.txt"}, nil},
		{"forced over a file in the index", nil,
			func(*submoduleForge) []string { return []string{"--force", "../inner.git", "super.txt"} },
			func(*submoduleForge) string { return "../inner.git" },
			SubmoduleAddOptions{Path: "super.txt", Force: true}, nil},
		{"directory holding a tracked file", nil,
			func(*submoduleForge) []string { return []string{"../inner.git", "libs"} },
			func(*submoduleForge) string { return "../inner.git" },
			SubmoduleAddOptions{Path: "libs"}, nil},
		{"ignored path", func(f *submoduleForge, dir string) {
			f.o.write(dir, ".git/info/exclude", "deps/\n")
		},
			func(*submoduleForge) []string { return []string{"../inner.git", "deps/inner"} },
			func(*submoduleForge) string { return "../inner.git" },
			SubmoduleAddOptions{Path: "deps/inner"}, nil},
		{"ignored path forced", func(f *submoduleForge, dir string) {
			f.o.write(dir, ".git/info/exclude", "deps/\n")
		},
			func(*submoduleForge) []string { return []string{"-f", "../inner.git", "deps/inner"} },
			func(*submoduleForge) string { return "../inner.git" },
			SubmoduleAddOptions{Path: "deps/inner", Force: true}, []string{"deps/inner"}},
		{"ignored file pattern", func(f *submoduleForge, dir string) {
			f.o.write(dir, ".git/info/exclude", "inner\n")
		},
			func(*submoduleForge) []string { return []string{"../inner.git"} },
			func(*submoduleForge) string { return "../inner.git" },
			SubmoduleAddOptions{}, nil},
		{"directory that is not a repository", func(f *submoduleForge, dir string) {
			f.o.write(dir, "plain/file.txt", "x\n")
		},
			func(*submoduleForge) []string { return []string{"../inner.git", "plain"} },
			func(*submoduleForge) string { return "../inner.git" },
			SubmoduleAddOptions{Path: "plain"}, nil},
		{"repository without commits", func(f *submoduleForge, dir string) {
			f.o.run(dir, "init", "-q", "empty")
		},
			func(*submoduleForge) []string { return []string{"../inner.git", "empty"} },
			func(*submoduleForge) string { return "../inner.git" },
			SubmoduleAddOptions{Path: "empty"}, nil},
		{"url that is neither absolute nor relative", nil,
			func(*submoduleForge) []string { return []string{"inner.git", "x"} },
			func(*submoduleForge) string { return "inner.git" },
			SubmoduleAddOptions{Path: "x"}, nil},
		{"invalid name", nil,
			func(*submoduleForge) []string { return []string{"--name", "../escape", "../inner.git", "x"} },
			func(*submoduleForge) string { return "../inner.git" },
			SubmoduleAddOptions{Path: "x", Name: "../escape"}, nil},
		{"missing branch", nil,
			func(*submoduleForge) []string { return []string{"-b", "nope", "../inner.git", "x"} },
			func(*submoduleForge) string { return "../inner.git" },
			SubmoduleAddOptions{Path: "x", Branch: "nope"}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newSubmoduleForge(t)
			f.forge(defaultGitmodules)
			gitDir, ourDir := f.clonePair("add")
			if tc.setup != nil {
				tc.setup(f, gitDir)
				tc.setup(f, ourDir)
			}

			f.addBoth(gitDir, ourDir, tc.gitArgs(f), tc.url(f), tc.opts)

			f.compareFiles(gitDir, ourDir, ".gitmodules")
			f.compareOutput(gitDir, ourDir, ".", "ls-files", "-s")
			f.compareFiles(gitDir, ourDir, ".git/config")
			f.compareOutput(gitDir, ourDir, ".", "status", "--porcelain=v2", "--ignored")
			if tc.paths != nil {
				f.compareAdded(gitDir, ourDir, tc.paths...)
			}
		})
	}
}

func TestOracleSubmoduleAddReactivatesAGitDirLikeGit(t *testing.T) {
	f := newSubmoduleForge(t)
	f.forge(defaultGitmodules)
	gitDir, ourDir := f.clonePair("reactivate")
	for _, dir := range []string{gitDir, ourDir} {
		f.o.run(dir, "submodule", "add", "../inner.git", "deps/inner")
		f.o.run(dir, "commit", "-q", "-m", "add inner")
		f.o.run(dir, "rm", "-q", "deps/inner")
		f.o.run(dir, "commit", "-q", "-m", "drop inner")
		if _, err := os.Stat(filepath.Join(dir, ".git", "modules", "deps", "inner")); err != nil {
			t.Fatal(err)
		}
	}

	f.addBoth(gitDir, ourDir, []string{"../inner.git", "deps/inner"}, "../inner.git", SubmoduleAddOptions{Path: "deps/inner"})
	f.compareFiles(gitDir, ourDir, ".gitmodules", ".git/config")
	f.addBoth(gitDir, ourDir, []string{"--force", "-b", "main", "../inner.git", "deps/inner"}, "../inner.git", SubmoduleAddOptions{Path: "deps/inner", Branch: "main", Force: true})

	f.compareAdded(gitDir, ourDir, "deps/inner")
	if !strings.Contains(f.o.read(ourDir, "deps/inner/.git"), "modules/deps/inner") {
		t.Fatal("the reactivated submodule does not point at its git directory")
	}
}
