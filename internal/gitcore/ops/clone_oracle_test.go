//go:build oracle

package ops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOracleCloneOfGitCreatedRepositoryPassesFsckAndMatchesHead(t *testing.T) {
	o := newOracle(t)
	src := o.repoDir("src")
	newOracleRepo(o, src)
	o.write(src, "a.txt", "hello\n")
	o.write(src, "sub/b.txt", "world\n")
	o.run(src, "add", ".")
	o.run(src, "commit", "-q", "-m", "initial")
	o.run(src, "branch", "topic")
	srcHead := strings.TrimSpace(o.run(src, "rev-parse", "HEAD"))

	dest := filepath.Join(o.t.TempDir(), "dest")
	r, err := Clone(t.Context(), src, dest, CloneOptions{})
	if err != nil {
		t.Fatalf("Clone returned error %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("Close returned error %v", err)
	}

	o.run(dest, "fsck", "--strict")
	if status := o.run(dest, "status", "--porcelain=v2"); status != "" {
		t.Fatalf("git status is not clean: %q", status)
	}
	destHead := strings.TrimSpace(o.run(dest, "rev-parse", "HEAD"))
	if destHead != srcHead {
		t.Fatalf("HEAD = %s, want %s", destHead, srcHead)
	}

	data, err := os.ReadFile(filepath.Join(dest, "sub", "b.txt"))
	if err != nil {
		t.Fatalf("ReadFile returned error %v", err)
	}
	if string(data) != "world\n" {
		t.Fatalf("sub/b.txt = %q, want world", data)
	}

	branchOut := o.run(dest, "branch", "-r")
	if !strings.Contains(branchOut, "origin/topic") {
		t.Fatalf("git branch -r = %q, want origin/topic to have been fetched", branchOut)
	}
}

func TestOracleCloneBareOfGitCreatedRepositoryMirrorsBranches(t *testing.T) {
	o := newOracle(t)
	src := o.repoDir("src")
	newOracleRepo(o, src)
	o.write(src, "a.txt", "hello\n")
	o.run(src, "add", ".")
	o.run(src, "commit", "-q", "-m", "initial")
	o.run(src, "branch", "topic")

	dest := filepath.Join(o.t.TempDir(), "dest.git")
	r, err := Clone(t.Context(), src, dest, CloneOptions{Bare: true})
	if err != nil {
		t.Fatalf("Clone returned error %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("Close returned error %v", err)
	}

	o.run(dest, "fsck", "--strict")
	branchOut := o.run(dest, "branch", "-a")
	if !strings.Contains(branchOut, "topic") {
		t.Fatalf("git branch -a = %q, want topic to have been mirrored", branchOut)
	}
}

func TestOracleSystemGitClonesOurRepository(t *testing.T) {
	o := newOracle(t)
	r := newTestRepo(t)
	r.writeFile("a.txt", "hello\n")
	mustStage(t, r, "a.txt")
	commitID := r.commitAll("initial")

	dest := filepath.Join(o.t.TempDir(), "dest")
	o.run("", "clone", "-q", r.dir, dest)

	gitHead := strings.TrimSpace(o.run(dest, "rev-parse", "HEAD"))
	if gitHead != commitID.String() {
		t.Fatalf("HEAD = %s, want %s", gitHead, commitID)
	}
	o.run(dest, "fsck", "--strict")
	if status := o.run(dest, "status", "--porcelain=v2"); status != "" {
		t.Fatalf("git status is not clean: %q", status)
	}
	data, err := os.ReadFile(filepath.Join(dest, "a.txt"))
	if err != nil {
		t.Fatalf("ReadFile returned error %v", err)
	}
	if string(data) != "hello\n" {
		t.Fatalf("a.txt = %q, want hello", data)
	}
}
