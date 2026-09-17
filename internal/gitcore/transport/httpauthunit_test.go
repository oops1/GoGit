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

func TestIntegratedTargetIsEmptyWithoutAHost(t *testing.T) {
	if got := (&httpSession{}).integratedTarget(); got != "" {
		t.Fatalf("integratedTarget = %q, want empty", got)
	}
	if got := testAuthSession(t, Options{}).integratedTarget(); got != "HTTP/example.com" {
		t.Fatalf("integratedTarget = %q, want HTTP/example.com", got)
	}
}

func TestNewAuthGeneratorWithoutCredentialsFailsOrUsesIntegrated(t *testing.T) {
	s := testAuthSession(t, Options{})
	gen, err := s.newAuthGenerator(t.Context(), schemeNTLM)
	if err == nil {
		gen.close()
		return
	}
	if !errors.Is(err, ErrNoCredentials) {
		t.Fatalf("newAuthGenerator without a source = %v, want ErrNoCredentials or an integrated generator", err)
	}
}

func TestNewAuthGeneratorPropagatesCredentialErrors(t *testing.T) {
	boom := errors.New("boom")
	s := testAuthSession(t, Options{Credentials: &fakeCredentialSource{err: boom}})
	if _, err := s.newAuthGenerator(t.Context(), schemeNegotiate); !errors.Is(err, boom) {
		t.Fatalf("newAuthGenerator = %v, want boom", err)
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
