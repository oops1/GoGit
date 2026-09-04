//go:build oracle

package ops

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/repo"
)

type oracle struct {
	t    *testing.T
	home string
	env  []string
}

func newOracle(t *testing.T) *oracle {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git is not available: %v", err)
	}
	home := filepath.Join(t.TempDir(), "home")
	if err := os.MkdirAll(home, 0o777); err != nil {
		t.Fatalf("MkdirAll returned error %v", err)
	}
	globalConfig := filepath.Join(home, "gitconfig")
	if err := os.WriteFile(globalConfig, nil, 0o666); err != nil {
		t.Fatalf("WriteFile returned error %v", err)
	}
	return &oracle{
		t:    t,
		home: home,
		env: []string{
			"PATH=" + os.Getenv("PATH"),
			"SystemRoot=" + os.Getenv("SystemRoot"),
			"HOME=" + home,
			"USERPROFILE=" + home,
			"GIT_CONFIG_GLOBAL=" + globalConfig,
			"GIT_CONFIG_NOSYSTEM=1",
			"GIT_TERMINAL_PROMPT=0",
		},
	}
}

func (o *oracle) run(dir string, args ...string) string {
	o.t.Helper()
	out, err := o.attempt(dir, args...)
	if err != nil {
		o.t.Fatalf("%v", err)
	}
	return out
}

func (o *oracle) attempt(dir string, args ...string) (string, error) {
	o.t.Helper()
	cmd := exec.CommandContext(o.t.Context(), "git", args...)
	cmd.Dir = dir
	cmd.Env = o.env
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	err := cmd.Run()
	if err != nil {
		err = fmt.Errorf("git %s in %s: %w: %s", strings.Join(args, " "), dir, err, errBuf.String())
	}
	return out.String(), err
}

func (o *oracle) repoDir(name string) string {
	dir := filepath.Join(o.t.TempDir(), name)
	if err := os.MkdirAll(dir, 0o777); err != nil {
		o.t.Fatalf("MkdirAll returned error %v", err)
	}
	return dir
}

func (o *oracle) options() repo.OpenOptions {
	return repo.OpenOptions{NoSystem: true, GlobalFile: filepath.Join(o.home, "gitconfig")}
}

func (o *oracle) write(dir, rel, text string) {
	o.t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o777); err != nil {
		o.t.Fatalf("MkdirAll returned error %v", err)
	}
	if err := os.WriteFile(full, []byte(text), 0o666); err != nil {
		o.t.Fatalf("WriteFile returned error %v", err)
	}
}

func (o *oracle) openRepo(dir string) *repo.Repository {
	o.t.Helper()
	r, err := repo.Open(dir, o.options())
	if err != nil {
		o.t.Fatalf("repo.Open returned error %v", err)
	}
	o.t.Cleanup(func() { _ = r.Close() })
	return r
}

func newOracleRepo(o *oracle, dir string) {
	o.run(dir, "init", "-q", "-b", "main", ".")
	o.run(dir, "config", "user.name", "oracle")
	o.run(dir, "config", "user.email", "oracle@example.com")
}

func TestOracleStageAndCommitProducesCleanStatusAndValidHistory(t *testing.T) {
	o := newOracle(t)
	dir := o.repoDir("work")
	newOracleRepo(o, dir)
	o.write(dir, "a.txt", "hello\n")
	o.write(dir, "sub/b.txt", "world\n")

	r := o.openRepo(dir)
	if err := Stage(t.Context(), r, []string{"a.txt", "sub"}, StageOptions{}); err != nil {
		t.Fatalf("Stage returned error %v", err)
	}
	id, err := Commit(t.Context(), r, CommitOptions{Message: "initial commit"})
	if err != nil {
		t.Fatalf("Commit returned error %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("Close returned error %v", err)
	}

	if status := o.run(dir, "status", "--porcelain=v2"); status != "" {
		t.Fatalf("git status is not clean: %q", status)
	}

	logOut := o.run(dir, "log", "-1", "--format=%H%n%T%n%P%n%an%n%ae%n%s")
	lines := strings.Split(strings.TrimRight(logOut, "\n"), "\n")
	if len(lines) != 6 {
		t.Fatalf("unexpected log output %q", logOut)
	}
	if lines[0] != id.String() {
		t.Fatalf("git HEAD = %s, want %s", lines[0], id)
	}
	if lines[2] != "" {
		t.Fatalf("git reports parents %q, want none", lines[2])
	}
	if lines[3] != "oracle" || lines[4] != "oracle@example.com" {
		t.Fatalf("author = %s <%s>", lines[3], lines[4])
	}
	if lines[5] != "initial commit" {
		t.Fatalf("subject = %q", lines[5])
	}

	o.run(dir, "fsck", "--strict")
}

func TestOracleAmendAndSecondCommitMatchGit(t *testing.T) {
	o := newOracle(t)
	dir := o.repoDir("work")
	newOracleRepo(o, dir)
	o.write(dir, "a.txt", "hello\n")
	o.run(dir, "add", ".")
	o.run(dir, "commit", "-q", "-m", "initial")
	first := strings.TrimSpace(o.run(dir, "rev-parse", "HEAD"))

	r := o.openRepo(dir)
	if err := Stage(t.Context(), r, []string{"a.txt"}, StageOptions{}); err != nil {
		t.Fatalf("Stage returned error %v", err)
	}
	if _, err := Commit(t.Context(), r, CommitOptions{Message: "amended", Amend: true, AllowEmpty: true}); err != nil {
		t.Fatalf("Commit returned error %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("Close returned error %v", err)
	}

	count := strings.TrimSpace(o.run(dir, "rev-list", "--count", "HEAD"))
	if count != "1" {
		t.Fatalf("commit count = %s, want 1", count)
	}
	newHead := strings.TrimSpace(o.run(dir, "rev-parse", "HEAD"))
	if newHead == first {
		t.Fatalf("amended commit kept the original id")
	}
	o.run(dir, "fsck", "--strict")
}

func TestOracleBranchLifecycleMatchesGit(t *testing.T) {
	o := newOracle(t)
	dir := o.repoDir("work")
	newOracleRepo(o, dir)
	o.write(dir, "a.txt", "hello\n")
	o.run(dir, "add", ".")
	o.run(dir, "commit", "-q", "-m", "initial")
	headText := strings.TrimSpace(o.run(dir, "rev-parse", "HEAD"))
	headID, err := hash.Parse(headText)
	if err != nil {
		t.Fatalf("hash.Parse returned error %v", err)
	}

	r := o.openRepo(dir)
	if err := CreateBranch(t.Context(), r, "feature", headID, CreateBranchOptions{}); err != nil {
		t.Fatalf("CreateBranch returned error %v", err)
	}
	if err := RenameBranch(t.Context(), r, "feature", "renamed", false); err != nil {
		t.Fatalf("RenameBranch returned error %v", err)
	}
	if err := Switch(t.Context(), r, "renamed", SwitchOptions{}); err != nil {
		t.Fatalf("Switch returned error %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("Close returned error %v", err)
	}

	branchOut := o.run(dir, "branch", "-v")
	if !strings.Contains(branchOut, "* renamed") {
		t.Fatalf("git branch -v = %q, want current branch renamed", branchOut)
	}
	if strings.Contains(branchOut, "feature") {
		t.Fatalf("git branch -v = %q, feature should not exist", branchOut)
	}
	o.run(dir, "fsck", "--strict")

	r2 := o.openRepo(dir)
	if err := Switch(t.Context(), r2, "main", SwitchOptions{}); err != nil {
		t.Fatalf("Switch returned error %v", err)
	}
	if err := DeleteBranch(t.Context(), r2, "renamed", false); err != nil {
		t.Fatalf("DeleteBranch returned error %v", err)
	}
	if err := r2.Close(); err != nil {
		t.Fatalf("Close returned error %v", err)
	}

	branchOut2 := o.run(dir, "branch", "-v")
	if strings.Contains(branchOut2, "renamed") {
		t.Fatalf("git branch -v = %q, renamed should have been deleted", branchOut2)
	}
	if !strings.Contains(branchOut2, "* main") {
		t.Fatalf("git branch -v = %q, want current branch main", branchOut2)
	}

	reflogOut := o.run(dir, "reflog", "show", "HEAD")
	if !strings.Contains(reflogOut, "checkout: moving from") {
		t.Fatalf("git reflog show HEAD = %q, missing checkout entries", reflogOut)
	}
}

func TestOracleStageOnGitCreatedFileMatchesGitAdd(t *testing.T) {
	o := newOracle(t)
	dir := o.repoDir("work")
	newOracleRepo(o, dir)
	o.write(dir, "a.txt", "hello\n")
	o.run(dir, "add", ".")
	o.run(dir, "commit", "-q", "-m", "initial")
	o.write(dir, "a.txt", "changed by git\n")

	r := o.openRepo(dir)
	if err := Stage(t.Context(), r, []string{"a.txt"}, StageOptions{}); err != nil {
		t.Fatalf("Stage returned error %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("Close returned error %v", err)
	}

	status := o.run(dir, "status", "--porcelain=v2")
	if !strings.Contains(status, "1 M. ") {
		t.Fatalf("git status = %q, want staged modification", status)
	}
}

func TestOracleSwitchToGitCreatedBranchMatchesGit(t *testing.T) {
	o := newOracle(t)
	dir := o.repoDir("work")
	newOracleRepo(o, dir)
	o.write(dir, "a.txt", "hello\n")
	o.run(dir, "add", ".")
	o.run(dir, "commit", "-q", "-m", "initial")
	o.run(dir, "branch", "topic")
	o.write(dir, "b.txt", "on topic\n")
	o.run(dir, "add", ".")
	o.run(dir, "commit", "-q", "-m", "topic commit")
	topicHead := strings.TrimSpace(o.run(dir, "rev-parse", "topic"))
	o.run(dir, "checkout", "-q", "main")

	r := o.openRepo(dir)
	if err := Switch(t.Context(), r, "topic", SwitchOptions{}); err != nil {
		t.Fatalf("Switch returned error %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("Close returned error %v", err)
	}

	gitHead := strings.TrimSpace(o.run(dir, "rev-parse", "HEAD"))
	if gitHead != topicHead {
		t.Fatalf("HEAD = %s, want %s", gitHead, topicHead)
	}
	branchName := strings.TrimSpace(o.run(dir, "symbolic-ref", "--short", "HEAD"))
	if branchName != "topic" {
		t.Fatalf("current branch = %q, want topic", branchName)
	}
	if status := o.run(dir, "status", "--porcelain=v2"); status != "" {
		t.Fatalf("git status is not clean: %q", status)
	}
}

func TestOracleCommitOnTopOfGitHistoryPassesFsck(t *testing.T) {
	o := newOracle(t)
	dir := o.repoDir("work")
	newOracleRepo(o, dir)
	o.write(dir, "a.txt", "hello\n")
	o.run(dir, "add", ".")
	o.run(dir, "commit", "-q", "-m", "initial")

	r := o.openRepo(dir)
	o.write(dir, "b.txt", "world\n")
	if err := Stage(t.Context(), r, []string{"b.txt"}, StageOptions{}); err != nil {
		t.Fatalf("Stage returned error %v", err)
	}
	id, err := Commit(t.Context(), r, CommitOptions{Message: "second"})
	if err != nil {
		t.Fatalf("Commit returned error %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("Close returned error %v", err)
	}

	gitHead := strings.TrimSpace(o.run(dir, "rev-parse", "HEAD"))
	if gitHead != id.String() {
		t.Fatalf("HEAD = %s, want %s", gitHead, id)
	}
	o.run(dir, "fsck", "--strict")
}
