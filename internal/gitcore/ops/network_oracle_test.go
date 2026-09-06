//go:build oracle

package ops

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/remote"
)

func TestOracleOurPushIsVisibleToSystemGit(t *testing.T) {
	o := newOracle(t)
	server := o.repoDir("server")
	newOracleRepo(o, server)

	client := newTestRepo(t)
	client.writeFile("a.txt", "hello\n")
	mustStage(t, client, "a.txt")
	commit := client.commitAll("initial")

	if err := AddRemote(client.repo, "origin", server); err != nil {
		t.Fatalf("AddRemote returned error %v", err)
	}
	client.repo = client.reopen()

	specs := mustPushSpecs(t, "refs/heads/main:refs/heads/main")
	if _, err := Push(t.Context(), client.repo, "", remote.PushOptions{Refspecs: specs}); err != nil {
		t.Fatalf("Push returned error %v", err)
	}

	o.run(server, "fsck", "--strict")
	logHead := strings.TrimSpace(o.run(server, "log", "-1", "--format=%H"))
	if logHead != commit.String() {
		t.Fatalf("git log HEAD = %s, want %s", logHead, commit)
	}
}

func TestOracleOurPullLeavesSystemGitStatusClean(t *testing.T) {
	o := newOracle(t)
	server := o.repoDir("server")
	newOracleRepo(o, server)
	o.write(server, "a.txt", "hello\n")
	o.run(server, "add", ".")
	o.run(server, "commit", "-q", "-m", "initial")

	dest := filepath.Join(o.t.TempDir(), "dest")
	r, err := Clone(t.Context(), server, dest, CloneOptions{})
	if err != nil {
		t.Fatalf("Clone returned error %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })

	o.write(server, "b.txt", "world\n")
	o.run(server, "add", ".")
	o.run(server, "commit", "-q", "-m", "second")
	serverHead := strings.TrimSpace(o.run(server, "rev-parse", "HEAD"))

	result, err := Pull(t.Context(), r, PullOptions{})
	if err != nil {
		t.Fatalf("Pull returned error %v", err)
	}
	if !result.Updated {
		t.Fatalf("Pull result = %+v, want Updated", result)
	}

	if status := o.run(dest, "status", "--porcelain=v2"); status != "" {
		t.Fatalf("git status is not clean: %q", status)
	}
	destHead := strings.TrimSpace(o.run(dest, "rev-parse", "HEAD"))
	if destHead != serverHead {
		t.Fatalf("HEAD = %s, want %s", destHead, serverHead)
	}
}
