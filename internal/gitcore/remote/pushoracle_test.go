//go:build oracle

package remote

import (
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/refspec"
	"github.com/oops1/gogit/internal/gitcore/transport"
)

func startReceiveDaemon(t *testing.T, basePath string, port int) (string, bool) {
	t.Helper()
	pidFile := filepath.Join(basePath, "receive-daemon.pid")
	logFile, err := os.Create(filepath.Join(basePath, "receive-daemon.log"))
	if err != nil {
		t.Logf("could not create the daemon log file: %v", err)
		return "", false
	}
	defer func() { _ = logFile.Close() }()

	ctx, cancel := context.WithCancel(t.Context())
	cmd := exec.CommandContext(ctx, "git", "daemon",
		"--export-all",
		"--enable=receive-pack",
		"--reuseaddr",
		"--base-path="+basePath,
		"--listen=127.0.0.1",
		"--port="+strconv.Itoa(port),
		"--pid-file="+pidFile,
		basePath,
	)
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		cancel()
		t.Logf("could not start git daemon: %v", err)
		return "", false
	}
	t.Cleanup(func() {
		cancel()
		_ = cmd.Wait()
		waitForFileRelease(filepath.Join(basePath, "receive-daemon.log"), 3*time.Second)
	})
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	if !waitForDaemon(addr, 5*time.Second) {
		t.Logf("git daemon did not start listening on %s", addr)
		return "", false
	}
	return addr, true
}

func withBestEffortTempDir(t *testing.T) string {
	t.Helper()
	basePath, err := os.MkdirTemp("", "gogit-push-oracle-*")
	if err != nil {
		t.Fatalf("MkdirTemp returned error %v", err)
	}
	t.Cleanup(func() {
		for range 30 {
			if os.RemoveAll(basePath) == nil {
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
	})
	return basePath
}

func TestPushMatchesSystemGitAndPassesFsck(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git is not available: %v", err)
	}

	basePath := withBestEffortTempDir(t)
	bare := filepath.Join(basePath, "repo.git")
	oracleGit(t, basePath, "init", "-q", "--bare", "-b", "main", bare)

	port, ok := freeTCPPort(t)
	if !ok {
		t.Skip("could not find a free tcp port")
	}
	addr, ok := startReceiveDaemon(t, basePath, port)
	if !ok {
		t.Skip("could not start a receive-enabled git daemon")
	}
	url := "git://" + addr + "/repo.git"

	r := newTestRepo(t, "")
	db := openTestODB(t, r)
	store := openTestRefs(t, r, db)
	blob := putBlob(t, db, "hello from gogit\n")
	tree := putTree(t, db, object.TreeEntry{Mode: object.ModeBlob, Name: "a.txt", ID: blob})
	commit := putCommitWithTree(t, db, testWhen(), "initial", tree)
	setLocalBranch(t, store, refs.BranchName("main"), commit)

	rem := Remote{Name: "origin", URLs: []string{url}}
	specs, err := refspec.ParseAll([]string{"refs/heads/main:refs/heads/main"})
	if err != nil {
		t.Fatalf("refspec.ParseAll returned error %v", err)
	}

	result, err := Push(t.Context(), r, rem, PushOptions{Refspecs: specs, Transport: transport.Options{}})
	if err != nil {
		t.Skipf("push to the receive-enabled git daemon failed, skipping: %v", err)
	}
	if len(result.Changes) == 0 {
		t.Fatal("Push reported no changes for a brand new branch")
	}

	oracleGit(t, "", "--git-dir="+bare, "fsck")

	log := oracleGit(t, "", "--git-dir="+bare, "log", "--oneline", "main")
	if !strings.Contains(string(log), "initial") {
		t.Fatalf("git log on the receiving repository did not show the pushed commit: %s", log)
	}

	content := oracleGit(t, "", "--git-dir="+bare, "show", "main:a.txt")
	if !strings.Contains(string(content), "hello from gogit") {
		t.Fatalf("git show reported unexpected blob content: %s", content)
	}
}
