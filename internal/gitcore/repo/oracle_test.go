//go:build oracle

package repo

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

type oracle struct {
	t    *testing.T
	dir  string
	home string
	env  []string
}

func newOracle(t *testing.T) *oracle {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git is not available: %v", err)
	}
	root := tempDir(t)
	home := makeDir(t, filepath.Join(root, "home"))
	writeFile(t, filepath.Join(home, "gitconfig"), "")
	return &oracle{
		t:    t,
		dir:  makeDir(t, filepath.Join(root, "tree")),
		home: home,
		env: []string{
			"PATH=" + os.Getenv("PATH"),
			"SystemRoot=" + os.Getenv("SystemRoot"),
			"HOME=" + home,
			"USERPROFILE=" + home,
			"GIT_CONFIG_GLOBAL=" + filepath.Join(home, "gitconfig"),
			"GIT_CONFIG_NOSYSTEM=1",
			"GIT_TERMINAL_PROMPT=0",
			"GIT_AUTHOR_NAME=oracle",
			"GIT_AUTHOR_EMAIL=oracle@example.com",
			"GIT_COMMITTER_NAME=oracle",
			"GIT_COMMITTER_EMAIL=oracle@example.com",
		},
	}
}

func (o *oracle) run(dir string, args ...string) string {
	o.t.Helper()
	cmd := exec.CommandContext(o.t.Context(), "git", args...)
	cmd.Dir = dir
	cmd.Env = o.env
	out, err := cmd.Output()
	if err != nil {
		o.t.Fatalf("git %s in %s returned error %v", strings.Join(args, " "), dir, err)
	}
	return string(out)
}

func (o *oracle) fails(dir string, args ...string) bool {
	o.t.Helper()
	cmd := exec.CommandContext(o.t.Context(), "git", args...)
	cmd.Dir = dir
	cmd.Env = o.env
	return cmd.Run() != nil
}

func (o *oracle) line(dir string, args ...string) string {
	o.t.Helper()
	return strings.TrimSpace(o.run(dir, args...))
}

func (o *oracle) records(dir string, args ...string) []string {
	o.t.Helper()
	var records []string
	for _, record := range strings.Split(o.run(dir, append(args, "--null")...), "\x00") {
		if record != "" {
			records = append(records, strings.Replace(record, "\n", "=", 1))
		}
	}
	slices.Sort(records)
	return records
}

func (o *oracle) options() OpenOptions {
	return OpenOptions{
		Env:        env{}.get,
		NoSystem:   true,
		GlobalFile: filepath.Join(o.home, "gitconfig"),
	}
}

func (o *oracle) initOptions() InitOptions {
	return InitOptions{
		Env:        env{}.get,
		NoSystem:   true,
		GlobalFile: filepath.Join(o.home, "gitconfig"),
	}
}

func gitPath(t *testing.T, base, raw string) string {
	t.Helper()
	return resolveFrom(base, filepath.FromSlash(strings.TrimSpace(raw)))
}

func compareLayout(t *testing.T, name string, got Layout, wantGitDir, wantCommonDir, wantWorkTree string) {
	t.Helper()
	if !samePath(got.GitDir, wantGitDir) {
		t.Errorf("%s: GitDir = %q, git says %q", name, got.GitDir, wantGitDir)
	}
	if !samePath(got.CommonDir, wantCommonDir) {
		t.Errorf("%s: CommonDir = %q, git says %q", name, got.CommonDir, wantCommonDir)
	}
	if !samePath(got.WorkTree, wantWorkTree) {
		t.Errorf("%s: WorkTree = %q, git says %q", name, got.WorkTree, wantWorkTree)
	}
}

func (o *oracle) discovered(t *testing.T, start string) Layout {
	t.Helper()
	layout, err := Discover(start, DiscoverOptions{Env: env{}.get})
	if err != nil {
		t.Fatalf("Discover(%q) returned error %v", start, err)
	}
	return layout
}

func TestOracleDiscoverMatchesRevParseForPlainRepository(t *testing.T) {
	o := newOracle(t)
	o.run(o.dir, "init", "-q", "plain")
	work := filepath.Join(o.dir, "plain")
	deep := makeDir(t, filepath.Join(work, "a", "b"))

	for _, start := range []string{work, deep} {
		layout := o.discovered(t, start)
		compareLayout(t, start, layout,
			gitPath(t, start, o.line(start, "rev-parse", "--git-dir")),
			gitPath(t, start, o.line(start, "rev-parse", "--git-common-dir")),
			gitPath(t, start, o.line(start, "rev-parse", "--show-toplevel")))
		if layout.Bare || layout.IsWorktree {
			t.Errorf("%s: layout reports Bare=%v IsWorktree=%v, want both false", start, layout.Bare, layout.IsWorktree)
		}
	}
}

func TestOracleDiscoverMatchesRevParseForBareRepository(t *testing.T) {
	o := newOracle(t)
	o.run(o.dir, "init", "-q", "--bare", "store.git")
	store := filepath.Join(o.dir, "store.git")

	layout := o.discovered(t, store)
	compareLayout(t, store, layout,
		gitPath(t, store, o.line(store, "rev-parse", "--git-dir")),
		gitPath(t, store, o.line(store, "rev-parse", "--git-common-dir")),
		"")
	if !layout.Bare {
		t.Errorf("layout reports Bare=false although git says %q", o.line(store, "rev-parse", "--is-bare-repository"))
	}
}

func TestOracleDiscoverMatchesRevParseForSeparateGitDir(t *testing.T) {
	o := newOracle(t)
	o.run(o.dir, "init", "-q", "--separate-git-dir", "sep.git", "work")
	work := filepath.Join(o.dir, "work")

	layout := o.discovered(t, work)
	compareLayout(t, work, layout,
		gitPath(t, work, o.line(work, "rev-parse", "--git-dir")),
		gitPath(t, work, o.line(work, "rev-parse", "--git-common-dir")),
		gitPath(t, work, o.line(work, "rev-parse", "--show-toplevel")))
	if !samePath(layout.GitDir, filepath.Join(o.dir, "sep.git")) {
		t.Errorf("layout has GitDir %q, want %q", layout.GitDir, filepath.Join(o.dir, "sep.git"))
	}
}

func TestOracleDiscoverMatchesRevParseForLinkedWorktree(t *testing.T) {
	o := newOracle(t)
	o.run(o.dir, "init", "-q", "main")
	main := filepath.Join(o.dir, "main")
	writeFile(t, filepath.Join(main, "a.txt"), "hello\n")
	o.run(main, "add", "a.txt")
	o.run(main, "commit", "-q", "-m", "first")
	o.run(main, "worktree", "add", "-q", filepath.Join(o.dir, "second"), "-b", "second")
	second := filepath.Join(o.dir, "second")

	layout := o.discovered(t, second)
	compareLayout(t, second, layout,
		gitPath(t, second, o.line(second, "rev-parse", "--git-dir")),
		gitPath(t, second, o.line(second, "rev-parse", "--git-common-dir")),
		gitPath(t, second, o.line(second, "rev-parse", "--show-toplevel")))
	if !layout.IsWorktree {
		t.Error("layout reports IsWorktree=false for a linked worktree")
	}
}

func TestOracleGitPathMatchesRevParseInLinkedWorktree(t *testing.T) {
	o := newOracle(t)
	o.run(o.dir, "init", "-q", "main")
	main := filepath.Join(o.dir, "main")
	writeFile(t, filepath.Join(main, "a.txt"), "hello\n")
	o.run(main, "add", "a.txt")
	o.run(main, "commit", "-q", "-m", "first")
	o.run(main, "worktree", "add", "-q", filepath.Join(o.dir, "second"), "-b", "second")
	second := filepath.Join(o.dir, "second")

	repository, err := Open(second, o.options())
	if err != nil {
		t.Fatalf("Open returned error %v", err)
	}
	defer repository.Close()

	for _, rel := range []string{
		"HEAD", "index", "ORIG_HEAD", "MERGE_HEAD", "FETCH_HEAD",
		"logs/HEAD", "logs/refs/heads/second", "refs/bisect", "refs/worktree/x",
		"refs/heads/second", "config", "config.worktree", "packed-refs", "shallow",
		"objects/pack", "hooks/pre-commit", "info/exclude", "info/sparse-checkout",
		"sequencer", "rebase-merge", "worktrees", "gc.pid", "branches", "remotes/origin",
	} {
		want := gitPath(t, second, o.line(second, "rev-parse", "--git-path", rel))
		if got := repository.GitPath(rel); !samePath(got, want) {
			t.Errorf("GitPath(%q) = %q, git says %q", rel, got, want)
		}
	}
}

func TestOracleAcceptsRepositoryCreatedByOurInit(t *testing.T) {
	o := newOracle(t)
	work := filepath.Join(o.dir, "ours")
	repository, err := Init(work, o.initOptions())
	if err != nil {
		t.Fatalf("Init returned error %v", err)
	}
	defer repository.Close()

	if got := o.line(work, "rev-parse", "--is-bare-repository"); got != "false" {
		t.Errorf("git reports --is-bare-repository %q, want %q", got, "false")
	}
	if got := o.line(work, "symbolic-ref", "HEAD"); got != "refs/heads/master" {
		t.Errorf("git reports HEAD %q, want %q", got, "refs/heads/master")
	}
	if got := o.line(work, "status", "--porcelain"); got != "" {
		t.Errorf("git status reports %q, want an empty working tree", got)
	}
	o.run(work, "fsck", "--strict")
	if !samePath(gitPath(t, work, o.line(work, "rev-parse", "--show-toplevel")), work) {
		t.Errorf("git reports a different top level than %q", work)
	}

	writeFile(t, filepath.Join(work, "a.txt"), "hello\n")
	o.run(work, "add", "a.txt")
	o.run(work, "commit", "-q", "-m", "first")
	o.run(work, "fsck", "--strict")
	if got := o.line(work, "log", "--format=%s"); got != "first" {
		t.Errorf("git log reports %q, want %q", got, "first")
	}
}

func TestOracleAcceptsBareRepositoryCreatedByOurInit(t *testing.T) {
	o := newOracle(t)
	store := filepath.Join(o.dir, "ours.git")
	opts := o.initOptions()
	opts.Bare = true
	repository, err := Init(store, opts)
	if err != nil {
		t.Fatalf("Init returned error %v", err)
	}
	defer repository.Close()

	if got := o.line(store, "rev-parse", "--is-bare-repository"); got != "true" {
		t.Errorf("git reports --is-bare-repository %q, want %q", got, "true")
	}
	o.run(store, "fsck", "--strict")
	if !o.fails(store, "status") {
		t.Error("git status succeeded inside a bare repository")
	}

	o.run(o.dir, "clone", "-q", filepath.ToSlash(store), "clone")
	if got := o.line(filepath.Join(o.dir, "clone"), "rev-parse", "--is-bare-repository"); got != "false" {
		t.Errorf("the clone reports --is-bare-repository %q, want %q", got, "false")
	}
}

func TestOracleLocalConfigMatchesGitInit(t *testing.T) {
	for _, bare := range []bool{false, true} {
		name := "worktree"
		if bare {
			name = "bare"
		}
		t.Run(name, func(t *testing.T) {
			o := newOracle(t)
			theirs := filepath.Join(o.dir, "theirs")
			args := []string{"init", "-q"}
			if bare {
				args = append(args, "--bare")
			}
			o.run(o.dir, append(args, "theirs")...)
			want := o.records(theirs, "config", "--list", "--local")

			ours := filepath.Join(o.dir, "ours")
			opts := o.initOptions()
			opts.Bare = bare
			repository, err := Init(ours, opts)
			if err != nil {
				t.Fatalf("Init returned error %v", err)
			}
			defer repository.Close()

			got := o.records(ours, "config", "--list", "--local")
			if !slices.Equal(got, want) {
				t.Fatalf("git init writes\n%q\nwe write\n%q", want, got)
			}
		})
	}
}

func TestOracleTemplateFilesMatchGitInit(t *testing.T) {
	o := newOracle(t)
	o.run(o.dir, "init", "-q", "theirs")
	ours := filepath.Join(o.dir, "ours")
	repository, err := Init(ours, o.initOptions())
	if err != nil {
		t.Fatalf("Init returned error %v", err)
	}
	defer repository.Close()

	for _, rel := range []string{descriptionName, infoExcludeName} {
		want := readFile(t, filepath.Join(o.dir, "theirs", dotGit, filepath.FromSlash(rel)))
		got := readFile(t, filepath.Join(ours, dotGit, filepath.FromSlash(rel)))
		if got != want {
			t.Errorf("%s holds\n%q\ngit writes\n%q", rel, got, want)
		}
	}
}

func TestOracleReadsRepositoryReinitialisedByUs(t *testing.T) {
	o := newOracle(t)
	o.run(o.dir, "init", "-q", "theirs")
	work := filepath.Join(o.dir, "theirs")
	o.run(work, "config", "user.name", "Оракул")
	o.run(work, "checkout", "-q", "-b", "trunk")
	writeFile(t, filepath.Join(work, "a.txt"), "hello\n")
	o.run(work, "add", "a.txt")
	o.run(work, "commit", "-q", "-m", "first")

	repository, err := Init(work, o.initOptions())
	if err != nil {
		t.Fatalf("Init returned error %v", err)
	}
	defer repository.Close()

	if got := o.line(work, "symbolic-ref", "HEAD"); got != "refs/heads/trunk" {
		t.Errorf("git reports HEAD %q, want %q", got, "refs/heads/trunk")
	}
	if got := o.line(work, "config", "user.name"); got != "Оракул" {
		t.Errorf("git reports user.name %q, want %q", got, "Оракул")
	}
	if got := o.line(work, "status", "--porcelain"); got != "" {
		t.Errorf("git status reports %q, want an empty working tree", got)
	}
	o.run(work, "fsck", "--strict")
}
