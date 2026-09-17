//go:build windows

package transport

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestSSPIIntegratedGeneratorProducesTokens(t *testing.T) {
	for _, scheme := range []string{schemeNTLM, schemeNegotiate} {
		t.Run(scheme, func(t *testing.T) {
			gen, ok, err := newIntegratedGenerator(scheme, "HTTP/localhost")
			if err != nil {
				t.Fatalf("newIntegratedGenerator returned %v", err)
			}
			if !ok {
				t.Skip("no SSPI logon context available on this runner")
			}
			defer gen.close()
			value, last, err := gen.next(nil)
			if err != nil {
				t.Fatalf("first next returned %v", err)
			}
			if last {
				t.Fatalf("first token should not be the last leg")
			}
			prefix := "Negotiate "
			if scheme == schemeNTLM {
				prefix = "NTLM "
			}
			if !strings.HasPrefix(value, prefix) {
				t.Fatalf("token = %q, want prefix %q", value, prefix)
			}
			token := decodeAuthorization(t, value)
			if scheme == schemeNTLM && messageType(token) != ntlmNegotiate {
				t.Fatalf("NTLM SSPI token is not a negotiate message: %x", token)
			}
		})
	}
}

func TestNewAuthGeneratorUsesSSPIWhenEmptyAuthIsSetAndNoCredentials(t *testing.T) {
	if gen, ok, _ := newIntegratedGenerator(schemeNTLM, "HTTP/localhost"); ok {
		gen.close()
	} else {
		t.Skip("no SSPI logon context available on this runner")
	}
	cfg := testGitConfig(t, "[http]\n\temptyAuth = true\n")
	session, err := newHTTPSession(Endpoint{Scheme: SchemeHTTP, Host: "example.com"}, nil, UploadPack, Options{Config: cfg, RemoteName: "origin"})
	if err != nil {
		t.Fatalf("newHTTPSession returned %v", err)
	}
	gen, err := session.newAuthGenerator(t.Context(), schemeNTLM)
	if err != nil {
		t.Fatalf("newAuthGenerator returned %v", err)
	}
	defer gen.close()
	if _, ok := gen.(*sspiGenerator); !ok {
		t.Fatalf("newAuthGenerator returned %T, want *sspiGenerator when emptyAuth is set", gen)
	}
}

func decodeAuthorization(t *testing.T, value string) []byte {
	t.Helper()
	_, raw, _ := strings.Cut(value, " ")
	token, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		t.Fatalf("token is not base64: %v", err)
	}
	return token
}
