//go:build oracle

package ops

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type submoduleForge struct {
	o    *oracle
	root string
}

func newSubmoduleForge(t *testing.T) *submoduleForge {
	t.Helper()
	o := newOracle(t)
	global := "[user]\n\tname = oracle\n\temail = oracle@example.com\n" +
		"[protocol \"file\"]\n\tallow = always\n" +
		"[init]\n\tdefaultBranch = main\n" +
		"[core]\n\tautocrlf = false\n"
	if err := os.WriteFile(filepath.Join(o.home, "gitconfig"), []byte(global), 0o666); err != nil {
		t.Fatal(err)
	}
	return &submoduleForge{o: o, root: o.repoDir("forge")}
}

func fileURL(path string) string {
	slash := filepath.ToSlash(path)
	if !strings.HasPrefix(slash, "/") {
		slash = "/" + slash
	}
	return "file://" + slash
}

func (f *submoduleForge) path(name string) string {
	return filepath.Join(f.root, name)
}

func (f *submoduleForge) publish(name string, build func(work string)) string {
	work := f.path("w-" + name)
	if err := os.MkdirAll(work, 0o777); err != nil {
		f.o.t.Fatal(err)
	}
	f.o.run(work, "init", "-q", ".")
	build(work)
	bare := f.path(name + ".git")
	f.o.run(f.root, "clone", "-q", "--bare", work, bare)
	return strings.TrimSpace(f.o.run(work, "rev-parse", "HEAD"))
}

func (f *submoduleForge) commit(work, file, text, message string) {
	f.o.write(work, file, text)
	f.o.run(work, "add", file)
	f.o.run(work, "commit", "-q", "-m", message)
}

func (f *submoduleForge) link(work, path, sha string) {
	f.o.run(work, "update-index", "--add", "--cacheinfo", "160000,"+sha+","+path)
}

type forgedProject struct {
	inner    string
	innerOld string
	lib      string
	libOld   string
	super    string
}

func (f *submoduleForge) forge(gitmodules func(kind string) string) forgedProject {
	var p forgedProject
	f.publish("inner", func(work string) {
		f.commit(work, "inner.txt", "one\n", "inner one")
		p.innerOld = strings.TrimSpace(f.o.run(work, "rev-parse", "HEAD"))
		f.commit(work, "inner.txt", "two\n", "inner two")
	})
	p.inner = strings.TrimSpace(f.o.run(f.path("inner.git"), "rev-parse", "HEAD"))
	p.lib = f.publish("lib", func(work string) {
		f.commit(work, "lib.txt", "one\n", "lib one")
		f.o.write(work, ".gitmodules", gitmodules("lib"))
		f.o.run(work, "add", ".gitmodules")
		f.link(work, "inner", p.innerOld)
		f.o.run(work, "commit", "-q", "-m", "lib with inner")
		p.libOld = strings.TrimSpace(f.o.run(work, "rev-parse", "HEAD"))
		f.link(work, "inner", p.inner)
		f.commit(work, "lib.txt", "two\n", "lib two")
	})
	p.super = f.publish("super", func(work string) {
		f.commit(work, "super.txt", "one\n", "super one")
		f.o.write(work, ".gitmodules", gitmodules("super"))
		f.o.run(work, "add", ".gitmodules")
		f.link(work, "libs/lib", p.libOld)
		f.o.run(work, "commit", "-q", "-m", "super with lib")
	})
	return p
}

func defaultGitmodules(kind string) string {
	if kind == "lib" {
		return "[submodule \"inner\"]\n\tpath = inner\n\turl = ../inner.git\n"
	}
	return "[submodule \"lib\"]\n\tpath = libs/lib\n\turl = ../lib.git\n"
}

func (f *submoduleForge) clonePair(name string) (gitDir, ourDir string) {
	gitDir, ourDir = f.path(name+"-git"), f.path(name+"-ours")
	f.o.run(f.root, "clone", "-q", fileURL(f.path("super.git")), gitDir)
	f.o.run(f.root, "clone", "-q", fileURL(f.path("super.git")), ourDir)
	return gitDir, ourDir
}

func (f *submoduleForge) compareFiles(gitDir, ourDir string, rels ...string) {
	f.o.t.Helper()
	for _, rel := range rels {
		want, wantErr := os.ReadFile(filepath.Join(gitDir, filepath.FromSlash(rel)))
		got, gotErr := os.ReadFile(filepath.Join(ourDir, filepath.FromSlash(rel)))
		if (wantErr == nil) != (gotErr == nil) || string(want) != string(got) {
			f.o.t.Fatalf("%s differs:\ngit (%v):\n%s\nours (%v):\n%s", rel, wantErr, want, gotErr, got)
		}
	}
}

func (f *submoduleForge) compareOutput(gitDir, ourDir, rel string, args ...string) {
	f.o.t.Helper()
	want := strings.ReplaceAll(f.o.run(filepath.Join(gitDir, filepath.FromSlash(rel)), args...), filepath.ToSlash(gitDir), "<top>")
	got := strings.ReplaceAll(f.o.run(filepath.Join(ourDir, filepath.FromSlash(rel)), args...), filepath.ToSlash(ourDir), "<top>")
	if want != got {
		f.o.t.Fatalf("git %s in %s differs:\ngit:\n%s\nours:\n%s", strings.Join(args, " "), rel, want, got)
	}
}

func (f *submoduleForge) compareSubmoduleState(gitDir, ourDir string, paths ...string) {
	f.o.t.Helper()
	f.compareFiles(gitDir, ourDir, ".git/config")
	f.compareOutput(gitDir, ourDir, ".", "status", "--porcelain=v2")
	f.compareOutput(gitDir, ourDir, ".", "submodule", "status", "--recursive")
	for _, path := range paths {
		f.compareFiles(gitDir, ourDir, path+"/.git")
		f.compareOutput(gitDir, ourDir, path, "rev-parse", "HEAD", "--git-dir", "--show-toplevel")
		f.compareOutput(gitDir, ourDir, path, "ls-files", "-s")
		f.compareOutput(gitDir, ourDir, path, "status", "--porcelain=v2")
		f.compareOutput(gitDir, ourDir, path, "for-each-ref", "--format=%(refname) %(objectname) %(symref)")
		f.compareOutput(gitDir, ourDir, path, "config", "--local", "--list")
		f.compareOutput(gitDir, ourDir, path, "rev-parse", "--symbolic-full-name", "HEAD")
	}
}

func TestOracleSubmoduleInitWritesTheSameConfigAsGit(t *testing.T) {
	f := newSubmoduleForge(t)
	f.forge(defaultGitmodules)
	gitDir, ourDir := f.clonePair("init")

	f.o.run(gitDir, "submodule", "init")
	r := f.o.openRepo(ourDir)
	if err := SubmoduleInit(t.Context(), r, nil, SubmoduleInitOptions{}); err != nil {
		t.Fatalf("SubmoduleInit returned error %v", err)
	}

	f.compareFiles(gitDir, ourDir, ".git/config")
}

func TestOracleSubmoduleUpdateClonesLikeGit(t *testing.T) {
	f := newSubmoduleForge(t)
	f.forge(defaultGitmodules)
	gitDir, ourDir := f.clonePair("update")

	f.o.run(gitDir, "submodule", "update", "--init", "--recursive")
	r := f.o.openRepo(ourDir)
	if err := SubmoduleUpdate(t.Context(), r, nil, SubmoduleUpdateOptions{Init: true, Recursive: true}); err != nil {
		t.Fatalf("SubmoduleUpdate returned error %v", err)
	}

	f.compareSubmoduleState(gitDir, ourDir, "libs/lib", "libs/lib/inner")
	f.compareFiles(gitDir, ourDir, ".git/modules/lib/config", ".git/modules/lib/modules/inner/config", ".git/modules/lib/HEAD", ".git/modules/lib/modules/inner/HEAD")
	f.o.run(filepath.Join(ourDir, "libs", "lib"), "fsck", "--strict")
}

func (f *submoduleForge) updateBoth(gitDir, ourDir string, gitArgs []string, paths []string, opts SubmoduleUpdateOptions) {
	f.o.t.Helper()
	_, gitErr := f.o.attempt(gitDir, append([]string{"submodule", "update"}, gitArgs...)...)
	r := f.o.openRepo(ourDir)
	ourErr := SubmoduleUpdate(f.o.t.Context(), r, paths, opts)
	if (gitErr == nil) != (ourErr == nil) {
		f.o.t.Fatalf("git returned %v, SubmoduleUpdate returned %v", gitErr, ourErr)
	}
	if err := r.Close(); err != nil {
		f.o.t.Fatal(err)
	}
}

func (f *submoduleForge) advance(p *forgedProject) {
	f.o.t.Helper()
	libWork := f.path("w-lib")
	f.commit(libWork, "lib.txt", "three\n", "lib three")
	f.o.run(libWork, "push", "-q", f.path("lib.git"), "HEAD:main")
	p.lib = strings.TrimSpace(f.o.run(libWork, "rev-parse", "HEAD"))
	superWork := f.path("w-super")
	f.link(superWork, "libs/lib", p.lib)
	f.o.run(superWork, "commit", "-q", "-m", "super moves lib")
	f.o.run(superWork, "push", "-q", f.path("super.git"), "HEAD:main")
}

func TestOracleSubmoduleUpdateFetchesAMissingCommitLikeGit(t *testing.T) {
	f := newSubmoduleForge(t)
	p := f.forge(defaultGitmodules)
	gitDir, ourDir := f.clonePair("fetch")
	f.o.run(gitDir, "submodule", "update", "--init", "--recursive")
	f.o.run(ourDir, "submodule", "update", "--init", "--recursive")
	f.advance(&p)
	f.o.run(gitDir, "pull", "-q")
	f.o.run(ourDir, "pull", "-q")

	f.updateBoth(gitDir, ourDir, []string{"--recursive"}, nil, SubmoduleUpdateOptions{Recursive: true})

	f.compareSubmoduleState(gitDir, ourDir, "libs/lib", "libs/lib/inner")
	if head := strings.TrimSpace(f.o.run(filepath.Join(ourDir, "libs", "lib"), "rev-parse", "HEAD")); head != p.lib {
		t.Fatalf("lib HEAD = %s, want %s", head, p.lib)
	}
	f.o.run(filepath.Join(ourDir, "libs", "lib"), "fsck", "--strict")
}

func TestOracleSubmoduleUpdateModesMatchGit(t *testing.T) {
	tests := []struct {
		name    string
		config  string
		gitArgs []string
		opts    SubmoduleUpdateOptions
	}{
		{"configured none", "none", nil, SubmoduleUpdateOptions{}},
		{"configured merge", "merge", nil, SubmoduleUpdateOptions{}},
		{"configured rebase", "rebase", nil, SubmoduleUpdateOptions{}},
		{"explicit checkout beats configured none", "none", []string{"--checkout"}, SubmoduleUpdateOptions{Mode: SubmoduleUpdateCheckout}},
		{"explicit merge", "", []string{"--merge"}, SubmoduleUpdateOptions{Mode: SubmoduleUpdateMerge}},
		{"explicit rebase", "", []string{"--rebase"}, SubmoduleUpdateOptions{Mode: SubmoduleUpdateRebase}},
		{"remote tracking branch", "", []string{"--remote"}, SubmoduleUpdateOptions{Remote: true}},
		{"no fetch", "", []string{"--no-fetch"}, SubmoduleUpdateOptions{NoFetch: true}},
		{"forced", "", []string{"--force"}, SubmoduleUpdateOptions{Force: true}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newSubmoduleForge(t)
			p := f.forge(defaultGitmodules)
			gitDir, ourDir := f.clonePair("modes")
			for _, dir := range []string{gitDir, ourDir} {
				f.o.run(dir, "submodule", "update", "--init")
				if tc.config != "" {
					f.o.run(dir, "config", "submodule.lib.update", tc.config)
				}
			}
			f.advance(&p)
			f.o.run(gitDir, "pull", "-q")
			f.o.run(ourDir, "pull", "-q")

			f.updateBoth(gitDir, ourDir, tc.gitArgs, nil, tc.opts)

			f.compareSubmoduleState(gitDir, ourDir, "libs/lib")
		})
	}
}

func TestOracleSubmoduleUpdateOfNamedPathsMatchesGit(t *testing.T) {
	f := newSubmoduleForge(t)
	f.forge(defaultGitmodules)
	gitDir, ourDir := f.clonePair("paths")

	f.updateBoth(gitDir, ourDir, []string{"libs/lib"}, []string{"libs/lib"}, SubmoduleUpdateOptions{})
	f.compareSubmoduleState(gitDir, ourDir)
	f.updateBoth(gitDir, ourDir, []string{"--init", "--", "libs/lib"}, []string{"libs/lib"}, SubmoduleUpdateOptions{Init: true})
	f.compareSubmoduleState(gitDir, ourDir, "libs/lib")
	f.updateBoth(gitDir, ourDir, []string{"--", "missing"}, []string{"missing"}, SubmoduleUpdateOptions{})
}

func TestOracleSubmoduleUpdateRefillsAnAbsorbedGitDirLikeGit(t *testing.T) {
	f := newSubmoduleForge(t)
	f.forge(defaultGitmodules)
	gitDir, ourDir := f.clonePair("refill")
	for _, dir := range []string{gitDir, ourDir} {
		f.o.run(dir, "submodule", "update", "--init")
		if err := os.RemoveAll(filepath.Join(dir, "libs", "lib")); err != nil {
			t.Fatal(err)
		}
	}

	f.updateBoth(gitDir, ourDir, nil, nil, SubmoduleUpdateOptions{})

	f.compareSubmoduleState(gitDir, ourDir, "libs/lib")
}

func TestOracleSubmoduleSyncMatchesGit(t *testing.T) {
	tests := []struct {
		name  string
		setup func(f *submoduleForge, dir string)
	}{
		{"moved url in .gitmodules", func(f *submoduleForge, dir string) {
			f.o.write(dir, ".gitmodules", "[submodule \"lib\"]\n\tpath = libs/lib\n\turl = ../moved/lib.git\n")
			f.o.write(dir, "libs/lib/.gitmodules", "[submodule \"inner\"]\n\tpath = inner\n\turl = ../../elsewhere/inner\n")
		}},
		{"relative superproject remote", func(f *submoduleForge, dir string) {
			f.o.run(dir, "config", "remote.origin.url", "../upstream/super.git")
		}},
		{"scp-like superproject remote", func(f *submoduleForge, dir string) {
			f.o.run(dir, "config", "remote.origin.url", "git@example.com:group/super.git")
		}},
		{"absolute url", func(f *submoduleForge, dir string) {
			f.o.write(dir, ".gitmodules", "[submodule \"lib\"]\n\tpath = libs/lib\n\turl = https://example.com/lib.git\n")
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newSubmoduleForge(t)
			f.forge(defaultGitmodules)
			gitDir, ourDir := f.clonePair("sync")
			for _, dir := range []string{gitDir, ourDir} {
				f.o.run(dir, "submodule", "update", "--init", "--recursive")
				tc.setup(f, dir)
			}

			f.o.run(gitDir, "submodule", "sync", "--recursive")
			r := f.o.openRepo(ourDir)
			if err := SubmoduleSync(t.Context(), r, nil, SubmoduleSyncOptions{Recursive: true}); err != nil {
				t.Fatalf("SubmoduleSync returned error %v", err)
			}

			f.compareFiles(gitDir, ourDir, ".git/config", ".git/modules/lib/config", ".git/modules/lib/modules/inner/config")
		})
	}
}

func TestOracleRelativeSubmoduleURLsResolveLikeGit(t *testing.T) {
	urls := []string{"../lib.git", "./nested/lib", "../../up/lib", "../../../far/lib", `..\back\lib`, "https://example.com/abs.git", "../lib.git/"}
	remotes := []struct {
		name  string
		setup func(f *submoduleForge, dir string)
	}{
		{"file url", func(*submoduleForge, string) {}},
		{"https with trailing slash", func(f *submoduleForge, dir string) {
			f.o.run(dir, "config", "remote.origin.url", "https://example.com/group/super.git/")
		}},
		{"scp-like", func(f *submoduleForge, dir string) {
			f.o.run(dir, "config", "remote.origin.url", "git@example.com:super.git")
		}},
		{"relative path", func(f *submoduleForge, dir string) {
			f.o.run(dir, "config", "remote.origin.url", "../upstream/super")
		}},
		{"plain relative path", func(f *submoduleForge, dir string) {
			f.o.run(dir, "config", "remote.origin.url", "super")
		}},
		{"ssh url", func(f *submoduleForge, dir string) {
			f.o.run(dir, "config", "remote.origin.url", "ssh://host/repo")
		}},
		{"branch remote", func(f *submoduleForge, dir string) {
			f.o.run(dir, "config", "remote.upstream.url", "https://mirror.example.com/a/b/super")
			f.o.run(dir, "config", "branch.main.remote", "upstream")
		}},
		{"no remote", func(f *submoduleForge, dir string) {
			f.o.run(dir, "config", "--unset", "remote.origin.url")
		}},
	}
	for _, rem := range remotes {
		t.Run(rem.name, func(t *testing.T) {
			f := newSubmoduleForge(t)
			var gitmodules strings.Builder
			for i, url := range urls {
				fmt.Fprintf(&gitmodules, "[submodule \"m%d\"]\n\tpath = m%d\n\turl = %s\n", i, i, url)
			}
			f.publish("super", func(work string) {
				f.commit(work, "super.txt", "one\n", "super one")
				f.o.write(work, ".gitmodules", gitmodules.String())
				f.o.run(work, "add", ".gitmodules")
				for i := range urls {
					f.link(work, fmt.Sprintf("m%d", i), strings.TrimSpace(f.o.run(work, "rev-parse", "HEAD")))
				}
				f.o.run(work, "commit", "-q", "-m", "many modules")
			})
			gitDir, ourDir := f.clonePair("urls")
			rem.setup(f, gitDir)
			rem.setup(f, ourDir)
			for i := range urls {
				path := fmt.Sprintf("m%d", i)
				_, gitErr := f.o.attempt(gitDir, "submodule", "init", "--", path)
				r := f.o.openRepo(ourDir)
				ourErr := SubmoduleInit(t.Context(), r, []string{path}, SubmoduleInitOptions{})
				if (gitErr == nil) != (ourErr == nil) {
					t.Fatalf("%s: git returned %v, SubmoduleInit returned %v", urls[i], gitErr, ourErr)
				}
				_ = r.Close()
				want := strings.ReplaceAll(f.o.read(gitDir, ".git/config"), filepath.ToSlash(gitDir), "<top>")
				got := strings.ReplaceAll(f.o.read(ourDir, ".git/config"), filepath.ToSlash(ourDir), "<top>")
				if want != got {
					t.Fatalf("%s: config differs:\ngit:\n%s\nours:\n%s", urls[i], want, got)
				}
			}
		})
	}
}

func TestOracleCloneWithRecursiveSubmodulesMatchesGit(t *testing.T) {
	for _, shallow := range []bool{false, true} {
		t.Run(fmt.Sprintf("shallow=%v", shallow), func(t *testing.T) {
			f := newSubmoduleForge(t)
			f.forge(defaultGitmodules)
			gitDir, ourDir := f.path("clone-git"), f.path("clone-ours")
			args := []string{"clone", "-q", "--recurse-submodules"}
			if shallow {
				args = append(args, "--shallow-submodules")
			}
			f.o.run(f.root, append(args, fileURL(f.path("super.git")), gitDir)...)
			r, err := Clone(t.Context(), fileURL(f.path("super.git")), ourDir, CloneOptions{RecurseSubmodules: true, ShallowSubmodules: shallow, Open: f.o.options()})
			if err != nil {
				t.Fatalf("Clone returned error %v", err)
			}
			_ = r.Close()

			f.compareSubmoduleState(gitDir, ourDir, "libs/lib", "libs/lib/inner")
			f.compareFiles(gitDir, ourDir, ".git/modules/lib/config", ".git/modules/lib/modules/inner/config", ".git/modules/lib/shallow", ".git/modules/lib/modules/inner/shallow")
		})
	}
}

func TestOracleSwitchWithSubmoduleRecurseMatchesGitCheckout(t *testing.T) {
	f := newSubmoduleForge(t)
	p := f.forge(defaultGitmodules)
	gitDir, ourDir := f.clonePair("switch")
	for _, dir := range []string{gitDir, ourDir} {
		f.o.run(dir, "submodule", "update", "--init", "--recursive")
		f.o.run(dir, "config", "submodule.recurse", "true")
		f.o.run(dir, "branch", "next")
	}
	superWork := f.path("w-super")
	f.link(superWork, "libs/lib", p.lib)
	f.o.run(superWork, "commit", "-q", "-m", "next moves lib")
	f.o.run(superWork, "push", "-q", f.path("super.git"), "HEAD:next")
	for _, dir := range []string{gitDir, ourDir} {
		f.o.run(dir, "fetch", "-q", "origin")
		f.o.run(dir, "branch", "-f", "next", "origin/next")
	}

	f.o.run(gitDir, "checkout", "-q", "next")
	r := f.o.openRepo(ourDir)
	if err := Switch(t.Context(), r, "next", SwitchOptions{}); err != nil {
		t.Fatalf("Switch returned error %v", err)
	}
	_ = r.Close()

	f.compareSubmoduleState(gitDir, ourDir, "libs/lib", "libs/lib/inner")
}

func TestOraclePullWithSubmoduleRecurseMatchesGit(t *testing.T) {
	for _, rebase := range []bool{false, true} {
		t.Run(fmt.Sprintf("rebase=%v", rebase), func(t *testing.T) {
			f := newSubmoduleForge(t)
			p := f.forge(defaultGitmodules)
			gitDir, ourDir := f.clonePair("pull")
			for _, dir := range []string{gitDir, ourDir} {
				f.o.run(dir, "submodule", "update", "--init", "--recursive")
				f.o.run(dir, "config", "submodule.recurse", "true")
				f.o.run(dir, "config", "pull.rebase", fmt.Sprint(rebase))
			}
			f.advance(&p)

			f.o.run(gitDir, "pull", "-q")
			r := f.o.openRepo(ourDir)
			if _, err := Pull(t.Context(), r, PullOptions{}); err != nil {
				t.Fatalf("Pull returned error %v", err)
			}
			_ = r.Close()

			f.compareSubmoduleState(gitDir, ourDir, "libs/lib", "libs/lib/inner")
		})
	}
}
