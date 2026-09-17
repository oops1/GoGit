package transport

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func armIntegratedNegotiate(t *testing.T, host string) {
	t.Helper()
	f := newKerberosFixtureForHost(t, host, time.Hour)
	useKerberosFiles(t, writeKrb5Conf(t, refusingKDC(t)), f.writeCCache(t, true, time.Now().Add(time.Hour)))
	if _, ok := newIntegratedGenerator(schemeNegotiate, host, nil); !ok {
		t.Skip("integrated Negotiate is unavailable on this platform without a real logon or KDC")
	}
}

func TestHTTPServerFallsBackToCredentialsWhenIntegratedNegotiateIsRejected(t *testing.T) {
	handler := newNTLMTestServer(t, "Negotiate", "hunter2")
	server := httptest.NewServer(http.HandlerFunc(handler.handle))
	t.Cleanup(server.Close)
	armIntegratedNegotiate(t, requestHost(server.URL))

	source := &feedbackRecorder{supply: []Credentials{{Username: `CORP\alice`, Password: []byte("hunter2")}}}
	if err := advertiseThrough(t, server.URL+"/repo.git", Options{Credentials: source}); err != nil {
		t.Fatalf("advertise returned error %v", err)
	}
	if handler.authenticated == 0 || len(source.asked) == 0 {
		t.Fatalf("integrated auth did not fall back to credentials: authenticated %d asked %q", handler.authenticated, source.asked)
	}
}

func TestHTTPProxyFallsBackToCredentialsWhenIntegratedNegotiateIsRejected(t *testing.T) {
	authority := newNTLMProxyAuthority(t, "Negotiate", "hunter2")
	proxy := startAuthenticatingProxy(t, authority, false)
	armIntegratedNegotiate(t, requestHost(proxy.server.URL))
	target := newProxyTarget(t, true)

	opts := proxyConfig(t, proxy.server.URL, target, "")
	source := &feedbackRecorder{supply: []Credentials{{Username: "alice", Password: []byte("hunter2")}}}
	opts.Credentials = source
	if err := advertiseThrough(t, target.url+"/repo.git", opts); err != nil {
		t.Fatalf("advertise returned error %v", err)
	}
	if authority.authenticated == 0 || len(source.asked) == 0 {
		t.Fatalf("integrated proxy auth did not fall back to credentials: authenticated %d asked %q", authority.authenticated, source.asked)
	}
}

func TestDialTLSRejectsAnAddressWithoutAPort(t *testing.T) {
	s := testAuthSession(t, Options{})
	if _, err := s.dialTLS(t.Context(), "tcp", "hostwithoutport"); err == nil {
		t.Fatalf("dialTLS accepted an address with no port")
	}
}

func TestHTTPServerIntegratedRejectionWithoutCredentialsReportsTheScheme(t *testing.T) {
	handler := newNTLMTestServer(t, "Negotiate", "hunter2")
	server := httptest.NewServer(http.HandlerFunc(handler.handle))
	t.Cleanup(server.Close)
	armIntegratedNegotiate(t, requestHost(server.URL))
	if err := advertiseThrough(t, server.URL+"/repo.git", Options{}); err == nil {
		t.Fatalf("advertise without credentials succeeded against a rejecting server")
	}
}
