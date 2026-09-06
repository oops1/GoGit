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

	"github.com/oops1/gogit/internal/gitcore/refspec"
	"github.com/oops1/gogit/internal/gitcore/transport"
)

func oracleGit(t *testing.T, dir string, args ...string) []byte {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir
	cmd.Env = append(cmd.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_AUTHOR_NAME=oracle",
		"GIT_AUTHOR_EMAIL=oracle@example.com",
		"GIT_COMMITTER_NAME=oracle",
		"GIT_COMMITTER_EMAIL=oracle@example.com",
	)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %s returned error %v: %s%s", strings.Join(args, " "), err, out, stderr.String())
	}
	return out
}

func freeTCPPort(t *testing.T) (int, bool) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, false
	}
	port := ln.Addr().(*net.TCPAddr).Port
	if err := ln.Close(); err != nil {
		return 0, false
	}
	return port, true
}

func waitForDaemon(addr string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

func waitForFileRelease(path string, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if os.Remove(path) == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func startGitDaemon(t *testing.T, basePath string, port int) (string, bool) {
	t.Helper()
	pidFile := filepath.Join(basePath, "daemon.pid")
	logFile, err := os.Create(filepath.Join(basePath, "daemon.log"))
	if err != nil {
		t.Logf("could not create the daemon log file: %v", err)
		return "", false
	}
	defer func() { _ = logFile.Close() }()

	ctx, cancel := context.WithCancel(t.Context())
	cmd := exec.CommandContext(ctx, "git", "daemon",
		"--export-all",
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
		waitForFileRelease(filepath.Join(basePath, "daemon.log"), time.Second)
	})
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	if !waitForDaemon(addr, 5*time.Second) {
		t.Logf("git daemon did not start listening on %s", addr)
		return "", false
	}
	return addr, true
}

func TestFetchMatchesSystemGitLsRemoteAndPassesFsck(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git is not available: %v", err)
	}

	basePath := withBestEffortTempDir(t)
	work := filepath.Join(basePath, "work")
	bare := filepath.Join(basePath, "repo.git")

	oracleGit(t, basePath, "init", "-q", "-b", "main", work)
	if err := os.WriteFile(filepath.Join(work, "a.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error %v", err)
	}
	oracleGit(t, work, "add", "a.txt")
	oracleGit(t, work, "commit", "-q", "-m", "initial")
	oracleGit(t, work, "tag", "-a", "v1", "-m", "v1")
	oracleGit(t, work, "branch", "other")
	oracleGit(t, basePath, "init", "-q", "-b", "main", "--bare", bare)
	oracleGit(t, work, "push", "-q", bare, "main:main", "other:other", "v1:v1")

	port, ok := freeTCPPort(t)
	if !ok {
		t.Skip("could not find a free tcp port")
	}
	addr, ok := startGitDaemon(t, basePath, port)
	if !ok {
		t.Skip("could not start git daemon")
	}
	url := "git://" + addr + "/repo.git"

	r := newTestRepo(t, "")
	spec, err := refspec.Parse("+refs/heads/*:refs/remotes/origin/*")
	if err != nil {
		t.Fatalf("refspec.Parse returned error %v", err)
	}
	rem := Remote{Name: "origin", URLs: []string{url}, Fetch: []refspec.RefSpec{spec}}

	result, err := Fetch(t.Context(), r, rem, FetchOptions{Tags: TagsAll, Transport: transport.Options{}})
	if err != nil {
		t.Fatalf("Fetch returned error %v", err)
	}

	lsRemote := oracleGit(t, basePath, "ls-remote", "--heads", "--tags", url)
	wantRefs := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(lsRemote)), "\n") {
		if line == "" {
			continue
		}
		oid, name, ok := strings.Cut(line, "\t")
		if !ok {
			t.Fatalf("unexpected ls-remote line %q", line)
		}
		if strings.HasSuffix(name, "^{}") {
			continue
		}
		wantRefs[name] = oid
	}
	gotRefs := map[string]string{}
	for _, ref := range result.Refs {
		gotRefs[ref.Name] = ref.ID.String()
	}
	for name, oid := range wantRefs {
		if gotRefs[name] != oid {
			t.Errorf("ref %s: Fetch advertisement reports %s, git ls-remote reports %s", name, gotRefs[name], oid)
		}
	}

	oracleGit(t, "", "--git-dir="+r.GitDir(), "fsck")

	log := oracleGit(t, "", "--git-dir="+r.GitDir(), "log", "--oneline", "refs/remotes/origin/main")
	if !strings.Contains(string(log), "initial") {
		t.Fatalf("git log on the fetched repository did not show the initial commit: %s", log)
	}
	if len(result.Changes) == 0 {
		t.Fatal("Fetch reported no changes for a fresh clone")
	}
}
