//go:build oracle

package indexeditor

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/ops"
	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
)

type gitRepo struct {
	t   *testing.T
	dir string
	env []string
}

func newGitRepo(t *testing.T) *gitRepo {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git is not available: %v", err)
	}
	home := filepath.Join(t.TempDir(), "home")
	if err := os.MkdirAll(home, 0o777); err != nil {
		t.Fatal(err)
	}
	r := &gitRepo{
		t:   t,
		dir: t.TempDir(),
		env: []string{
			"PATH=" + os.Getenv("PATH"),
			"SystemRoot=" + os.Getenv("SystemRoot"),
			"HOME=" + home,
			"USERPROFILE=" + home,
			"GIT_CONFIG_NOSYSTEM=1",
			"GIT_CONFIG_COUNT=2",
			"GIT_CONFIG_KEY_0=gc.auto",
			"GIT_CONFIG_VALUE_0=0",
			"GIT_CONFIG_KEY_1=maintenance.auto",
			"GIT_CONFIG_VALUE_1=false",
			"GIT_AUTHOR_NAME=oracle",
			"GIT_AUTHOR_EMAIL=oracle@example.com",
			"GIT_COMMITTER_NAME=oracle",
			"GIT_COMMITTER_EMAIL=oracle@example.com",
		},
	}
	r.run("init", "-q", "-b", "main", ".")
	return r
}

func (r *gitRepo) run(args ...string) string {
	r.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = r.dir
	cmd.Env = r.env
	out, err := cmd.CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func (r *gitRepo) write(name, content string) {
	r.t.Helper()
	if err := os.WriteFile(filepath.Join(r.dir, name), []byte(content), 0o600); err != nil {
		r.t.Fatal(err)
	}
}

func (r *gitRepo) status() string {
	r.t.Helper()
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = r.dir
	cmd.Env = r.env
	out, err := cmd.Output()
	if err != nil {
		r.t.Fatalf("git status: %v", err)
	}
	return strings.TrimRight(string(out), "\r\n")
}

func (r *gitRepo) open() *gitrepo.Repository {
	r.t.Helper()
	repository, err := gitrepo.Open(r.dir, gitrepo.OpenOptions{NoSystem: true})
	if err != nil {
		r.t.Fatal(err)
	}
	r.t.Cleanup(func() { _ = repository.Close() })
	return repository
}

func bodyOf(patch string) string {
	lines := strings.Split(patch, "\n")
	for at, line := range lines {
		if strings.HasPrefix(line, "@@") {
			return strings.Join(lines[at:], "\n")
		}
	}
	return patch
}

func TestTheStagedResultIsWhatGitDiffCachedShows(t *testing.T) {
	git := newGitRepo(t)
	git.write("f.txt", "one\ntwo\nthree\n")
	git.run("add", "f.txt")
	git.run("commit", "-q", "-m", "base")
	git.write("f.txt", "one\nTWO\nthree\nfour\n")
	repository := git.open()

	sides, err := ops.ReadIndexSides(t.Context(), repository, "f.txt")
	if err != nil {
		t.Fatalf("ReadIndexSides returned error %v", err)
	}
	view := newTestView(t)
	view.Show(File{
		Path:      sides.Path,
		HeadLabel: "main",
		Head:      sides.Head,
		Index:     sides.Index,
		Working:   sides.Working,
	})
	view.nextChange.OnClick()
	view.fromWorking.OnClick()
	result := view.Result()
	if err := ops.SaveIndexContent(t.Context(), repository, sides.Path, []byte(result)); err != nil {
		t.Fatalf("SaveIndexContent returned error %v", err)
	}

	if result != "one\ntwo\nthree\nfour\n" {
		t.Fatalf("result = %q, want only the added line staged", result)
	}
	if got := bodyOf(git.run("diff", "--cached")); got != "@@ -1,3 +1,4 @@\n one\n two\n three\n+four" {
		t.Fatalf("git diff --cached = %q", got)
	}
	if got := git.run("diff", "--name-only"); got != "f.txt" {
		t.Fatalf("git diff = %q, want the working copy still ahead of the index", got)
	}
	if got := git.status(); got != "MM f.txt" {
		t.Fatalf("git status = %q", got)
	}
}

func TestAFileStagedFromNothingIsWhatGitDiffCachedShows(t *testing.T) {
	git := newGitRepo(t)
	git.write("kept.txt", "kept\n")
	git.run("add", "kept.txt")
	git.run("commit", "-q", "-m", "base")
	git.write("new.txt", "first\nsecond\n")
	repository := git.open()

	sides, err := ops.ReadIndexSides(t.Context(), repository, "new.txt")
	if err != nil {
		t.Fatalf("ReadIndexSides returned error %v", err)
	}
	view := newTestView(t)
	view.Show(File{Path: sides.Path, HeadLabel: "main", Head: sides.Head, Index: sides.Index, Working: sides.Working})
	view.fromWorking.OnClick()
	if err := ops.SaveIndexContent(t.Context(), repository, sides.Path, []byte(view.Result())); err != nil {
		t.Fatalf("SaveIndexContent returned error %v", err)
	}

	if got := bodyOf(git.run("diff", "--cached")); got != "@@ -0,0 +1,2 @@\n+first\n+second" {
		t.Fatalf("git diff --cached = %q", got)
	}
	if got := git.status(); got != "A  new.txt" {
		t.Fatalf("git status = %q, want the file staged whole", got)
	}
}

func TestUnstagingALineInTheEditorIsWhatGitDiffCachedShows(t *testing.T) {
	git := newGitRepo(t)
	git.write("f.txt", "one\ntwo\n")
	git.run("add", "f.txt")
	git.run("commit", "-q", "-m", "base")
	git.write("f.txt", "one\nTWO\n")
	git.run("add", "f.txt")
	repository := git.open()

	sides, err := ops.ReadIndexSides(t.Context(), repository, "f.txt")
	if err != nil {
		t.Fatalf("ReadIndexSides returned error %v", err)
	}
	view := newTestView(t)
	view.Show(File{Path: sides.Path, HeadLabel: "main", Head: sides.Head, Index: sides.Index, Working: sides.Working})
	view.fromHead.OnClick()
	if err := ops.SaveIndexContent(t.Context(), repository, sides.Path, []byte(view.Result())); err != nil {
		t.Fatalf("SaveIndexContent returned error %v", err)
	}

	if got := git.run("diff", "--cached"); got != "" {
		t.Fatalf("git diff --cached = %q, want nothing staged", got)
	}
	if got := git.status(); got != " M f.txt" {
		t.Fatalf("git status = %q, want the change left in the working copy only", got)
	}
	if got, err := os.ReadFile(filepath.Join(git.dir, "f.txt")); err != nil || string(got) != "one\nTWO\n" {
		t.Fatalf("f.txt = %q, %v", got, err)
	}
}

func TestTheIndexWrittenByTheEditorPassesFsck(t *testing.T) {
	git := newGitRepo(t)
	git.write("f.txt", "one\n")
	git.run("add", "f.txt")
	git.run("commit", "-q", "-m", "base")
	repository := git.open()

	if err := ops.SaveIndexContent(t.Context(), repository, "f.txt", []byte("one\ntwo\n")); err != nil {
		t.Fatalf("SaveIndexContent returned error %v", err)
	}
	git.run("fsck", "--strict")

	if got := bodyOf(git.run("diff", "--cached")); got != "@@ -1 +1,2 @@\n one\n+two" {
		t.Fatalf("git diff --cached = %q", got)
	}
}
