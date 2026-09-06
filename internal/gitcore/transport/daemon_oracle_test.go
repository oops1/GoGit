//go:build oracle

package transport

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

	"github.com/oops1/gogit/internal/gitcore/hash"
)

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

func killDaemonFromPidFile(pidFile string) {
	data, err := os.ReadFile(pidFile)
	if err != nil {
		return
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return
	}
	_ = proc.Kill()
}

func startGitDaemon(t *testing.T, basePath string, port int) (addr string, ok bool) {
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
		killDaemonFromPidFile(pidFile)
	})
	addr = net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	if !waitForDaemon(addr, 5*time.Second) {
		t.Logf("git daemon did not start listening on %s", addr)
		return "", false
	}
	return addr, true
}

func TestGitDaemonFetchAndLsRefsMatchGitLsRemote(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git is not available: %v", err)
	}

	basePath := t.TempDir()
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

	session, err := Dial(t.Context(), "git://"+addr+"/repo.git", UploadPack, Options{})
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	defer func() { _ = session.Close() }()

	adv, err := session.Advertise(t.Context())
	if err != nil {
		t.Fatalf("Advertise returned error %v", err)
	}
	if adv.Version != 2 {
		t.Fatalf("Advertisement.Version = %d, want 2 (git daemon supports protocol v2)", adv.Version)
	}

	lsRemote := oracleGit(t, basePath, "ls-remote", "git://"+addr+"/repo.git")
	wantRefs := map[string]string{}
	wantPeeled := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(lsRemote)), "\n") {
		if line == "" {
			continue
		}
		oid, name, ok := strings.Cut(line, "\t")
		if !ok {
			t.Fatalf("unexpected ls-remote line %q", line)
		}
		if peeledName, isPeeled := strings.CutSuffix(name, "^{}"); isPeeled {
			wantPeeled[peeledName] = oid
			continue
		}
		wantRefs[name] = oid
	}

	gotRefs := map[string]string{}
	gotPeeled := map[string]string{}
	for _, ref := range adv.Refs {
		gotRefs[ref.Name] = ref.ID.String()
		if !ref.Peeled.IsZero() {
			gotPeeled[ref.Name] = ref.Peeled.String()
		}
	}
	if len(gotRefs) != len(wantRefs) {
		t.Fatalf("Advertise found %d refs %v, git ls-remote reports %d %v", len(gotRefs), gotRefs, len(wantRefs), wantRefs)
	}
	for name, oid := range wantRefs {
		if gotRefs[name] != oid {
			t.Errorf("ref %s: Advertise reports %s, git ls-remote reports %s", name, gotRefs[name], oid)
		}
	}
	for name, oid := range wantPeeled {
		if gotPeeled[name] != oid {
			t.Errorf("peeled ref %s: Advertise reports %s, git ls-remote reports %s", name, gotPeeled[name], oid)
		}
	}

	mainRef, foundMain := findRefByName(adv.Refs, "refs/heads/main")
	if !foundMain {
		t.Fatalf("Advertise did not report refs/heads/main")
	}

	fetchSession, err := Dial(t.Context(), "git://"+addr+"/repo.git", UploadPack, Options{})
	if err != nil {
		t.Fatalf("Dial returned error %v", err)
	}
	defer func() { _ = fetchSession.Close() }()

	resp, err := fetchSession.Fetch(t.Context(), FetchRequest{Wants: []hash.ObjectID{mainRef.ID}}, nil)
	if err != nil {
		t.Fatalf("Fetch returned error %v", err)
	}
	defer func() { _ = resp.Pack.Close() }()

	var header [4]byte
	if _, err := resp.Pack.Read(header[:]); err != nil {
		t.Fatalf("reading the pack header returned error %v", err)
	}
	if string(header[:]) != "PACK" {
		t.Fatalf("pack header = %q, want PACK", header[:])
	}
}
