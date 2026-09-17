//go:build oracle

package transport

import (
	"os/exec"
	"slices"
	"strings"
	"testing"
)

func TestGitCurlAndGoGitTalkNTLMToTheSameProxyAlike(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git is not available: %v", err)
	}
	for _, secure := range []bool{false, true} {
		authority := newNTLMProxyAuthority(t, "NTLM", "hunter2")
		proxy := startAuthenticatingProxy(t, authority, false)
		target := newProxyTarget(t, secure)
		proxyURL := strings.Replace(proxy.server.URL, "http://", "http://alice:hunter2@", 1)
		clearProxyEnvironment(t)

		cmd := exec.CommandContext(t.Context(), "git",
			"-c", "http.proxy="+proxyURL,
			"-c", "http.proxyAuthMethod=ntlm",
			"-c", "http.sslVerify=false",
			"-c", "protocol.version=1",
			"ls-remote", target.url+"/repo.git", "refs/heads/main")
		cmd.Env = append(cmd.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+t.TempDir()+"/none", "GIT_TERMINAL_PROMPT=0")
		out, err := cmd.CombinedOutput()
		if err != nil || !strings.Contains(string(out), "refs/heads/main") {
			t.Skipf("secure=%v: system git/curl could not use the NTLM proxy (skipping oracle): %v\n%s", secure, err, out)
		}
		gitTrace := authority.takeTrace()

		opts := proxyConfig(t, proxyURL, target, "\tproxyAuthMethod = ntlm\n")
		if err := advertiseThrough(t, target.url+"/repo.git", opts); err != nil {
			t.Fatalf("secure=%v: advertise through the NTLM proxy returned error %v", secure, err)
		}
		ourTrace := authority.takeTrace()
		if !slices.Equal(gitTrace, ourTrace) {
			t.Fatalf("secure=%v: proxy saw git %q but Go.Git %q", secure, gitTrace, ourTrace)
		}
	}
}
