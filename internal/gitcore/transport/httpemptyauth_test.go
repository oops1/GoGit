package transport

import "testing"

func TestResolveHTTPSettingsReadsEmptyAuth(t *testing.T) {
	cases := []struct {
		name   string
		config string
		want   emptyAuthMode
	}{
		{"unset means auto", "[http]\n\tproxy = p:1\n", emptyAuthAuto},
		{"auto", "[http]\n\temptyAuth = auto\n", emptyAuthAuto},
		{"true", "[http]\n\temptyAuth = true\n", emptyAuthOn},
		{"bare key is true", "[http]\n\temptyAuth\n", emptyAuthOn},
		{"false", "[http]\n\temptyAuth = false\n", emptyAuthOff},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, err := resolveHTTPSettings(testGitConfig(t, tc.config), "origin", settingsEndpoint)
			if err != nil {
				t.Fatalf("resolveHTTPSettings returned error %v", err)
			}
			if s.emptyAuth != tc.want {
				t.Fatalf("emptyAuth = %v, want %v", s.emptyAuth, tc.want)
			}
		})
	}
}

func TestResolveHTTPSettingsRejectsBadEmptyAuth(t *testing.T) {
	if _, err := resolveHTTPSettings(testGitConfig(t, "[http]\n\temptyAuth = maybe\n"), "origin", settingsEndpoint); err == nil {
		t.Fatalf("resolveHTTPSettings accepted a non-boolean emptyAuth")
	}
}

func TestResolveHTTPSettingsReadsProxyAuthMethodWithGitPrecedence(t *testing.T) {
	cfg := testGitConfig(t, "[http]\n\tproxyAuthMethod = Basic\n[remote \"origin\"]\n\tproxyAuthMethod = NTLM\n")
	s, err := resolveHTTPSettings(cfg, "", settingsEndpoint)
	if err != nil || s.proxyAuthMethod != "basic" {
		t.Fatalf("http.proxyAuthMethod gave %q, %v; want basic", s.proxyAuthMethod, err)
	}
	if s, _ = resolveHTTPSettings(cfg, "origin", settingsEndpoint); s.proxyAuthMethod != "ntlm" {
		t.Fatalf("remote.origin.proxyAuthMethod gave %q, want ntlm", s.proxyAuthMethod)
	}
	t.Setenv("GIT_HTTP_PROXY_AUTHMETHOD", "Negotiate")
	if s, _ = resolveHTTPSettings(cfg, "origin", settingsEndpoint); s.proxyAuthMethod != "negotiate" {
		t.Fatalf("GIT_HTTP_PROXY_AUTHMETHOD gave %q, want negotiate", s.proxyAuthMethod)
	}
}
