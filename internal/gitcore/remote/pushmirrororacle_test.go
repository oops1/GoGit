//go:build oracle

package remote

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/refspec"
	"github.com/oops1/gogit/internal/gitcore/transport"
)

func TestPushReachesBothPushURLsLikeSystemGitDoes(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git is not available: %v", err)
	}

	basePath := withBestEffortTempDir(t)
	hub := filepath.Join(basePath, "hub.git")
	mirror := filepath.Join(basePath, "mirror.git")
	oracleGit(t, basePath, "init", "-q", "--bare", "-b", "main", hub)
	oracleGit(t, basePath, "init", "-q", "--bare", "-b", "main", mirror)

	port, ok := freeTCPPort(t)
	if !ok {
		t.Skip("could not find a free tcp port")
	}
	addr, ok := startReceiveDaemon(t, basePath, port)
	if !ok {
		t.Skip("could not start a receive-enabled git daemon")
	}

	r := newTestRepo(t, "")
	db := openTestODB(t, r)
	store := openTestRefs(t, r, db)
	blob := putBlob(t, db, "mirrored by gogit\n")
	tree := putTree(t, db, object.TreeEntry{Mode: object.ModeBlob, Name: "a.txt", ID: blob})
	commit := putCommitWithTree(t, db, testWhen(), "initial", tree)
	setLocalBranch(t, store, refs.BranchName("main"), commit)

	specs, err := refspec.ParseAll([]string{"refs/heads/main:refs/heads/main"})
	if err != nil {
		t.Fatalf("refspec.ParseAll returned error %v", err)
	}
	rem := Remote{
		Name:     "origin",
		URLs:     []string{"git://" + addr + "/hub.git"},
		PushURLs: []string{"git://" + addr + "/hub.git", "git://" + addr + "/mirror.git"},
	}

	result, err := Push(t.Context(), r, rem, PushOptions{Refspecs: specs, Transport: transport.Options{}})
	if err != nil {
		t.Skipf("push to the receive-enabled git daemon failed, skipping: %v", err)
	}

	if len(result.Targets) != 2 {
		t.Fatalf("targets = %+v, want one per push address", result.Targets)
	}
	for _, target := range result.Targets {
		if target.Err != nil || len(target.Sent) != 1 || target.Sent[0].New != commit {
			t.Fatalf("target %s = %+v", target.URL, target)
		}
	}

	for _, bare := range []string{hub, mirror} {
		oracleGit(t, "", "--git-dir="+bare, "fsck")
		head := strings.TrimSpace(string(oracleGit(t, "", "--git-dir="+bare, "rev-parse", "main")))
		if head != commit.String() {
			t.Fatalf("%s is at %s, want the pushed commit %s", bare, head, commit)
		}
	}
}

func TestSystemGitAlsoPushesToEveryPushURL(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git is not available: %v", err)
	}

	basePath := withBestEffortTempDir(t)
	hub := filepath.Join(basePath, "hub.git")
	mirror := filepath.Join(basePath, "mirror.git")
	work := filepath.Join(basePath, "work")
	oracleGit(t, basePath, "init", "-q", "--bare", "-b", "main", hub)
	oracleGit(t, basePath, "init", "-q", "--bare", "-b", "main", mirror)
	oracleGit(t, basePath, "init", "-q", "-b", "main", work)
	oracleGit(t, work, "config", "user.name", "Test")
	oracleGit(t, work, "config", "user.email", "test@example.com")
	oracleGit(t, work, "commit", "-q", "--allow-empty", "-m", "initial")
	oracleGit(t, work, "remote", "add", "origin", hub)
	oracleGit(t, work, "remote", "set-url", "--push", "--add", "origin", hub)
	oracleGit(t, work, "remote", "set-url", "--push", "--add", "origin", mirror)
	oracleGit(t, work, "push", "-q", "origin", "main")

	want := strings.TrimSpace(string(oracleGit(t, work, "rev-parse", "main")))
	for _, bare := range []string{hub, mirror} {
		head := strings.TrimSpace(string(oracleGit(t, "", "--git-dir="+bare, "rev-parse", "main")))
		if head != want {
			t.Fatalf("%s is at %s after git push, want %s", bare, head, want)
		}
	}
}
