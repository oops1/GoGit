//go:build oracle

package transport

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGitCurlAuthenticatesAgainstOurNTLMServer(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git is not available: %v", err)
	}
	handler := newNTLMTestServer(t, "NTLM", "hunter2")
	server := httptest.NewServer(http.HandlerFunc(handler.handle))
	t.Cleanup(server.Close)

	home := t.TempDir()
	store := filepath.Join(home, "credentials")
	line := strings.Replace(server.URL, "http://", "http://alice:hunter2@", 1)
	if err := os.WriteFile(store, []byte(line+"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile returned error %v", err)
	}

	cmd := exec.CommandContext(t.Context(), "git",
		"-c", "credential.helper=store --file="+filepath.ToSlash(store),
		"ls-remote", server.URL+"/repo.git", "refs/heads/main")
	cmd.Env = append(cmd.Environ(),
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
		"HOME="+home,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Skipf("system git/curl could not do NTLM (skipping oracle): %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "refs/heads/main") {
		t.Fatalf("git ls-remote output = %q, want refs/heads/main", out)
	}
	if !handler.type3Seen {
		t.Fatalf("git reached the server without completing the NTLM handshake")
	}
	if !handler.sameConnection() {
		t.Fatalf("git sent type1 %q and type3 %q on different connections", handler.type1Addr, handler.type3Addr)
	}
}
