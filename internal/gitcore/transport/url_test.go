package transport

import (
	"errors"
	"strings"
	"testing"
)

func TestParseURLTable(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want Endpoint
	}{
		{"https-plain", "https://github.com/user/repo.git", Endpoint{Scheme: SchemeHTTPS, Host: "github.com", Path: "/user/repo.git"}},
		{"http-plain", "http://example.com/repo.git", Endpoint{Scheme: SchemeHTTP, Host: "example.com", Path: "/repo.git"}},
		{"https-user", "https://user@github.com/repo.git", Endpoint{Scheme: SchemeHTTPS, User: "user", Host: "github.com", Path: "/repo.git"}},
		{"https-user-password", "https://user:pass@github.com/repo.git", Endpoint{Scheme: SchemeHTTPS, User: "user", Host: "github.com", Path: "/repo.git"}},
		{"https-port", "https://github.com:8443/repo.git", Endpoint{Scheme: SchemeHTTPS, Host: "github.com", Port: "8443", Path: "/repo.git"}},
		{"git-plain", "git://github.com/user/repo.git", Endpoint{Scheme: SchemeGit, Host: "github.com", Path: "/user/repo.git"}},
		{"git-port", "git://github.com:9418/repo.git", Endpoint{Scheme: SchemeGit, Host: "github.com", Port: "9418", Path: "/repo.git"}},
		{"ssh-explicit", "ssh://git@github.com/user/repo.git", Endpoint{Scheme: SchemeSSH, User: "git", Host: "github.com", Path: "/user/repo.git"}},
		{"ssh-explicit-port", "ssh://git@github.com:2222/user/repo.git", Endpoint{Scheme: SchemeSSH, User: "git", Host: "github.com", Port: "2222", Path: "/user/repo.git"}},
		{"ssh-explicit-ipv6", "ssh://[::1]:22/repo.git", Endpoint{Scheme: SchemeSSH, Host: "::1", Port: "22", Path: "/repo.git"}},
		{"scp-user-host", "user@host.com:path/to/repo.git", Endpoint{Scheme: SchemeSSH, User: "user", Host: "host.com", Path: "path/to/repo.git"}},
		{"scp-host-only", "host.com:repo.git", Endpoint{Scheme: SchemeSSH, Host: "host.com", Path: "repo.git"}},
		{"scp-github-style", "git@github.com:user/repo.git", Endpoint{Scheme: SchemeSSH, User: "git", Host: "github.com", Path: "user/repo.git"}},
		{"scp-ipv6-no-user", "[::1]:path/to/repo", Endpoint{Scheme: SchemeSSH, Host: "::1", Path: "path/to/repo"}},
		{"scp-ipv6-user", "user@[::1]:path/to/repo", Endpoint{Scheme: SchemeSSH, User: "user", Host: "::1", Path: "path/to/repo"}},
		{"scp-colon-in-path", "git@github.com:group/repo:branch.git", Endpoint{Scheme: SchemeSSH, User: "git", Host: "github.com", Path: "group/repo:branch.git"}},
		{"scp-no-slash-form", "a:b", Endpoint{Scheme: SchemeSSH, Host: "a", Path: "b"}},
		{"scp-drive-relative-ambiguous", "C:repo", Endpoint{Scheme: SchemeSSH, Host: "C", Path: "repo"}},
		{"scp-scheme-lookalike", "http:repo", Endpoint{Scheme: SchemeSSH, Host: "http", Path: "repo"}},
		{"windows-drive-backslash", `C:\repo`, Endpoint{Scheme: SchemeFile, Path: `C:\repo`}},
		{"windows-drive-forwardslash", "C:/repo", Endpoint{Scheme: SchemeFile, Path: "C:/repo"}},
		{"windows-drive-lowercase", `c:\repo`, Endpoint{Scheme: SchemeFile, Path: `c:\repo`}},
		{"windows-drive-deep-path", `D:\Projects\x`, Endpoint{Scheme: SchemeFile, Path: `D:\Projects\x`}},
		{"posix-absolute", "/home/user/repo.git", Endpoint{Scheme: SchemeFile, Path: "/home/user/repo.git"}},
		{"posix-relative-dot", "./relative/repo", Endpoint{Scheme: SchemeFile, Path: "./relative/repo"}},
		{"posix-relative-bare", "relative/repo.git", Endpoint{Scheme: SchemeFile, Path: "relative/repo.git"}},
		{"posix-bare-name", "repo.git", Endpoint{Scheme: SchemeFile, Path: "repo.git"}},
		{"local-colon-after-slash-dot", "./foo:bar", Endpoint{Scheme: SchemeFile, Path: "./foo:bar"}},
		{"local-colon-after-slash", "sub/foo:bar", Endpoint{Scheme: SchemeFile, Path: "sub/foo:bar"}},
		{"windows-unc", `\\server\share\repo`, Endpoint{Scheme: SchemeFile, Path: `\\server\share\repo`}},
		{"posix-unc-like", "//server/share", Endpoint{Scheme: SchemeFile, Path: "//server/share"}},
		{"file-uri-drive", "file:///C:/repo", Endpoint{Scheme: SchemeFile, Path: "C:/repo"}},
		{"file-uri-posix", "file:///home/user/repo.git", Endpoint{Scheme: SchemeFile, Path: "/home/user/repo.git"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, _, err := ParseURL(test.raw)
			if err != nil {
				t.Fatalf("ParseURL(%q) returned error %v", test.raw, err)
			}
			if got != test.want {
				t.Fatalf("ParseURL(%q) = %+v, want %+v", test.raw, got, test.want)
			}
		})
	}
}

func TestParseURLPasswordIsReturnedSeparately(t *testing.T) {
	endpoint, password, err := ParseURL("https://user:s3cr3t@github.com/repo.git")
	if err != nil {
		t.Fatalf("ParseURL returned error %v", err)
	}
	if endpoint.User != "user" {
		t.Fatalf("Endpoint.User = %q, want %q", endpoint.User, "user")
	}
	if string(password) != "s3cr3t" {
		t.Fatalf("password = %q, want %q", password, "s3cr3t")
	}
	if password.String() != redactedPassword {
		t.Fatalf("Password.String() = %q, leaked the secret", password.String())
	}
}

func TestParseURLNoPasswordReturnsNil(t *testing.T) {
	_, password, err := ParseURL("https://github.com/repo.git")
	if err != nil {
		t.Fatalf("ParseURL returned error %v", err)
	}
	if password != nil {
		t.Fatalf("password = %v, want nil", password)
	}
}

func TestPasswordWipeZeroesBytes(t *testing.T) {
	password := Password([]byte("secret"))
	password.Wipe()
	if password != nil {
		t.Fatalf("Wipe left password = %v, want nil", password)
	}
}

func TestPasswordBytes(t *testing.T) {
	password := Password([]byte("abc"))
	if string(password.Bytes()) != "abc" {
		t.Fatalf("Bytes() = %q, want %q", password.Bytes(), "abc")
	}
}

func TestParseURLErrors(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{"empty", ""},
		{"https-no-host", "https://"},
		{"ssh-no-host", "ssh://"},
		{"unsupported-scheme", "ftp://host/path"},
		{"invalid-percent-encoding", "https://host/%zz"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, _, err := ParseURL(test.raw); err == nil {
				t.Fatalf("ParseURL(%q) returned no error", test.raw)
			}
		})
	}
}

func TestParseURLUnsupportedSchemeWraps(t *testing.T) {
	_, _, err := ParseURL("ftp://host/path")
	if !errors.Is(err, ErrUnsupportedScheme) {
		t.Fatalf("ParseURL returned %v, want ErrUnsupportedScheme", err)
	}
}

func TestParseURLInvalidURLWraps(t *testing.T) {
	_, _, err := ParseURL("")
	if !errors.Is(err, ErrInvalidURL) {
		t.Fatalf("ParseURL returned %v, want ErrInvalidURL", err)
	}
}

func TestSplitSCPHostPathBracketWithoutClose(t *testing.T) {
	if _, _, ok := splitSCPHostPath("[::1"); ok {
		t.Fatalf("splitSCPHostPath accepted an unclosed bracket")
	}
}

func TestSplitSCPHostPathBracketWithoutColon(t *testing.T) {
	if _, _, ok := splitSCPHostPath("[::1]path"); ok {
		t.Fatalf("splitSCPHostPath accepted a bracket host without a following colon")
	}
}

func TestSplitSCPHostPathNoColon(t *testing.T) {
	if _, _, ok := splitSCPHostPath("nocolon"); ok {
		t.Fatalf("splitSCPHostPath accepted a string with no colon")
	}
}

func TestSplitSCPHostPathEmptyHost(t *testing.T) {
	if _, _, ok := splitSCPHostPath(":path"); ok {
		t.Fatalf("splitSCPHostPath accepted an empty host")
	}
}

func TestSplitSCPHostPathHostWithSlash(t *testing.T) {
	if _, _, ok := splitSCPHostPath("a/b:path"); ok {
		t.Fatalf("splitSCPHostPath accepted a host containing a slash")
	}
}

func TestParseSCPLikeRejectsUnsplittableRest(t *testing.T) {
	if _, ok := parseSCPLike("user@nocolon"); ok {
		t.Fatalf("parseSCPLike accepted a rest with no host/path separator")
	}
}

func TestParseFileURLWithEmptyPathFallsBackToOpaque(t *testing.T) {
	endpoint, _, err := ParseURL("file://")
	if err != nil {
		t.Fatalf("ParseURL returned error %v", err)
	}
	if endpoint != (Endpoint{Scheme: SchemeFile, Path: ""}) {
		t.Fatalf("ParseURL(\"file://\") = %+v", endpoint)
	}
}

func TestParseURLNoHostErrorRedactsThePassword(t *testing.T) {
	_, _, err := ParseURL("https://user:s3cr3t@")
	if err == nil {
		t.Fatalf("ParseURL succeeded for a userinfo-only url with no host")
	}
	if strings.Contains(err.Error(), "s3cr3t") {
		t.Fatalf("error %q leaked the password", err.Error())
	}
	if !strings.Contains(err.Error(), "xxxxx") {
		t.Fatalf("error %q, want the redacted password marker", err.Error())
	}
}

func TestEndpointIsLocal(t *testing.T) {
	fileEndpoint, _, err := ParseURL("/home/user/repo.git")
	if err != nil {
		t.Fatalf("ParseURL returned error %v", err)
	}
	if !fileEndpoint.IsLocal() {
		t.Fatalf("IsLocal() = false for a file endpoint")
	}
	httpEndpoint, _, err := ParseURL("https://github.com/repo.git")
	if err != nil {
		t.Fatalf("ParseURL returned error %v", err)
	}
	if httpEndpoint.IsLocal() {
		t.Fatalf("IsLocal() = true for an https endpoint")
	}
}
