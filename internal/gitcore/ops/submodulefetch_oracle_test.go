//go:build oracle

package ops

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/remote"
)

func (f *submoduleForge) hideLibCommit(p *forgedProject) {
	f.o.t.Helper()
	libWork := f.path("w-lib")
	f.commit(libWork, "lib.txt", "hidden\n", "lib hidden")
	f.o.run(libWork, "push", "-q", f.path("lib.git"), "HEAD:refs/hidden/one")
	p.lib = strings.TrimSpace(f.o.run(libWork, "rev-parse", "HEAD"))
	f.o.run(libWork, "reset", "-q", "--hard", "HEAD~1")
	superWork := f.path("w-super")
	f.link(superWork, "libs/lib", p.lib)
	f.o.run(superWork, "commit", "-q", "-m", "super points at a hidden commit")
	f.o.run(superWork, "push", "-q", f.path("super.git"), "HEAD:main")
}

func (f *submoduleForge) compareFetchedSubmodules(gitDir, ourDir string, commits ...string) {
	f.o.t.Helper()
	for _, gitdir := range []string{".git/modules/lib", ".git/modules/lib/modules/inner"} {
		f.compareOutput(gitDir, ourDir, ".", "--git-dir="+gitdir, "--work-tree=.", "for-each-ref", "--format=%(refname) %(objectname)")
		for _, commit := range commits {
			_, wantErr := f.o.attempt(gitDir, "--git-dir="+gitdir, "--work-tree=.", "cat-file", "-e", commit)
			_, gotErr := f.o.attempt(ourDir, "--git-dir="+gitdir, "--work-tree=.", "cat-file", "-e", commit)
			if (wantErr == nil) != (gotErr == nil) {
				f.o.t.Fatalf("%s has %s: git %v, ours %v", gitdir, commit, wantErr == nil, gotErr == nil)
			}
		}
	}
	f.compareOutput(gitDir, ourDir, ".", "status", "--porcelain=v2")
	f.compareOutput(gitDir, ourDir, ".", "for-each-ref", "--format=%(refname) %(objectname)")
}

func TestOracleFetchRecursesIntoSubmodulesLikeGit(t *testing.T) {
	tests := []struct {
		name   string
		setup  func(f *submoduleForge, dir string)
		hidden bool
	}{
		{"on demand by default", nil, false},
		{"switched off", func(f *submoduleForge, dir string) {
			f.o.run(dir, "config", "fetch.recurseSubmodules", "false")
		}, false},
		{"submodule.recurse fetches every populated submodule", func(f *submoduleForge, dir string) {
			f.o.run(dir, "config", "submodule.recurse", "true")
		}, false},
		{"fetch.recurseSubmodules wins when it comes later", func(f *submoduleForge, dir string) {
			f.o.run(dir, "config", "submodule.recurse", "true")
			f.o.run(dir, "config", "fetch.recurseSubmodules", "on-demand")
		}, false},
		{"per submodule switch", func(f *submoduleForge, dir string) {
			f.o.run(dir, "config", "submodule.lib.fetchRecurseSubmodules", "false")
		}, false},
		{"per submodule switch loses to fetch.recurseSubmodules", func(f *submoduleForge, dir string) {
			f.o.run(dir, "config", "submodule.lib.fetchRecurseSubmodules", "false")
			f.o.run(dir, "config", "fetch.recurseSubmodules", "on-demand")
		}, false},
		{"unpopulated submodule with a git directory", func(f *submoduleForge, dir string) {
			f.o.run(dir, "submodule", "deinit", "-q", "--all")
		}, false},
		{"removed submodule that stays registered", func(f *submoduleForge, dir string) {
			f.o.run(dir, "rm", "-q", "libs/lib")
			f.o.write(dir, ".gitmodules", "[submodule \"other\"]\n\tpath = other\n\turl = ../lib.git\n")
		}, false},
		{"removed submodule that is no longer active", func(f *submoduleForge, dir string) {
			f.o.run(dir, "rm", "-q", "libs/lib")
			f.o.write(dir, ".gitmodules", "[submodule \"other\"]\n\tpath = other\n\turl = ../lib.git\n")
			f.o.run(dir, "config", "--remove-section", "submodule.lib")
		}, false},
		{"commit reachable only by id", nil, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newSubmoduleForge(t)
			p := f.forge(defaultGitmodules)
			gitDir, ourDir := f.clonePair("fetch")
			for _, dir := range []string{gitDir, ourDir} {
				f.o.run(dir, "submodule", "update", "--init", "--recursive")
				if tc.setup != nil {
					tc.setup(f, dir)
				}
			}
			if tc.hidden {
				f.hideLibCommit(&p)
			} else {
				f.advance(&p)
			}

			_, gitErr := f.o.attempt(gitDir, "fetch", "-q")
			r := f.o.openRepo(ourDir)
			_, ourErr := Fetch(t.Context(), r, "", remote.FetchOptions{})
			_ = r.Close()
			if (gitErr == nil) != (ourErr == nil) {
				t.Fatalf("git returned %v, Fetch returned %v", gitErr, ourErr)
			}

			f.compareFetchedSubmodules(gitDir, ourDir, p.lib, p.inner)
		})
	}
}

func TestOraclePullFetchesChangedSubmodulesWithoutUpdatingThemLikeGit(t *testing.T) {
	f := newSubmoduleForge(t)
	p := f.forge(defaultGitmodules)
	gitDir, ourDir := f.clonePair("pull-fetch")
	for _, dir := range []string{gitDir, ourDir} {
		f.o.run(dir, "submodule", "update", "--init", "--recursive")
	}
	f.advance(&p)

	f.o.run(gitDir, "pull", "-q")
	r := f.o.openRepo(ourDir)
	if _, err := Pull(t.Context(), r, PullOptions{Fetch: remote.FetchOptions{}}); err != nil {
		t.Fatalf("Pull returned error %v", err)
	}
	_ = r.Close()

	f.compareFetchedSubmodules(gitDir, ourDir, p.lib)
	f.compareOutput(gitDir, ourDir, filepath.Join("libs", "lib"), "rev-parse", "HEAD")
}
