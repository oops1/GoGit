//go:build oracle

package credential

import (
	"context"
	"strings"
	"testing"
)

func eraseOracleHostCredentials(t *testing.T, host string) {
	t.Helper()
	t.Cleanup(func() {
		creds, err := windowsCredentials.enumerate("")
		if err != nil {
			t.Errorf("list credentials for cleanup: %v", err)
			return
		}
		defer wipeWinCredentials(creds)
		for _, c := range creds {
			if strings.Contains(c.target, host) {
				if err := windowsCredentials.remove(gcmTrimLegacyPrefix(c.target)); err != nil {
					t.Errorf("remove %s: %v", c.target, err)
				}
			}
		}
	})
}

func oracleWindowsHost(t *testing.T) string {
	t.Helper()
	host := oracleHost()
	eraseOracleHostCredentials(t, host)
	return host
}

func TestOracleWincredSharesCredentialsWithGit(t *testing.T) {
	config := []string{"credential.helper=wincred", "credential.useHttpPath=true"}
	h := &wincredHelper{name: "wincred", manager: windowsCredentials}
	ctx := context.Background()

	for _, q := range []Query{
		{Protocol: "https", Host: oracleWindowsHost(t)},
		{Protocol: "https", Host: oracleWindowsHost(t) + ":8443", Path: "org/repo.git"},
	} {
		mustGitCredential(t, config, "approve", withOraclePassword(oracleQueryFields(q), "bob@corp", "from-git"))
		requireNativeAnswer(t, h, q, "bob@corp", "from-git")
		withUser := q
		withUser.Username = "bob@corp"
		requireNativeAnswer(t, h, withUser, "bob@corp", "from-git")
		if err := h.Erase(ctx, q, Answer{Username: "bob@corp", Password: []byte("stale")}); err != nil {
			t.Fatal(err)
		}
		requireNativeAnswer(t, h, q, "bob@corp", "from-git")
		if err := h.Erase(ctx, q, Answer{Username: "bob@corp", Password: []byte("from-git")}); err != nil {
			t.Fatal(err)
		}
		requireGitFillMisses(t, config, oracleQueryFields(q))

		if err := h.Store(ctx, q, Answer{Username: "eve", Password: []byte("from-gogit-пароль")}); err != nil {
			t.Fatal(err)
		}
		out := mustGitCredential(t, config, "fill", oracleQueryFields(q))
		if out["username"] != "eve" || out["password"] != "from-gogit-пароль" {
			t.Fatalf("git fill = %q", out)
		}
		mustGitCredential(t, config, "reject", withOraclePassword(oracleQueryFields(q), "eve", "from-gogit-пароль"))
		requireNativeMiss(t, h, q)
	}
}

func TestOracleCredentialManagerSharesWindowsCredentialsWithGit(t *testing.T) {
	requireCredentialManagerInstalled(t)
	config := []string{"credential.helper=manager", "credential.provider=generic", "credential.credentialStore=wincredman"}
	h := &managerHelper{name: "manager", store: &gcmWindowsStore{manager: windowsCredentials, namespace: gcmDefaultNamespace}}
	ctx := context.Background()
	q := Query{Protocol: "https", Host: oracleWindowsHost(t)}

	mustGitCredential(t, config, "approve", withOraclePassword(oracleQueryFields(q), "bob", "from-git"))
	requireNativeAnswer(t, h, q, "bob", "from-git")
	mustGitCredential(t, config, "approve", withOraclePassword(oracleQueryFields(q), "eve@corp", "second-account"))
	second := q
	second.Username = "eve@corp"
	requireNativeAnswer(t, h, second, "eve@corp", "second-account")

	if err := h.Store(ctx, q, Answer{Username: "bob", Password: []byte("updated-by-gogit")}); err != nil {
		t.Fatal(err)
	}
	first := q
	first.Username = "bob"
	out := mustGitCredential(t, config, "fill", oracleQueryFields(first))
	if out["username"] != "bob" || out["password"] != "updated-by-gogit" {
		t.Fatalf("git fill = %q", out)
	}
	if err := h.Store(ctx, q, Answer{Username: "zoe", Password: []byte("third-account")}); err != nil {
		t.Fatal(err)
	}
	third := q
	third.Username = "zoe"
	out = mustGitCredential(t, config, "fill", oracleQueryFields(third))
	if out["username"] != "zoe" || out["password"] != "third-account" {
		t.Fatalf("git fill = %q", out)
	}

	mustGitCredential(t, config, "reject", withOraclePassword(oracleQueryFields(third), "zoe", "third-account"))
	requireNativeMiss(t, h, third)
	if err := h.Erase(ctx, second, Answer{Username: "eve@corp", Password: []byte("second-account")}); err != nil {
		t.Fatal(err)
	}
	requireGitFillMisses(t, config, oracleQueryFields(second))
	if err := h.Erase(ctx, first, Answer{Username: "bob", Password: []byte("updated-by-gogit")}); err != nil {
		t.Fatal(err)
	}
	requireGitFillMisses(t, config, oracleQueryFields(q))
}
