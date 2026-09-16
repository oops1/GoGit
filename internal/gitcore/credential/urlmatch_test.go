package credential

import "testing"

func TestNormalizeURLFollowsGitURLNormalization(t *testing.T) {
	cases := []struct {
		raw   string
		globs bool
		want  urlInfo
		ok    bool
	}{
		{"HTTPS://Bob:Secret@Example.COM:0443/a/./b/../c/%7euser%2f?q=%41#f", false, urlInfo{scheme: "https", hasUser: true, user: "Bob", host: "example.com", path: "/a/c/~user%2F?q=A#f"}, true},
		{"https://@h", false, urlInfo{scheme: "https", hasUser: true, host: "h", path: "/"}, true},
		{"http://h:80", false, urlInfo{scheme: "http", host: "h", path: "/"}, true},
		{"http://h:8080/", false, urlInfo{scheme: "http", host: "h", port: "8080", path: "/"}, true},
		{"https://h:", false, urlInfo{scheme: "https", host: "h", path: "/"}, true},
		{"https://[::1]:8443/x", false, urlInfo{scheme: "https", host: "[::1]", port: "8443", path: "/x"}, true},
		{"https://[::1]/x", false, urlInfo{scheme: "https", host: "[::1]", path: "/x"}, true},
		{"file:///etc", false, urlInfo{scheme: "file", path: "/etc"}, true},
		{"file://", false, urlInfo{scheme: "file", path: "/"}, true},
		{"https://*.example.com", true, urlInfo{scheme: "https", host: "*.example.com", path: "/"}, true},
		{"https://h/a/..", false, urlInfo{scheme: "https", host: "h", path: "/"}, true},
		{"https://h/./a", false, urlInfo{scheme: "https", host: "h", path: "/a"}, true},
		{"https://h/%00%20a\x7f\xc3", false, urlInfo{scheme: "https", host: "h", path: "/%00%20a%7F%C3"}, true},
		{"https://*.example.com", false, urlInfo{}, false},
		{"https://h:00", false, urlInfo{}, false},
		{"https://h:65536", false, urlInfo{}, false},
		{"https://h:123456", false, urlInfo{}, false},
		{"https://h:8a", false, urlInfo{}, false},
		{"file://:8/x", false, urlInfo{}, false},
		{"https:///x", false, urlInfo{}, false},
		{"https://", false, urlInfo{}, false},
		{"1http://h", false, urlInfo{}, false},
		{"https:/h", false, urlInfo{}, false},
		{"", false, urlInfo{}, false},
		{"https://ho st", false, urlInfo{}, false},
		{"https://bad%zz@h", false, urlInfo{}, false},
		{"https://h/%z", false, urlInfo{}, false},
		{"https://h/%zz", false, urlInfo{}, false},
		{"https://h/a?%", false, urlInfo{}, false},
		{"https://h/..", false, urlInfo{}, false},
	}
	for _, c := range cases {
		got, ok := normalizeURL(c.raw, c.globs)
		if ok != c.ok || got != c.want {
			t.Errorf("normalizeURL(%q, %v) = %+v, %v; want %+v, %v", c.raw, c.globs, got, ok, c.want, c.ok)
		}
	}
}

func TestURLMatchesComparesEveryPart(t *testing.T) {
	url := urlInfo{scheme: "https", hasUser: true, user: "bob", host: "git.example.com", port: "8443", path: "/org/repo.git"}
	cases := []struct {
		pattern urlInfo
		want    bool
	}{
		{urlInfo{scheme: "https", host: "git.example.com", port: "8443", path: "/"}, true},
		{urlInfo{scheme: "https", hasUser: true, user: "bob", host: "*.example.com", port: "8443", path: "/org/"}, true},
		{urlInfo{scheme: "http", host: "git.example.com", port: "8443", path: "/"}, false},
		{urlInfo{scheme: "https", hasUser: true, user: "eve", host: "git.example.com", port: "8443", path: "/"}, false},
		{urlInfo{scheme: "https", host: "example.com", port: "8443", path: "/"}, false},
		{urlInfo{scheme: "https", host: "git.example.com", path: "/"}, false},
		{urlInfo{scheme: "https", host: "git.example.com", port: "8443", path: "/organization"}, false},
	}
	for _, c := range cases {
		if got := urlMatches(url, c.pattern); got != c.want {
			t.Errorf("urlMatches(%+v) = %v, want %v", c.pattern, got, c.want)
		}
	}
	if urlMatches(urlInfo{scheme: "https", host: "h", path: "/"}, urlInfo{scheme: "https", hasUser: true, host: "h", path: "/"}) {
		t.Error("a pattern with a user must not match a url without one")
	}
}

func TestURLHostAndPathMatching(t *testing.T) {
	hosts := []struct {
		host, pattern string
		want          bool
	}{
		{"a.b", "*.b", true},
		{"a.b.c", "*.c", false},
		{"a.", "a", true},
		{"", "", true},
		{"a", "", false},
	}
	for _, c := range hosts {
		if got := urlHostMatches(c.host, c.pattern); got != c.want {
			t.Errorf("urlHostMatches(%q, %q) = %v", c.host, c.pattern, got)
		}
	}
	paths := []struct {
		path, prefix string
		want         bool
	}{
		{"/org/x", "/org/", true},
		{"/organization", "/org", false},
		{"/org", "/org", true},
		{"/other", "/org", false},
		{"", "/", true},
		{"x", "", false},
		{"/a", "", true},
	}
	for _, c := range paths {
		if got := urlPathMatches(c.path, c.prefix); got != c.want {
			t.Errorf("urlPathMatches(%q, %q) = %v", c.path, c.prefix, got)
		}
	}
}

func TestCredentialURLPercentEncodesLikeGit(t *testing.T) {
	if got := percentEncode("a/b@c d\x01é", true, false); got != "a%2Fb%40c%20d%01%C3%A9" {
		t.Errorf("username = %q", got)
	}
	if got := percentEncode("h_x:8443[]", false, true); got != "h%5Fx:8443[]" {
		t.Errorf("host = %q", got)
	}
	if got := percentEncode("a/b:c", false, false); got != "a/b%3Ac" {
		t.Errorf("path = %q", got)
	}
	if got := credentialURL(Query{Protocol: "https", Host: "h", Path: "org/r", Username: "u"}); got != "https://u@h/org/r" {
		t.Errorf("credentialURL = %q", got)
	}
	if got := credentialURL(Query{Protocol: "https", Host: "h"}); got != "https://h" {
		t.Errorf("credentialURL = %q", got)
	}
}

func TestURLDecodeKeepsInvalidEscapes(t *testing.T) {
	if got := urlDecode("a%41%zz%4"); got != "aA%zz%4" {
		t.Fatalf("urlDecode = %q", got)
	}
}

func TestParsePartialCredentialURLFollowsGit(t *testing.T) {
	cases := []struct {
		raw  string
		want partialCredential
		ok   bool
	}{
		{"example.com", partialCredential{host: "example.com", hasHost: true}, true},
		{"https://", partialCredential{protocol: "https", hasProtocol: true}, true},
		{"://h", partialCredential{host: "h", hasHost: true}, true},
		{"u:p@h/a/b//", partialCredential{username: "u", hasUser: true, host: "h", hasHost: true, path: "a/b", hasPath: true}, true},
		{"u@h", partialCredential{username: "u", hasUser: true, host: "h", hasHost: true}, true},
		{"h:8@x", partialCredential{username: "h", hasUser: true, host: "x", hasHost: true}, true},
		{"@h", partialCredential{hasUser: true, host: "h", hasHost: true}, true},
		{"h/%2F", partialCredential{host: "h", hasHost: true, path: "/", hasPath: true}, true},
		{"h/?q", partialCredential{host: "h", hasHost: true, path: "?q", hasPath: true}, true},
		{"h/", partialCredential{host: "h", hasHost: true}, true},
		{"h%0a", partialCredential{}, false},
		{"u:%0a@h", partialCredential{}, false},
	}
	for _, c := range cases {
		got, ok := parsePartialCredentialURL(c.raw)
		if ok != c.ok || got != c.want {
			t.Errorf("parsePartialCredentialURL(%q) = %+v, %v; want %+v, %v", c.raw, got, ok, c.want, c.ok)
		}
	}
}

func TestPartialCredentialMatchesOnlyTheGivenParts(t *testing.T) {
	q := Query{Protocol: "https", Host: "h:8443", Path: "org/repo", Username: "bob"}
	cases := []struct {
		pattern partialCredential
		want    bool
	}{
		{partialCredential{}, true},
		{partialCredential{protocol: "https", hasProtocol: true, host: "h:8443", hasHost: true, path: "org/repo", hasPath: true, username: "bob", hasUser: true}, true},
		{partialCredential{protocol: "http", hasProtocol: true}, false},
		{partialCredential{host: "h", hasHost: true}, false},
		{partialCredential{path: "org", hasPath: true}, false},
		{partialCredential{hasUser: true}, false},
	}
	for _, c := range cases {
		if got := c.pattern.matches(q); got != c.want {
			t.Errorf("%+v.matches = %v, want %v", c.pattern, got, c.want)
		}
	}
	if (partialCredential{username: "bob", hasUser: true}).matches(Query{Protocol: "https", Host: "h"}) {
		t.Error("a user pattern must not match a query without a user")
	}
}

func TestCredentialScopeMatchesFallsBackToPartialURLs(t *testing.T) {
	q := Query{Protocol: "https", Host: "my_host"}
	target, ok := normalizeURL(credentialURL(q), false)
	if ok {
		t.Fatalf("a host with %%5F must not normalize, got %+v", target)
	}
	if credentialScopeMatches("https://my_host", target, ok, q) {
		t.Error("a url scope cannot match a url that does not normalize")
	}
	if !credentialScopeMatches("my_host", target, ok, q) {
		t.Error("a partial scope must still match the host")
	}
	if credentialScopeMatches("my%0ahost", target, ok, q) {
		t.Error("a scope with a newline must be skipped")
	}
}
