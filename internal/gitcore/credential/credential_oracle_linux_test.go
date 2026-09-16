//go:build oracle

package credential

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/secretservice"
)

func libsecretHelperProgram(t *testing.T) string {
	t.Helper()
	candidates := []string{"/usr/share/doc/git/contrib/credential/libsecret/git-credential-libsecret"}
	if out, err := exec.CommandContext(t.Context(), "git", "--exec-path").Output(); err == nil {
		candidates = append([]string{filepath.Join(strings.TrimSpace(string(out)), "git-credential-libsecret")}, candidates...)
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return candidate
		}
	}
	t.Skip("git-credential-libsecret is not installed")
	return ""
}

func requireSecretService(t *testing.T) {
	t.Helper()
	conn, err := secretservice.Dial()
	if err != nil {
		t.Skip("no session bus")
	}
	defer conn.Close()
	if _, err := conn.OpenSession(t.Context()); err != nil {
		t.Skip("no secret service on the session bus")
	}
}

func eraseOracleKeyringHost(t *testing.T, host string) {
	t.Helper()
	t.Cleanup(func() {
		k, err := openDBusKeyring()
		if err != nil {
			t.Errorf("open keyring for cleanup: %v", err)
			return
		}
		defer k.close()
		ctx := context.Background()
		server, _ := libsecretHostPort(host)
		items, err := k.search(ctx, map[string]string{"server": server})
		if err != nil {
			t.Errorf("search keyring for cleanup: %v", err)
			return
		}
		defer wipeKeyringItems(items)
		if err := k.remove(ctx, items); err != nil {
			t.Errorf("remove keyring items for cleanup: %v", err)
		}
	})
}

func TestOracleLibsecretSharesCredentialsWithGit(t *testing.T) {
	program := libsecretHelperProgram(t)
	requireSecretService(t)
	config := []string{"credential.helper=" + program, "credential.useHttpPath=true"}
	h := &libsecretHelper{name: "libsecret", open: openDBusKeyring}
	ctx := context.Background()

	for _, q := range []Query{
		{Protocol: "https", Host: oracleHost()},
		{Protocol: "https", Host: oracleHost() + ":8443", Path: "org/repo.git"},
	} {
		eraseOracleKeyringHost(t, q.Host)
		mustGitCredential(t, config, "approve", withOraclePassword(oracleQueryFields(q), "bob", "from-git"))
		requireNativeAnswer(t, h, q, "bob", "from-git")
		if err := h.Erase(ctx, q, Answer{Username: "bob", Password: []byte("stale")}); err != nil {
			t.Fatal(err)
		}
		requireNativeAnswer(t, h, q, "bob", "from-git")
		if err := h.Erase(ctx, q, Answer{Username: "bob", Password: []byte("from-git")}); err != nil {
			t.Fatal(err)
		}
		requireGitFillMisses(t, config, oracleQueryFields(q))

		if err := h.Store(ctx, q, Answer{Username: "eve", Password: []byte("from-gogit")}); err != nil {
			t.Fatal(err)
		}
		out := mustGitCredential(t, config, "fill", oracleQueryFields(q))
		if out["username"] != "eve" || out["password"] != "from-gogit" {
			t.Fatalf("git fill = %q", out)
		}
		mustGitCredential(t, config, "reject", withOraclePassword(oracleQueryFields(q), "eve", "from-gogit"))
		requireNativeMiss(t, h, q)
	}
}
