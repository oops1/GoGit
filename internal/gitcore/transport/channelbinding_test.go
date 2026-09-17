package transport

import (
	"crypto/sha256"
	"crypto/sha512"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTLSServerEndPointPicksTheHashFromTheSignatureAlgorithm(t *testing.T) {
	raw := []byte("certificate")
	sum256 := sha256.Sum256(raw)
	sum384 := sha512.Sum384(raw)
	sum512 := sha512.Sum512(raw)
	cases := []struct {
		alg  x509.SignatureAlgorithm
		hash []byte
		md5  string
	}{
		{x509.MD5WithRSA, sum256[:], "a8398e96473163fef6cae76013e8ab5a"},
		{x509.SHA1WithRSA, sum256[:], "a8398e96473163fef6cae76013e8ab5a"},
		{x509.ECDSAWithSHA1, sum256[:], "a8398e96473163fef6cae76013e8ab5a"},
		{x509.SHA256WithRSA, sum256[:], "a8398e96473163fef6cae76013e8ab5a"},
		{x509.PureEd25519, sum256[:], "a8398e96473163fef6cae76013e8ab5a"},
		{x509.SHA384WithRSA, sum384[:], "739577dd5d88de0182832d9514adc780"},
		{x509.ECDSAWithSHA384, sum384[:], "739577dd5d88de0182832d9514adc780"},
		{x509.SHA384WithRSAPSS, sum384[:], "739577dd5d88de0182832d9514adc780"},
		{x509.SHA512WithRSA, sum512[:], "45b8f0cc0e9b980cb5ca628926d6b564"},
		{x509.ECDSAWithSHA512, sum512[:], "45b8f0cc0e9b980cb5ca628926d6b564"},
		{x509.SHA512WithRSAPSS, sum512[:], "45b8f0cc0e9b980cb5ca628926d6b564"},
	}
	for _, c := range cases {
		cert := &x509.Certificate{Raw: raw, SignatureAlgorithm: c.alg}
		got := tlsServerEndPoint(cert)
		if want := append([]byte(tlsServerEndPointPrefix), c.hash...); string(got) != string(want) {
			t.Errorf("%v: application data = %x, want %x", c.alg, got, want)
		}
		if gotMD5 := hex.EncodeToString(ntlmChannelBindingHash(got)); gotMD5 != c.md5 {
			t.Errorf("%v: MsvAvChannelBindings = %s, want %s", c.alg, gotMD5, c.md5)
		}
	}
}

func TestGSSChannelBindingsLayout(t *testing.T) {
	got := hex.EncodeToString(gssChannelBindings([]byte("ab")))
	if want := "00000000000000000000000000000000020000006162"; got != want {
		t.Fatalf("gss_channel_bindings_struct = %s, want %s", got, want)
	}
}

func TestChannelBindingIsAbsentWithoutACertificate(t *testing.T) {
	if channelBindingFromState(nil) != nil || channelBindingFromState(&tls.ConnectionState{}) != nil {
		t.Fatalf("a connection without a peer certificate must have no binding")
	}
	if ntlmChannelBindingHash(nil) != nil {
		t.Fatalf("no application data must give no MsvAvChannelBindings")
	}
}

func startBindingOrigin(t *testing.T, scheme string) (*httptest.Server, *ntlmTestServer) {
	t.Helper()
	handler := newNTLMTestServer(t, scheme, "hunter2")
	server := httptest.NewUnstartedServer(http.HandlerFunc(handler.handle))
	server.StartTLS()
	t.Cleanup(server.Close)
	handler.expectBinding = ntlmChannelBindingHash(tlsServerEndPoint(server.Certificate()))
	return server, handler
}

func TestHTTPSendsTheTLSChannelBindingToAnExtendedProtectionServer(t *testing.T) {
	for _, scheme := range []string{"NTLM", "Negotiate"} {
		t.Run(scheme, func(t *testing.T) {
			server, handler := startBindingOrigin(t, scheme)
			cfg := testGitConfig(t, "[http]\n\temptyAuth = false\n\tsslCAInfo = "+writeServerCertificate(t, server)+"\n")
			creds := &fakeCredentialSource{creds: []Credentials{{Username: `CORP\alice`, Password: []byte("hunter2")}}}
			if err := advertiseThrough(t, server.URL+"/repo.git", Options{Config: cfg, Credentials: creds}); err != nil {
				t.Fatalf("advertise returned error %v", err)
			}
			if handler.authenticated == 0 {
				t.Fatalf("the server did not accept the channel binding")
			}
		})
	}
}

func TestHTTPFailsAgainstAnExtendedProtectionServerExpectingAnotherBinding(t *testing.T) {
	server, handler := startBindingOrigin(t, "NTLM")
	handler.expectBinding = ntlmChannelBindingHash([]byte("tls-server-end-point:other"))
	cfg := testGitConfig(t, "[http]\n\temptyAuth = false\n\tsslCAInfo = "+writeServerCertificate(t, server)+"\n")
	creds := &fakeCredentialSource{creds: []Credentials{{Username: "alice", Password: []byte("hunter2")}}}
	if err := advertiseThrough(t, server.URL+"/repo.git", Options{Config: cfg, Credentials: creds}); !errors.Is(err, ErrNTLMAuthFailed) {
		t.Fatalf("advertise returned %v, want ErrNTLMAuthFailed", err)
	}
}
