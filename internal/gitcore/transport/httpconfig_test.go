package transport

import (
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/config"
)

func testGitConfig(t *testing.T, text string) *config.Config {
	t.Helper()
	path := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatalf("WriteFile returned error %v", err)
	}
	cfg, err := config.Load(config.Options{GlobalFile: path, NoSystem: true})
	if err != nil {
		t.Fatalf("config.Load returned error %v", err)
	}
	return cfg
}

func clearProxyEnvironment(t *testing.T) {
	t.Helper()
	for _, name := range []string{"http_proxy", "HTTP_PROXY", "https_proxy", "HTTPS_PROXY", "all_proxy", "ALL_PROXY", "no_proxy", "NO_PROXY"} {
		t.Setenv(name, "")
	}
}

func advertiseV1Handler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/x-git-upload-pack-advertisement")
	_, _ = w.Write(httpV1Discovery(UploadPack, testHeadCaps, [][2]string{{idOf(1).String(), "refs/heads/main"}}))
}

func advertiseThrough(t *testing.T, rawURL string, opts Options) error {
	t.Helper()
	opts.Version = 1
	session, err := Dial(t.Context(), rawURL, UploadPack, opts)
	if err != nil {
		return err
	}
	defer func() { _ = session.Close() }()
	_, err = session.Advertise(t.Context())
	return err
}

var settingsEndpoint = Endpoint{Scheme: SchemeHTTPS, User: "alice", Host: "git.example.com", Path: "/team/repo.git"}

func TestResolveHTTPSettingsUsesGitDefaultsWithoutConfig(t *testing.T) {
	s, err := resolveHTTPSettings(nil, "origin", settingsEndpoint)
	if err != nil {
		t.Fatalf("resolveHTTPSettings returned error %v", err)
	}
	if !s.sslVerify || s.proxySet || s.postBuffer != defaultPostBuffer || s.lowSpeedLimit != 0 || len(s.extraHeaders) != 0 {
		t.Fatalf("settings = %+v, want the git defaults", s)
	}
}

func TestResolveHTTPSettingsPicksTheMostSpecificURL(t *testing.T) {
	tests := []struct {
		name   string
		config string
		want   string
	}{
		{"plain key", "[http]\n\tproxy = plain:1\n", "plain:1"},
		{"url key beats a later plain key", "[http \"https://git.example.com\"]\n\tproxy = url:1\n[http]\n\tproxy = plain:1\n", "url:1"},
		{"longer path wins", "[http \"https://git.example.com/team/repo.git\"]\n\tproxy = long:1\n[http \"https://git.example.com/team\"]\n\tproxy = short:1\n", "long:1"},
		{"matching user wins at an equal path", "[http \"https://alice@git.example.com\"]\n\tproxy = user:1\n[http \"https://git.example.com\"]\n\tproxy = anyone:1\n", "user:1"},
		{"later entry of an equal rank wins", "[http \"https://git.example.com\"]\n\tproxy = first:1\n\tproxy = second:1\n", "second:1"},
		{"wildcard host matches", "[http \"https://*.example.com\"]\n\tproxy = wild:1\n", "wild:1"},
		{"exact host beats a wildcard", "[http \"https://git.example.com\"]\n\tproxy = exact:1\n[http \"https://*.example.com\"]\n\tproxy = wild:1\n", "exact:1"},
		{"explicit default port matches", "[http \"https://git.example.com:443\"]\n\tproxy = port:1\n", "port:1"},
		{"other port does not match", "[http \"https://git.example.com:8443\"]\n\tproxy = port:1\n", ""},
		{"other scheme does not match", "[http \"http://git.example.com\"]\n\tproxy = http:1\n", ""},
		{"other user does not match", "[http \"https://bob@git.example.com\"]\n\tproxy = bob:1\n", ""},
		{"path prefix ends at a slash", "[http \"https://git.example.com/te\"]\n\tproxy = partial:1\n", ""},
		{"other label count does not match", "[http \"https://example.com\"]\n\tproxy = short:1\n", ""},
		{"other host does not match", "[http \"https://other.example.com\"]\n\tproxy = other:1\n", ""},
		{"malformed wildcard does not match", "[http \"https://git.example.c[m\"]\n\tproxy = bad:1\n", ""},
		{"name without a host is ignored", "[http \"just-a-name\"]\n\tproxy = none:1\n", ""},
		{"unparsable url is ignored", "[http \"https://%zz\"]\n\tproxy = none:1\n", ""},
		{"less specific entry after a more specific one is ignored", "[http \"https://git.example.com/team\"]\n\tproxy = team:1\n[http]\n\tproxy = plain:1\n", "team:1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, err := resolveHTTPSettings(testGitConfig(t, tt.config), "", settingsEndpoint)
			if err != nil {
				t.Fatalf("resolveHTTPSettings returned error %v", err)
			}
			if s.proxy != tt.want || s.proxySet != (tt.want != "") {
				t.Fatalf("proxy = %q (set %v), want %q", s.proxy, s.proxySet, tt.want)
			}
		})
	}
}

func TestResolveHTTPSettingsRemoteProxyBeatsHTTPProxy(t *testing.T) {
	cfg := testGitConfig(t, "[http]\n\tproxy = http:1\n[remote \"origin\"]\n\tproxy =\n")
	s, err := resolveHTTPSettings(cfg, "origin", settingsEndpoint)
	if err != nil {
		t.Fatalf("resolveHTTPSettings returned error %v", err)
	}
	if s.proxy != "" || !s.proxySet {
		t.Fatalf("proxy = %q (set %v), want the empty remote proxy", s.proxy, s.proxySet)
	}
	unnamed, err := resolveHTTPSettings(cfg, "", settingsEndpoint)
	if err != nil {
		t.Fatalf("resolveHTTPSettings returned error %v", err)
	}
	if unnamed.proxy != "http:1" {
		t.Fatalf("proxy without a remote name = %q, want http:1", unnamed.proxy)
	}
}

func TestResolveHTTPSettingsCollectsAndResetsExtraHeaders(t *testing.T) {
	tests := []struct {
		name   string
		config string
		want   []string
	}{
		{"plain and url headers add up", "[http]\n\textraHeader = A: 1\n\textraHeader = B: 2\n[http \"https://git.example.com\"]\n\textraHeader = C: 3\n", []string{"A: 1", "B: 2", "C: 3"}},
		{"empty value resets the list", "[http]\n\textraHeader = A: 1\n[http \"https://git.example.com\"]\n\textraHeader =\n\textraHeader = C: 3\n[http]\n\textraHeader = D: 4\n", []string{"C: 3"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, err := resolveHTTPSettings(testGitConfig(t, tt.config), "", settingsEndpoint)
			if err != nil {
				t.Fatalf("resolveHTTPSettings returned error %v", err)
			}
			if !slices.Equal(s.extraHeaders, tt.want) {
				t.Fatalf("extraHeaders = %q, want %q", s.extraHeaders, tt.want)
			}
		})
	}
}

func TestResolveHTTPSettingsParsesTypedValues(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfg := testGitConfig(t, "[core]\n\tbare = false\n[http]\n\tunknownKey = 1\n\tsslVerify\n\tsslCAInfo = ~/ca.pem\n\tsslCAPath = /certs\n\tsslCert = /client.pem\n\tsslKey = /client.key\n\tlowSpeedLimit = 1k\n\tlowSpeedTime = 30\n\tpostBuffer = 10\n")
	s, err := resolveHTTPSettings(cfg, "", settingsEndpoint)
	if err != nil {
		t.Fatalf("resolveHTTPSettings returned error %v", err)
	}
	want := httpSettings{
		sslVerify:     true,
		caInfo:        filepath.Join(home, "/ca.pem"),
		caPath:        "/certs",
		sslCert:       "/client.pem",
		sslKey:        "/client.key",
		lowSpeedLimit: 1024,
		lowSpeedTime:  30,
		postBuffer:    minPostBuffer,
	}
	if s.sslVerify != want.sslVerify || s.caInfo != want.caInfo || s.caPath != want.caPath || s.sslCert != want.sslCert || s.sslKey != want.sslKey || s.lowSpeedLimit != want.lowSpeedLimit || s.lowSpeedTime != want.lowSpeedTime || s.postBuffer != want.postBuffer {
		t.Fatalf("settings = %+v, want %+v", s, want)
	}
	off, err := resolveHTTPSettings(testGitConfig(t, "[http]\n\tsslVerify = false\n"), "", settingsEndpoint)
	if err != nil || off.sslVerify {
		t.Fatalf("sslVerify = false gave %+v, %v", off, err)
	}
}

func TestResolveHTTPSettingsRejectsInvalidValues(t *testing.T) {
	for _, line := range []string{"sslVerify = maybe", "sslCAInfo = ~someone/ca.pem", "postBuffer = lots"} {
		t.Run(line, func(t *testing.T) {
			_, err := resolveHTTPSettings(testGitConfig(t, "[http]\n\t"+line+"\n"), "", settingsEndpoint)
			if err == nil || !strings.Contains(err.Error(), "http.") {
				t.Fatalf("resolveHTTPSettings returned %v, want an error naming the key", err)
			}
		})
	}
}

func TestDialFailsOnAnInvalidHTTPConfig(t *testing.T) {
	cfg := testGitConfig(t, "[http]\n\tsslVerify = maybe\n")
	if _, err := Dial(t.Context(), "https://git.example.com/repo.git", UploadPack, Options{Config: cfg}); err == nil {
		t.Fatalf("Dial succeeded with an invalid http.sslVerify")
	}
}

func TestResolveHTTPSettingsLetsTheEnvironmentOverrideConfig(t *testing.T) {
	t.Setenv("GIT_SSL_NO_VERIFY", "1")
	t.Setenv("GIT_SSL_CAINFO", "/env/ca.pem")
	t.Setenv("GIT_SSL_CAPATH", "/env/certs")
	t.Setenv("GIT_SSL_CERT", "/env/client.pem")
	t.Setenv("GIT_SSL_KEY", "/env/client.key")
	t.Setenv("GIT_HTTP_LOW_SPEED_LIMIT", "500")
	t.Setenv("GIT_HTTP_LOW_SPEED_TIME", "soon")
	cfg := testGitConfig(t, "[http]\n\tsslVerify = true\n\tsslCAInfo = /cfg/ca.pem\n\tlowSpeedTime = 9\n")
	s, err := resolveHTTPSettings(cfg, "", settingsEndpoint)
	if err != nil {
		t.Fatalf("resolveHTTPSettings returned error %v", err)
	}
	if s.sslVerify || s.caInfo != "/env/ca.pem" || s.caPath != "/env/certs" || s.sslCert != "/env/client.pem" || s.sslKey != "/env/client.key" || s.lowSpeedLimit != 500 || s.lowSpeedTime != 0 {
		t.Fatalf("settings = %+v, want the environment values", s)
	}
}

func TestURLMatchLessRanksHostThenPathThenUser(t *testing.T) {
	tests := []struct {
		a, b urlMatch
		want bool
	}{
		{urlMatch{hostLen: 3}, urlMatch{hostLen: 4}, true},
		{urlMatch{hostLen: 4, pathLen: 9}, urlMatch{hostLen: 3}, false},
		{urlMatch{hostLen: 3, pathLen: 1}, urlMatch{hostLen: 3, pathLen: 2}, true},
		{urlMatch{hostLen: 3, pathLen: 2}, urlMatch{hostLen: 3, pathLen: 1}, false},
		{urlMatch{hostLen: 3}, urlMatch{hostLen: 3, user: true}, true},
		{urlMatch{hostLen: 3, user: true}, urlMatch{hostLen: 3}, false},
		{urlMatch{hostLen: 3}, urlMatch{hostLen: 3}, false},
	}
	for _, tt := range tests {
		if got := tt.a.less(tt.b); got != tt.want {
			t.Errorf("%+v.less(%+v) = %v, want %v", tt.a, tt.b, got, tt.want)
		}
	}
}

func smallPostBuffer(t *testing.T) *config.Config {
	t.Helper()
	return testGitConfig(t, "[http]\n\tpostBuffer = 1\n")
}
