//go:build oracle

package ops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func (f *submoduleForge) branchesForSwitch(p forgedProject, dir string) {
	f.o.run(dir, "submodule", "update", "--init", "--recursive")
	f.o.run(dir, "checkout", "-q", "-b", "plain")
	f.o.run(dir, "rm", "-q", "-r", "libs/lib")
	f.o.run(dir, "commit", "-q", "-m", "plain")
	f.o.run(dir, "checkout", "-q", "main")
	f.o.run(dir, "checkout", "-q", "-b", "nolink")
	f.o.run(dir, "rm", "-q", "--cached", "libs/lib")
	f.o.run(dir, "commit", "-q", "-m", "nolink")
	f.o.run(dir, "checkout", "-q", "-f", "main")
	f.o.run(dir, "submodule", "update", "--init", "--recursive")
	f.o.run(dir, "checkout", "-q", "-b", "moved")
	f.o.run(filepath.Join(dir, "libs", "lib"), "checkout", "-q", p.lib)
	f.o.run(dir, "add", "libs/lib")
	f.o.run(dir, "commit", "-q", "-m", "moved")
	f.o.run(dir, "checkout", "-q", "main")
	f.o.run(dir, "submodule", "update", "--recursive")
	f.o.run(dir, "config", "submodule.recurse", "true")
}

func (f *submoduleForge) compareSwitched(gitDir, ourDir string) {
	f.o.t.Helper()
	f.compareFiles(gitDir, ourDir, ".git/HEAD", ".git/config", ".git/modules/lib/config", ".git/modules/lib/modules/inner/config", "libs/lib/.git", "libs/lib/inner/.git")
	f.compareOutput(gitDir, ourDir, ".", "ls-files", "-s")
	f.compareOutput(gitDir, ourDir, ".", "status", "--porcelain=v2", "--untracked-files=all", "--ignore-submodules=none")
	if want, got := f.listing(gitDir, "libs", ".git/modules"), f.listing(ourDir, "libs", ".git/modules"); want != got {
		f.o.t.Fatalf("layout differs:\ngit:\n%s\nours:\n%s", want, got)
	}
	for _, rel := range []string{"libs/lib", "libs/lib/inner"} {
		if _, err := os.Stat(filepath.Join(ourDir, filepath.FromSlash(rel), ".git")); err != nil {
			continue
		}
		want, _ := f.o.attempt(filepath.Join(gitDir, filepath.FromSlash(rel)), "rev-parse", "HEAD", "--symbolic-full-name", "HEAD")
		got, _ := f.o.attempt(filepath.Join(ourDir, filepath.FromSlash(rel)), "rev-parse", "HEAD", "--symbolic-full-name", "HEAD")
		if want != got {
			f.o.t.Fatalf("%s HEAD differs: git %q, ours %q", rel, want, got)
		}
	}
}

func TestOracleSwitchRecursionAddsRemovesAndMovesSubmodulesLikeGitCheckout(t *testing.T) {
	tests := []struct {
		name   string
		from   string
		target string
		force  bool
		setup  func(f *submoduleForge, p forgedProject, dir string)
	}{
		{"removed", "main", "plain", false, nil},
		{"removed with an untracked file", "main", "plain", false, func(f *submoduleForge, _ forgedProject, dir string) {
			f.o.write(dir, "libs/lib/new.txt", "new\n")
		}},
		{"removed with a modified file", "main", "plain", false, func(f *submoduleForge, _ forgedProject, dir string) {
			f.o.write(dir, "libs/lib/lib.txt", "changed\n")
		}},
		{"removed with a modified file forced", "main", "plain", true, func(f *submoduleForge, _ forgedProject, dir string) {
			f.o.write(dir, "libs/lib/lib.txt", "changed\n")
		}},
		{"dirty submodule index", "main", "moved", false, func(f *submoduleForge, _ forgedProject, dir string) {
			f.o.write(dir, "libs/lib/other.txt", "staged\n")
			f.o.run(filepath.Join(dir, "libs", "lib"), "add", "other.txt")
		}},
		{"moved with nested submodule", "main", "moved", false, nil},
		{"moved back", "moved", "main", false, nil},
		{"moved over a modified file", "main", "moved", false, func(f *submoduleForge, _ forgedProject, dir string) {
			f.o.write(dir, "libs/lib/lib.txt", "changed\n")
		}},
		{"moved keeping an unrelated untracked file", "main", "moved", false, func(f *submoduleForge, _ forgedProject, dir string) {
			f.o.write(dir, "libs/lib/keep.txt", "keep\n")
		}},
		{"reappears from its git directory", "plain", "main", false, nil},
		{"reappears while .gitmodules already knows it", "nolink", "main", false, nil},
		{"known path blocked by an empty directory", "nolink", "main", false, func(f *submoduleForge, _ forgedProject, dir string) {
			if err := os.MkdirAll(filepath.Join(dir, "libs", "lib"), 0o777); err != nil {
				f.o.t.Fatal(err)
			}
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newSubmoduleForge(t)
			p := f.forge(defaultGitmodules)
			gitDir, ourDir := f.clonePair("switch")
			for _, dir := range []string{gitDir, ourDir} {
				f.branchesForSwitch(p, dir)
				if tc.from != "main" {
					f.o.run(dir, "checkout", "-q", tc.from)
				}
				if tc.setup != nil {
					tc.setup(f, p, dir)
				}
			}
			f.compareSwitched(gitDir, ourDir)

			args := []string{"checkout", "-q"}
			if tc.force {
				args = append(args, "-f")
			}
			_, gitErr := f.o.attempt(gitDir, append(args, tc.target)...)
			r := f.o.openRepo(ourDir)
			ourErr := Switch(t.Context(), r, tc.target, SwitchOptions{Force: tc.force})
			_ = r.Close()
			if (gitErr == nil) != (ourErr == nil) {
				t.Fatalf("git returned %v, Switch returned %v", gitErr, ourErr)
			}
			f.compareSwitched(gitDir, ourDir)
		})
	}
}

func TestOracleSwitchRecursionRefusesAMissingGitDirWhereGitBreaks(t *testing.T) {
	f := newSubmoduleForge(t)
	p := f.forge(defaultGitmodules)
	gitDir, ourDir := f.clonePair("gitdir")
	for _, dir := range []string{gitDir, ourDir} {
		f.branchesForSwitch(p, dir)
		f.o.run(dir, "checkout", "-q", "plain")
		if err := os.RemoveAll(filepath.Join(dir, ".git", "modules", "lib")); err != nil {
			t.Fatal(err)
		}
	}

	_, gitErr := f.o.attempt(gitDir, "checkout", "-q", "main")
	r := f.o.openRepo(ourDir)
	ourErr := Switch(t.Context(), r, "main", SwitchOptions{})
	_ = r.Close()

	if gitErr == nil || ourErr == nil {
		t.Fatalf("git returned %v, Switch returned %v", gitErr, ourErr)
	}
	if head := strings.TrimSpace(f.o.run(ourDir, "symbolic-ref", "HEAD")); head != "refs/heads/plain" {
		t.Fatalf("the refused switch moved HEAD to %s", head)
	}
}
