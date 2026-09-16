package transport

import (
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func stubSSHHome(t *testing.T, home string, err error) {
	t.Helper()
	restoreHome := sshHomeDir
	sshHomeDir = func() (string, error) { return home, err }
	restoreUser := currentUser
	currentUser = func() (*user.User, error) { return &user.User{Username: `DOMAIN\local`}, nil }
	t.Cleanup(func() {
		sshHomeDir = restoreHome
		currentUser = restoreUser
	})
}

func writeSSHConfig(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSplitSSHConfigLine(t *testing.T) {
	tests := []struct {
		line string
		key  string
		args []string
	}{
		{line: "", key: ""},
		{line: "   # comment", key: ""},
		{line: "Host foo bar", key: "host", args: []string{"foo", "bar"}},
		{line: "  Port=2222\r\n", key: "port", args: []string{"2222"}},
		{line: "User = git", key: "user", args: []string{"git"}},
		{line: `IdentityFile "C:\Program Files\key" second`, key: "identityfile", args: []string{`C:\Program Files\key`, "second"}},
		{line: "HostName host # trailing", key: "hostname", args: []string{"host"}},
		{line: "Compression", key: "compression"},
	}
	for _, tt := range tests {
		key, args, err := splitSSHConfigLine(tt.line)
		if err != nil {
			t.Fatalf("splitSSHConfigLine(%q) returned error %v", tt.line, err)
		}
		if key != tt.key || !slices.Equal(args, tt.args) {
			t.Fatalf("splitSSHConfigLine(%q) = %q %q, want %q %q", tt.line, key, args, tt.key, tt.args)
		}
	}
	if _, _, err := splitSSHConfigLine(`IdentityFile "open`); !errors.Is(err, errUnterminatedQuote) {
		t.Fatalf("unterminated quote returned %v", err)
	}
}

func TestSSHHostMatches(t *testing.T) {
	tests := []struct {
		patterns []string
		host     string
		want     bool
	}{
		{patterns: []string{"*"}, host: "github.com", want: true},
		{patterns: []string{"GitHub.*"}, host: "github.COM", want: true},
		{patterns: []string{"git?ub.com"}, host: "github.com", want: true},
		{patterns: []string{"git?ub.com"}, host: "gitub.com", want: false},
		{patterns: []string{"*.example.com", "!secret.example.com"}, host: "secret.example.com", want: false},
		{patterns: []string{"*.example.com", "!secret.example.com"}, host: "open.example.com", want: true},
		{patterns: []string{"!secret"}, host: "open", want: false},
		{patterns: []string{"a,b*"}, host: "bee", want: true},
		{patterns: []string{"a*c"}, host: "abd", want: false},
		{patterns: []string{"abc"}, host: "ab", want: false},
		{patterns: []string{"abc?"}, host: "abc", want: false},
		{patterns: []string{"", "!"}, host: "x", want: false},
		{patterns: nil, host: "x", want: false},
	}
	for _, tt := range tests {
		if got := sshHostMatches(tt.patterns, tt.host); got != tt.want {
			t.Fatalf("sshHostMatches(%q, %q) = %v, want %v", tt.patterns, tt.host, got, tt.want)
		}
	}
}

func TestSSHConfigFirstObtainedValueWins(t *testing.T) {
	dir := t.TempDir()
	path := writeSSHConfig(t, dir, "config", strings.Join([]string{
		"User global",
		"Host work",
		"  HostName work.example.com",
		"  IdentityFile ~/.ssh/work",
		"Host *",
		"  HostName fallback.example.com",
		"  Port 2200",
		"  IdentityFile ~/.ssh/id_ed25519",
		"Match host other",
		"  Port 9999",
		"Match all",
		"  Compression yes",
		"Host",
		"  Port 1",
	}, "\n"))
	cfg, err := loadSSHConfig(SSHOptions{ConfigFiles: []string{path, filepath.Join(dir, "missing")}})
	if err != nil {
		t.Fatalf("loadSSHConfig returned error %v", err)
	}
	work := cfg.resolve("WORK")
	if work.value("hostname") != "work.example.com" || work.value("port") != "2200" || work.value("user") != "global" {
		t.Fatalf("work = %+v", work.values)
	}
	if !slices.Equal(work.identityFiles, []string{"~/.ssh/work", "~/.ssh/id_ed25519"}) {
		t.Fatalf("identity files = %q", work.identityFiles)
	}
	if work.value("compression") != "yes" {
		t.Fatalf("Match all did not apply: %+v", work.values)
	}
	if work.value("missing") != "" {
		t.Fatalf("an unset keyword has a value")
	}
}

func TestSSHConfigIncludeFollowsTheEnclosingHostBlock(t *testing.T) {
	dir := t.TempDir()
	writeSSHConfig(t, dir, "conf.d/10-work", "User from-include\nHost nested\n  Port 2022\n")
	writeSSHConfig(t, dir, "conf.d/20-other", "IdentityFile second\n")
	writeSSHConfig(t, dir, "inactive", "Port 1\n")
	path := writeSSHConfig(t, dir, "config", "Host work nested\n  Include conf.d/*\nHost never\n  Include "+filepath.Join(dir, "inactive")+"\n")

	cfg, err := loadSSHConfig(SSHOptions{ConfigFiles: []string{path}})
	if err != nil {
		t.Fatalf("loadSSHConfig returned error %v", err)
	}
	if got := cfg.resolve("work"); got.value("user") != "from-include" || got.value("port") != "" || !slices.Equal(got.identityFiles, []string{"second"}) {
		t.Fatalf("work = %+v %q", got.values, got.identityFiles)
	}
	if got := cfg.resolve("nested"); got.value("port") != "2022" {
		t.Fatalf("nested = %+v", got.values)
	}
	if got := cfg.resolve("other"); len(got.values) != 0 {
		t.Fatalf("other = %+v, want nothing from blocks for other hosts", got.values)
	}
}

func TestSSHConfigExpandsTheHomeInIncludes(t *testing.T) {
	home := t.TempDir()
	stubSSHHome(t, home, nil)
	writeSSHConfig(t, home, "extra", "Port 2323\n")
	path := writeSSHConfig(t, t.TempDir(), "config", "Include ~/extra\n")
	cfg, err := loadSSHConfig(SSHOptions{ConfigFiles: []string{path}})
	if err != nil {
		t.Fatalf("loadSSHConfig returned error %v", err)
	}
	if cfg.resolve("any").value("port") != "2323" {
		t.Fatalf("the include under the home directory was not read")
	}
}

func TestSSHConfigReportsBrokenFiles(t *testing.T) {
	dir := t.TempDir()
	loop := filepath.Join(dir, "loop")
	writeSSHConfig(t, dir, "loop", "Include "+loop+"\n")
	tests := map[string]SSHOptions{
		"bad override":    {Overrides: []string{`User "unterminated`}},
		"bad line":        {ConfigFiles: []string{writeSSHConfig(t, dir, "bad", "Host x\nUser \"open\n")}},
		"include loop":    {ConfigFiles: []string{loop}},
		"bad glob":        {ConfigFiles: []string{writeSSHConfig(t, dir, "glob", "Include [\n")}},
		"unreadable file": {ConfigFiles: []string{dir}},
		"broken include":  {ConfigFiles: []string{writeSSHConfig(t, dir, "outer", "Include bad\n")}},
	}
	for name, opts := range tests {
		if _, err := loadSSHConfig(opts); !errors.Is(err, ErrSSHConfig) {
			t.Fatalf("%s: loadSSHConfig returned %v, want ErrSSHConfig", name, err)
		}
	}
	if _, err := resolveSSHRoute(Endpoint{Host: "x"}, tests["bad line"]); !errors.Is(err, ErrSSHConfig) {
		t.Fatalf("resolveSSHRoute returned %v, want ErrSSHConfig", err)
	}
}

func TestResolveSSHRouteAppliesTheHostSettings(t *testing.T) {
	home := t.TempDir()
	stubSSHHome(t, home, nil)
	path := writeSSHConfig(t, t.TempDir(), "config", strings.Join([]string{
		"Host github-work",
		"  HostName %h.example.com",
		"  User configured",
		"  Port 2200",
		"  IdentityFile ~/.ssh/%n_%r_%u_%p",
		"  IdentityFile %d/abs%%%x%",
		"  IdentityFile relative/key",
		"  IdentityFile none",
		"  IdentitiesOnly yes",
		"  HostKeyAlias pinned",
		"  UserKnownHostsFile ~/.ssh/work_hosts none",
		"  StrictHostKeyChecking Accept-New",
	}, "\n"))

	hops, err := resolveSSHRoute(Endpoint{Host: "github-work", User: "git"}, SSHOptions{Overrides: []string{"Port 2400"}, ConfigFiles: []string{path}})
	if err != nil {
		t.Fatalf("resolveSSHRoute returned error %v", err)
	}
	if len(hops) != 1 {
		t.Fatalf("hops = %d, want 1", len(hops))
	}
	hop := hops[0]
	want := SSHTarget{Host: "github-work", HostName: "github-work.example.com", Port: "2400", User: "git", IdentitiesOnly: true}
	if hop.target.Host != want.Host || hop.target.HostName != want.HostName || hop.target.Port != want.Port || hop.target.User != want.User || !hop.target.IdentitiesOnly {
		t.Fatalf("target = %+v, want %+v", hop.target, want)
	}
	wantFiles := []string{
		filepath.Join(home, ".ssh", "github-work_git_local_2400"),
		filepath.Join(home, "abs%%x%"),
		filepath.Join(home, "relative", "key"),
	}
	if !slices.Equal(hop.target.IdentityFiles, wantFiles) {
		t.Fatalf("identity files = %q, want %q", hop.target.IdentityFiles, wantFiles)
	}
	if hop.addr != "github-work.example.com:2400" || hop.hostKeyName != "pinned:2400" || hop.strictHostKeys != "accept-new" {
		t.Fatalf("hop = %+v", hop)
	}
	if !slices.Equal(hop.knownHostsFiles, []string{filepath.Join(home, ".ssh", "work_hosts")}) {
		t.Fatalf("known hosts files = %q", hop.knownHostsFiles)
	}
}

func TestResolveSSHRouteDefaultsWithoutAConfig(t *testing.T) {
	stubSSHHome(t, "", errors.New("no home"))
	hops, err := resolveSSHRoute(Endpoint{Host: "example.com"}, SSHOptions{Overrides: []string{"IdentityFile ~/key", "IdentityFile rel"}})
	if err != nil {
		t.Fatalf("resolveSSHRoute returned error %v", err)
	}
	hop := hops[0]
	if hop.addr != "example.com:22" || hop.hostKeyName != hop.addr || hop.target.User != "local" {
		t.Fatalf("hop = %+v", hop)
	}
	if !slices.Equal(hop.target.IdentityFiles, []string{"~/key", "rel"}) {
		t.Fatalf("identity files = %q, want them untouched without a home", hop.target.IdentityFiles)
	}
}

func TestResolveSSHRouteBuildsTheProxyJumpChain(t *testing.T) {
	stubSSHHome(t, t.TempDir(), nil)
	options := SSHOptions{Overrides: []string{
		"Host target",
		"  ProxyJump alice@b1:2201,ssh://bob@b2:2202,[::1]:2203",
		"Host b1",
		"  HostName bastion-one.example.com",
		"  Port 9",
	}}
	hops, err := resolveSSHRoute(Endpoint{Host: "target", Port: "2222"}, SSHOptions{Overrides: []string{strings.Join(options.Overrides, "\n")}})
	if err != nil {
		t.Fatalf("resolveSSHRoute returned error %v", err)
	}
	var got []string
	for _, hop := range hops {
		got = append(got, hop.target.User+"@"+hop.addr)
	}
	want := []string{"alice@bastion-one.example.com:2201", "bob@b2:2202", "local@[::1]:2203", "local@target:2222"}
	if !slices.Equal(got, want) {
		t.Fatalf("route = %q, want %q", got, want)
	}
}

func TestResolveSSHRouteHandlesProxySettings(t *testing.T) {
	stubSSHHome(t, t.TempDir(), nil)
	tests := []struct {
		config  string
		hops    int
		wantErr error
	}{
		{config: "ProxyCommand ssh -W %h:%p bastion", wantErr: ErrProxyCommandUnsupported},
		{config: "ProxyJump none\nProxyCommand nc %h %p", wantErr: ErrProxyCommandUnsupported},
		{config: "ProxyCommand none", hops: 1},
		{config: "ProxyJump bastion\nProxyCommand nc %h %p", hops: 2},
		{config: "ProxyJump ssh://", wantErr: ErrSSHConfig},
		{config: "ProxyJump user@", wantErr: ErrSSHConfig},
		{config: "ProxyJump ssh://%zz", wantErr: ErrSSHConfig},
	}
	for _, tt := range tests {
		hops, err := resolveSSHRoute(Endpoint{Host: "target"}, SSHOptions{Overrides: []string{tt.config}})
		if tt.wantErr != nil {
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("%q: resolveSSHRoute returned %v, want %v", tt.config, err, tt.wantErr)
			}
			continue
		}
		if err != nil || len(hops) != tt.hops {
			t.Fatalf("%q: resolveSSHRoute = %d hops, %v; want %d hops", tt.config, len(hops), err, tt.hops)
		}
	}
}

func TestDefaultSSHConfigFilesListTheUserAndSystemConfigs(t *testing.T) {
	home := t.TempDir()
	stubSSHHome(t, home, nil)
	files := DefaultSSHConfigFiles()
	if len(files) == 0 || files[0] != filepath.Join(home, ".ssh", "config") {
		t.Fatalf("DefaultSSHConfigFiles = %q", files)
	}
	if system := systemSSHConfigPath(); system != "" && files[len(files)-1] != system {
		t.Fatalf("DefaultSSHConfigFiles = %q, want the system config %q last", files, system)
	}

	stubSSHHome(t, "", fmt.Errorf("no home"))
	for _, file := range DefaultSSHConfigFiles() {
		if strings.Contains(file, ".ssh") && !strings.HasPrefix(file, "/etc") && !strings.Contains(file, "ProgramData") {
			t.Fatalf("a user config %q was listed without a home directory", file)
		}
	}
}

func TestExpandUserHome(t *testing.T) {
	home := t.TempDir()
	stubSSHHome(t, home, nil)
	backslashed := `~\b`
	if os.IsPathSeparator('\\') {
		backslashed = filepath.Join(home, "b")
	}
	for input, want := range map[string]string{
		"~":        home,
		"~/a":      filepath.Join(home, "a"),
		`~\b`:      backslashed,
		"~other/x": "~other/x",
		"/abs":     "/abs",
	} {
		if got := expandUserHome(input); got != want {
			t.Fatalf("expandUserHome(%q) = %q, want %q", input, got, want)
		}
	}
}
