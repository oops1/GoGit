package logx

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestRedactTextMasksSecrets(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"GitHubTokenSchemeHeader", "Authorization: token ghp_abc123", "Authorization: token ***"},
		{"LowerCaseBearerHeader", "authorization: bearer abc.def", "authorization: bearer ***"},
		{"UpperCaseBasicHeader", "AUTHORIZATION: BASIC dXNlcjpwYXNz", "AUTHORIZATION: BASIC ***"},
		{"BareAuthorizationHeader", "Authorization: abc123", "Authorization: ***"},
		{"ProxyAuthorizationHeader", "Proxy-Authorization: Basic dXNlcg==", "Proxy-Authorization: Basic ***"},
		{"MultiPartSchemeHeaderMaskedWhole", "Authorization: AWS4-HMAC-SHA256 Credential=AKIA/x, Signature=abc", "Authorization: ***"},
		{"HeaderWithoutSpaceAfterColon", "Authorization:Bearer abc", "Authorization:Bearer ***"},
		{"AuthorizationSuffixedPairMasked", "x_authorization_header=abc", "x_authorization_header=***"},
		{"HeaderStopsAtLineEnd", "Authorization: Bearer abc\nnext line", "Authorization: Bearer ***\nnext line"},
		{"CookieHeader", "Cookie: sid=abc; csrftoken=x", "Cookie: ***"},
		{"JSONAuthorizationHeader", `{"Authorization":"Bearer abc"}`, `{"Authorization":"Bearer ***}`},
		{"HeaderMapValue", "map[Authorization:[Bearer abc] Accept:[*/*]]", "map[Authorization:***] Accept:[*/*]]"},
		{"UpperCaseSchemeTokenURL", "HTTPS://tok@host/r.git", "HTTPS://***@host/r.git"},
		{"MixedCaseSchemePasswordURL", "Https://u:p@host/r", "Https://u:***@host/r"},
		{"PasswordWithAtSign", "https://u:p@ss@host/path", "https://u:***@host/path"},
		{"PasswordWithColon", "https://u:p:ss@host/path", "https://u:***@host/path"},
		{"SSHPasswordURL", "ssh://git:pw@host/r.git", "ssh://git:***@host/r.git"},
		{"FTPPasswordURL", "ftp://user:pw@host", "ftp://user:***@host"},
		{"HarmlessSSHUserURL", "ssh://git@github.com/o/r.git", "ssh://git@github.com/o/r.git"},
		{"GitPlusHTTPSTokenURL", "git+https://tok@host/r", "git+https://***@host/r"},
		{"AtSignInPathKept", "see https://example.com/a@b", "see https://example.com/a@b"},
		{"QuotedURLInError", `parse "https://u:hunter2@host:bad": invalid port`, `parse "https://u:***@host:bad": invalid port`},
		{"QueryStringToken", "GET /x?access_token=abc&x=1", "GET /x?access_token=***&x=1"},
		{"PasswordPair", "password=hunter2", "password=***"},
		{"PasswordPairWithSpaces", "password = hunter2", "password = ***"},
		{"JSONTokenPair", `{"token":"abc","id":1}`, `{"token":"***","id":1}`},
		{"StructPasswordField", "{Username:bob Password:hunter2}", "{Username:bob Password:***}"},
		{"MapTokenKey", "map[token:abc]", "map[token:***]"},
		{"BracketedByteField", "&{User:bob Password:[104 117 110]}", "&{User:bob Password:***}"},
		{"ParenthesisedValue", "Password:(a b) next", "Password:*** next"},
		{"UnbalancedBracketMaskedToEnd", "Password:[a b", "Password:***"},
		{"EmptyValueKept", "password=&x=1", "password=&x=1"},
		{"ValueWithEqualsMaskedWhole", "password=a=b c", "password=*** c"},
		{"PatKeyExactOnly", "pat=abc path=/x", "pat=*** path=/x"},
		{"PlainTextKept", "nothing secret here count=3", "nothing secret here count=3"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RedactText(tt.in); got != tt.want {
				t.Fatalf("RedactText(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

type loginConfig struct {
	Username string
	Password string
}

type remoteName string

func (r remoteName) String() string {
	return string(r)
}

func TestRedactHandlerMasksAttributes(t *testing.T) {
	tests := []struct {
		name   string
		keys   []string
		msg    string
		attrs  []any
		want   []string
		absent []string
	}{
		{name: "AccessTokenKey", attrs: []any{"access_token", "s3cr3t"}, want: []string{"access_token=***"}, absent: []string{"s3cr3t"}},
		{name: "RefreshTokenKey", attrs: []any{"refresh-token", "s3cr3t"}, want: []string{"refresh-token=***"}, absent: []string{"s3cr3t"}},
		{name: "XAuthTokenKey", attrs: []any{"X-Auth-Token", "s3cr3t"}, want: []string{"X-Auth-Token=***"}, absent: []string{"s3cr3t"}},
		{name: "PatKey", attrs: []any{"pat", "s3cr3t"}, want: []string{"pat=***"}, absent: []string{"s3cr3t"}},
		{name: "PwdKey", attrs: []any{"pwd", "s3cr3t"}, want: []string{"pwd=***"}, absent: []string{"s3cr3t"}},
		{name: "PasswdKey", attrs: []any{"passwd", "s3cr3t"}, want: []string{"passwd=***"}, absent: []string{"s3cr3t"}},
		{name: "UpperCasePassphraseKey", attrs: []any{"PASSPHRASE", "s3cr3t"}, want: []string{"PASSPHRASE=***"}, absent: []string{"s3cr3t"}},
		{name: "ClientSecretKey", attrs: []any{"client_secret", "s3cr3t"}, want: []string{"client_secret=***"}, absent: []string{"s3cr3t"}},
		{name: "APIKeyKey", attrs: []any{"api_key", "s3cr3t"}, want: []string{"api_key=***"}, absent: []string{"s3cr3t"}},
		{name: "PrivateKeyKey", attrs: []any{"private.key", "s3cr3t"}, want: []string{"private.key=***"}, absent: []string{"s3cr3t"}},
		{name: "CookieKey", attrs: []any{"Set-Cookie", "s3cr3t"}, want: []string{"Set-Cookie=***"}, absent: []string{"s3cr3t"}},
		{name: "CredentialHelperKey", attrs: []any{"credentialHelper", "s3cr3t"}, want: []string{"credentialHelper=***"}, absent: []string{"s3cr3t"}},
		{name: "HarmlessKeysKept", attrs: []any{"user", "alice", "id", "7", "url", "https://host/r", "remote", "origin", "count", 3, "error", "boom"}, want: []string{"user=alice", "id=7", "url=https://host/r", "remote=origin", "count=3", "error=boom"}},
		{name: "ByteSliceValue", attrs: []any{"body", []byte("password=hunter2")}, want: []string{`body="password=***"`}, absent: []string{"hunter2"}},
		{name: "StringerValue", attrs: []any{"remote", remoteName("https://u:pw@host")}, want: []string{"remote=https://u:***@host"}, absent: []string{"pw@"}},
		{name: "ErrorValue", attrs: []any{"error", errors.New("Authorization: token ghp_x")}, want: []string{`error="Authorization: token ***"`}, absent: []string{"ghp_x"}},
		{name: "StructValue", attrs: []any{"cfg", loginConfig{Username: "bob", Password: "hunter2"}}, want: []string{`cfg="{Username:bob Password:***}"`}, absent: []string{"hunter2"}},
		{name: "MapValue", attrs: []any{"m", map[string]string{"token": "abc"}}, want: []string{"m=map[token:***]"}, absent: []string{"abc"}},
		{name: "PointerToStructValue", attrs: []any{"cfg", &loginConfig{Username: "bob", Password: "hunter2"}}, want: []string{`cfg="&{Username:bob Password:***}"`}, absent: []string{"hunter2"}},
		{name: "SliceValue", attrs: []any{"args", []string{"-c", "password=hunter2"}}, want: []string{`args="[-c password=***]"`}, absent: []string{"hunter2"}},
		{name: "NilValue", attrs: []any{"x", nil}, want: []string{"x=<nil>"}},
		{name: "ScalarKindsKept", attrs: []any{slog.Int("count", 3), slog.Bool("ok", true), slog.Duration("elapsed", 2*time.Second)}, want: []string{"count=3", "ok=true", "elapsed=2s"}},
		{name: "NumberUnderSecretKey", attrs: []any{"token", 123456}, want: []string{"token=***"}, absent: []string{"123456"}},
		{name: "MessageURLMasked", msg: "clone HTTPS://tok@host failed", want: []string{`msg="clone HTTPS://***@host failed"`}, absent: []string{"tok@"}},
		{name: "CustomKeyMatchesByContainment", keys: []string{"Session"}, attrs: []any{"user_session_id", "s3cr3t"}, want: []string{"user_session_id=***"}, absent: []string{"s3cr3t"}},
		{name: "EmptyCustomKeyIgnored", keys: []string{"-_."}, attrs: []any{"user", "alice"}, want: []string{"user=alice"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			log := newRedactLogger(&buf, tt.keys...)
			msg := tt.msg
			if msg == "" {
				msg = "event"
			}
			log.Info(msg, tt.attrs...)
			out := buf.String()
			for _, w := range tt.want {
				if !strings.Contains(out, w) {
					t.Fatalf("output %q lacks %q", out, w)
				}
			}
			for _, a := range tt.absent {
				if strings.Contains(out, a) {
					t.Fatalf("output %q leaks %q", out, a)
				}
			}
		})
	}
}
