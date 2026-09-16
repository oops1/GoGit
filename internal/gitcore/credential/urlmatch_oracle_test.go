//go:build oracle

package credential

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/config"
)

type urlmatchOracleCase struct {
	name   string
	config string
	stores map[string][]string
	url    string
}

func TestOracleCredentialSettingsFollowGitURLMatching(t *testing.T) {
	const host = "example.invalid"
	bobHost := []string{"https://bob:host@" + host, "https://bob:path@" + host + "/org/repo.git"}
	bobOrganization := []string{"https://bob:host@" + host, "https://bob:path@" + host + "/organization/repo.git"}
	users := []string{
		"https://alice:alice-host@" + host, "https://alice:alice-path@" + host + "/org/repo.git",
		"https://carol:carol-host@" + host, "https://carol:carol-path@" + host + "/org/repo.git",
	}
	wildcard := []string{"https://bob:host@git.sub.invalid", "https://bob:path@git.sub.invalid/org/repo.git", "https://bob:deep@a.git.sub.invalid"}
	cases := []urlmatchOracleCase{
		{"no settings", "[credential]\n\thelper = store --file={A}\n", map[string][]string{"A": bobHost}, "https://example.invalid/org/repo.git"},
		{"unscoped use http path", "[credential]\n\thelper = store --file={A}\n\tuseHttpPath = true\n", map[string][]string{"A": bobHost}, "https://example.invalid/org/repo.git"},
		{"host scoped use http path", "[credential]\n\thelper = store --file={A}\n[credential \"https://example.invalid\"]\n\tuseHttpPath = true\n", map[string][]string{"A": bobHost}, "https://example.invalid/org/repo.git"},
		{"other host scoped use http path", "[credential]\n\thelper = store --file={A}\n[credential \"https://other.invalid\"]\n\tuseHttpPath = true\n", map[string][]string{"A": bobHost}, "https://example.invalid/org/repo.git"},
		{"path scoped use http path", "[credential]\n\thelper = store --file={A}\n[credential \"https://example.invalid/org\"]\n\tusehttppath = true\n", map[string][]string{"A": bobHost}, "https://example.invalid/org/repo.git"},
		{"path scope stops at a segment boundary", "[credential]\n\thelper = store --file={A}\n[credential \"https://example.invalid/org\"]\n\tusehttppath = true\n", map[string][]string{"A": bobOrganization}, "https://example.invalid/organization/repo.git"},
		{"later setting wins", "[credential \"https://example.invalid\"]\n\thelper = store --file={A}\n\tuseHttpPath = true\n[credential]\n\tuseHttpPath = false\n", map[string][]string{"A": bobHost}, "https://example.invalid/org/repo.git"},
		{"wildcard host", "[credential]\n\thelper = store --file={A}\n[credential \"https://*.sub.invalid\"]\n\tuseHttpPath = true\n", map[string][]string{"A": wildcard}, "https://git.sub.invalid/org/repo.git"},
		{"wildcard covers one label", "[credential \"https://*.sub.invalid\"]\n\thelper = store --file={A}\n", map[string][]string{"A": wildcard}, "https://a.git.sub.invalid/org/repo.git"},
		{"scheme host and default port are normalized", "[credential \"HTTPS://Example.INVALID:0443/\"]\n\thelper = store --file={A}\n\tuseHttpPath = true\n", map[string][]string{"A": bobHost}, "https://example.invalid/org/repo.git"},
		{"explicit port must match", "[credential \"https://example.invalid:8443\"]\n\thelper = store --file={A}\n", map[string][]string{"A": bobHost}, "https://example.invalid/org/repo.git"},
		{"dot segments in the scope", "[credential \"https://example.invalid/x/../org/./\"]\n\thelper = store --file={A}\n", map[string][]string{"A": bobHost}, "https://example.invalid/org/repo.git"},
		{"scoped username", "[credential]\n\thelper = store --file={A}\n[credential \"https://example.invalid\"]\n\tusername = carol\n", map[string][]string{"A": users}, "https://example.invalid/org/repo.git"},
		{"url username wins", "[credential]\n\thelper = store --file={A}\n[credential \"https://example.invalid\"]\n\tusername = carol\n", map[string][]string{"A": users}, "https://alice@example.invalid/org/repo.git"},
		{"user scoped helper", "[credential \"https://alice@example.invalid\"]\n\thelper = store --file={A}\n", map[string][]string{"A": users}, "https://alice@example.invalid/org/repo.git"},
		{"user scoped helper skips other users", "[credential \"https://alice@example.invalid\"]\n\thelper = store --file={A}\n", map[string][]string{"A": users}, "https://carol@example.invalid/org/repo.git"},
		{"configured username does not select user scopes", "[credential]\n\tusername = alice\n[credential \"https://alice@example.invalid\"]\n\thelper = store --file={A}\n", map[string][]string{"A": users}, "https://example.invalid/org/repo.git"},
		{"scoped helper runs after the global one", "[credential]\n\thelper = store --file={A}\n[credential \"https://example.invalid/org\"]\n\thelper = store --file={B}\n", map[string][]string{"A": nil, "B": bobHost}, "https://example.invalid/org/repo.git"},
		{"empty helper resets the list", "[credential]\n\thelper = store --file={A}\n[credential \"https://example.invalid\"]\n\thelper =\n\thelper = store --file={B}\n", map[string][]string{"A": bobHost, "B": {"https://bob:second@" + host}}, "https://example.invalid/org/repo.git"},
		{"scope without a scheme matches the host", "[credential \"example.invalid\"]\n\thelper = store --file={A}\n", map[string][]string{"A": bobHost}, "https://example.invalid/org/repo.git"},
		{"scope without a scheme and with a path", "[credential \"example.invalid/org/repo.git\"]\n\thelper = store --file={A}\n", map[string][]string{"A": bobHost}, "https://example.invalid/org/repo.git"},
		{"scope with only a scheme", "[credential \"https://\"]\n\thelper = store --file={A}\n", map[string][]string{"A": bobHost}, "https://example.invalid/org/repo.git"},
		{"scope for another scheme", "[credential \"http://example.invalid\"]\n\thelper = store --file={A}\n", map[string][]string{"A": bobHost}, "https://example.invalid/org/repo.git"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			runURLMatchOracleCase(t, c)
		})
	}
}

func runURLMatchOracleCase(t *testing.T, c urlmatchOracleCase) {
	dir := t.TempDir()
	content := c.config
	for name, lines := range c.stores {
		path := filepath.ToSlash(filepath.Join(dir, "store-"+name))
		body := strings.Join(lines, "\n")
		if body != "" {
			body += "\n"
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		content = strings.ReplaceAll(content, "{"+name+"}", path)
	}
	global := filepath.Join(dir, "gitconfig")
	if err := os.WriteFile(global, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(dir, "home")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatal(err)
	}

	cmd := exec.CommandContext(t.Context(), "git", "credential", "fill")
	cmd.Dir = home
	cmd.Env = append(oracleGitEnv(t), "GIT_CONFIG_GLOBAL="+global, "HOME="+home, "USERPROFILE="+home, "XDG_CONFIG_HOME="+home)
	cmd.Stdin = strings.NewReader("url=" + c.url + "\n\n")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	gitErr := cmd.Run()
	gitOut := map[string]string{}
	for line := range strings.Lines(stdout.String()) {
		if key, value, ok := strings.Cut(strings.TrimRight(line, "\r\n"), "="); ok {
			gitOut[key] = value
		}
	}

	if err := os.WriteFile(filepath.Join(dir, "config"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", "")
	cfg, err := config.Load(config.Options{GitDir: dir, NoSystem: true})
	if err != nil {
		t.Fatal(err)
	}
	q, err := QueryFromConfig(cfg, c.url)
	if err != nil {
		t.Fatal(err)
	}
	chain, _, err := FromConfig(cfg, c.url)
	if err != nil {
		t.Fatal(err)
	}
	ans, found, err := chain.Get(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}

	if gitErr != nil {
		if found {
			t.Fatalf("git found no credential (%v), Go.Git found %q/%q for %+v", gitErr, ans.Username, ans.Password, q)
		}
		return
	}
	if !found {
		t.Fatalf("git found %q, Go.Git found nothing for %+v", gitOut, q)
	}
	if gitOut["path"] != q.Path || gitOut["username"] != ans.Username || gitOut["password"] != string(ans.Password) {
		t.Fatalf("git = path %q user %q password %q; Go.Git = path %q user %q password %q", gitOut["path"], gitOut["username"], gitOut["password"], q.Path, ans.Username, ans.Password)
	}
}
