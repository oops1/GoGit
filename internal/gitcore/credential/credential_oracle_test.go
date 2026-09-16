//go:build oracle

package credential

import (
	"bytes"
	"context"
	"crypto/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func oracleHost() string {
	return "gogit-oracle-" + strings.ToLower(rand.Text()) + ".invalid"
}

func oracleGitEnv(t *testing.T) []string {
	t.Helper()
	global := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(global, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	env := make([]string, 0, len(os.Environ())+8)
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		upper := strings.ToUpper(name)
		if strings.HasPrefix(upper, "GIT_") || strings.HasPrefix(upper, "GCM_") {
			continue
		}
		env = append(env, kv)
	}
	return append(env,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL="+global,
		"GIT_TERMINAL_PROMPT=0",
		"GIT_ASKPASS=",
		"SSH_ASKPASS=",
		"GCM_INTERACTIVE=never",
		"GCM_GUI_PROMPT=false",
	)
}

func runGitCredential(t *testing.T, config []string, action string, fields map[string]string) (map[string]string, error) {
	t.Helper()
	args := []string{"-c", "credential.helper="}
	for _, c := range config {
		args = append(args, "-c", c)
	}
	args = append(args, "credential", action)
	var input strings.Builder
	for _, key := range []string{"protocol", "host", "path", "username", "password"} {
		if v, ok := fields[key]; ok {
			input.WriteString(key + "=" + v + "\n")
		}
	}
	input.WriteString("\n")
	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Env = oracleGitEnv(t)
	cmd.Stdin = strings.NewReader(input.String())
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	out := map[string]string{}
	for line := range strings.Lines(stdout.String()) {
		if key, value, ok := strings.Cut(strings.TrimRight(line, "\r\n"), "="); ok {
			out[key] = value
		}
	}
	if err != nil && testing.Verbose() {
		t.Logf("git credential %s: %v: %s", action, err, stderr.String())
	}
	return out, err
}

func mustGitCredential(t *testing.T, config []string, action string, fields map[string]string) map[string]string {
	t.Helper()
	out, err := runGitCredential(t, config, action, fields)
	if err != nil {
		t.Fatalf("git credential %s failed: %v", action, err)
	}
	return out
}

func requireGitFillMisses(t *testing.T, config []string, fields map[string]string) {
	t.Helper()
	if out, err := runGitCredential(t, config, "fill", fields); err == nil {
		t.Fatalf("git credential fill still returns %q for %s", out["username"], fields["host"])
	}
}

func requireCredentialManagerInstalled(t *testing.T) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", "credential-manager", "--version")
	cmd.Env = oracleGitEnv(t)
	if err := cmd.Run(); err != nil {
		t.Skip("git-credential-manager is not installed")
	}
}

func oracleQueryFields(q Query) map[string]string {
	fields := map[string]string{"protocol": q.Protocol, "host": q.Host}
	if q.Path != "" {
		fields["path"] = q.Path
	}
	if q.Username != "" {
		fields["username"] = q.Username
	}
	return fields
}

func withOraclePassword(fields map[string]string, username, password string) map[string]string {
	out := map[string]string{}
	for k, v := range fields {
		out[k] = v
	}
	out["username"] = username
	out["password"] = password
	return out
}

func requireNativeAnswer(t *testing.T, h Helper, q Query, username, password string) {
	t.Helper()
	ans, ok, err := h.Get(context.Background(), q)
	if err != nil || !ok || ans.Username != username || string(ans.Password) != password {
		t.Fatalf("%s Get(%+v) = %q/%q, %v, %v; want %q/%q", h.Name(), q, ans.Username, ans.Password, ok, err, username, password)
	}
}

func requireNativeMiss(t *testing.T, h Helper, q Query) {
	t.Helper()
	if ans, ok, err := h.Get(context.Background(), q); ok || err != nil {
		t.Fatalf("%s Get(%+v) = %q, %v, %v; want a miss", h.Name(), q, ans.Username, ok, err)
	}
}

func TestOracleCredentialManagerPlaintextStoreIsSharedWithGit(t *testing.T) {
	requireCredentialManagerInstalled(t)
	root := filepath.Join(t.TempDir(), "store")
	config := []string{
		"credential.helper=manager",
		"credential.provider=generic",
		"credential.credentialStore=plaintext",
		"credential.plaintextStorePath=" + filepath.ToSlash(root),
	}
	h := &managerHelper{name: "manager", store: &gcmFileStore{root: root, namespace: gcmDefaultNamespace}}
	ctx := context.Background()

	q := Query{Protocol: "https", Host: oracleHost()}
	mustGitCredential(t, config, "approve", withOraclePassword(oracleQueryFields(q), "bob", "from-git"))
	requireNativeAnswer(t, h, q, "bob", "from-git")
	if err := h.Erase(ctx, q, Answer{Username: "bob", Password: []byte("from-git")}); err != nil {
		t.Fatal(err)
	}
	requireGitFillMisses(t, config, oracleQueryFields(q))

	q = Query{Protocol: "https", Host: oracleHost() + ":8443"}
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
