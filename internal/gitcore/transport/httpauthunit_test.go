package transport

import (
	"errors"
	"net/http"
	"testing"
)

func testAuthSession(t *testing.T, opts Options) *httpSession {
	t.Helper()
	s, err := newHTTPSession(Endpoint{Scheme: SchemeHTTP, Host: "example.com", Path: "/repo.git"}, nil, UploadPack, opts)
	if err != nil {
		t.Fatalf("newHTTPSession returned %v", err)
	}
	return s
}

func TestRequestHostIsEmptyForAnUnparsableURL(t *testing.T) {
	if got := requestHost("http://\x00"); got != "" {
		t.Fatalf("requestHost = %q, want empty", got)
	}
	if got := requestHost("https://git.example.com:8443/repo.git"); got != "git.example.com" {
		t.Fatalf("requestHost = %q, want git.example.com", got)
	}
}

func TestServerGeneratorWithoutCredentialsOrIntegratedFails(t *testing.T) {
	s := testAuthSession(t, Options{})
	if _, _, err := s.serverGenerator(t.Context(), schemeNTLM, nil, false); !errors.Is(err, ErrNoCredentials) {
		t.Fatalf("serverGenerator without a source = %v, want ErrNoCredentials", err)
	}
}

func TestServerGeneratorPropagatesCredentialErrors(t *testing.T) {
	boom := errors.New("boom")
	s := testAuthSession(t, Options{Credentials: &fakeCredentialSource{err: boom}})
	if _, _, err := s.serverGenerator(t.Context(), schemeNegotiate, nil, false); !errors.Is(err, boom) {
		t.Fatalf("serverGenerator = %v, want boom", err)
	}
}

func TestFetchConnectionCredentialsFallsBackToTheToken(t *testing.T) {
	s := testAuthSession(t, Options{Credentials: &fakeCredentialSource{creds: []Credentials{{Username: "alice", Token: []byte("t0ken")}}}})
	user, password, err := s.fetchConnectionCredentials(t.Context())
	if err != nil {
		t.Fatalf("fetchConnectionCredentials returned %v", err)
	}
	if user != "alice" || string(password) != "t0ken" {
		t.Fatalf("credentials = (%q, %q), want (alice, t0ken)", user, password)
	}
}

func TestUnauthorizedErrorClassifiesChallenges(t *testing.T) {
	withSource := testAuthSession(t, Options{Credentials: &fakeCredentialSource{}})
	noSource := testAuthSession(t, Options{})
	header := func(values ...string) http.Header {
		h := http.Header{}
		for _, v := range values {
			h.Add("WWW-Authenticate", v)
		}
		return h
	}
	cases := []struct {
		name    string
		session *httpSession
		header  http.Header
		want    error
	}{
		{"ntlm failed", withSource, header("NTLM"), ErrNTLMAuthFailed},
		{"negotiate failed", withSource, header("Negotiate"), ErrNegotiateAuthFailed},
		{"ntlm without credentials", noSource, header("NTLM"), ErrNoCredentials},
		{"digest only", withSource, header(`Digest realm="x"`), ErrAuthSchemeUnsupported},
		{"basic needs credentials", withSource, header(`Basic realm="x"`), ErrAuthRequired},
		{"no challenge with source", withSource, header(), ErrAuthRequired},
		{"no challenge no source", noSource, header(), ErrNoCredentials},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.session.unauthorizedError(c.header); !errors.Is(got, c.want) {
				t.Fatalf("unauthorizedError = %v, want %v", got, c.want)
			}
		})
	}
}

func TestScanForNTLMReturnsNilOnTruncatedInput(t *testing.T) {
	if got := scanForNTLM([]byte{0x30, 0x84, 0x01}); got != nil {
		t.Fatalf("scanForNTLM on truncated input = %x, want nil", got)
	}
}
