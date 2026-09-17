//go:build windows

package transport

import (
	"encoding/base64"
	"encoding/binary"
	"strings"
	"testing"
)

func TestSSPIIntegratedGeneratorProducesTokens(t *testing.T) {
	for _, scheme := range []string{schemeNTLM, schemeNegotiate} {
		t.Run(scheme, func(t *testing.T) {
			gen, ok := newIntegratedGenerator(scheme, "localhost", []byte("tls-server-end-point:binding"))
			if !ok {
				t.Skip("no SSPI logon context available on this runner")
			}
			defer gen.close()
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

func TestSSPIGeneratorFailsOnAGarbageServerToken(t *testing.T) {
	gen, ok := newIntegratedGenerator(schemeNTLM, "localhost", nil)
	if !ok {
		t.Skip("no SSPI logon context available on this runner")
	}
	defer gen.close()
	if _, _, err := gen.next(nil); err != nil {
		t.Fatalf("first next returned %v", err)
	}
	if _, _, err := gen.next([]byte("garbage")); err == nil {
		t.Fatalf("SSPI accepted a garbage challenge")
	}
}

func TestNewIntegratedGeneratorRejectsUnusableInput(t *testing.T) {
	for _, tc := range []struct{ scheme, host string }{
		{schemeBasic, "localhost"},
		{schemeNTLM, ""},
		{schemeNTLM, "bad\x00host"},
	} {
		if _, ok := newIntegratedGenerator(tc.scheme, tc.host, nil); ok {
			t.Errorf("newIntegratedGenerator(%q, %q) succeeded", tc.scheme, tc.host)
		}
	}
}

func TestSSPIChannelBindingsLayout(t *testing.T) {
	if sspiChannelBindings(nil) != nil {
		t.Fatalf("no application data must give no bindings buffer")
	}
	data := []byte("tls-server-end-point:abc")
	b := sspiChannelBindings(data)
	if len(b) != sspiChannelBindingsSize+len(data) {
		t.Fatalf("bindings length = %d", len(b))
	}
	if binary.LittleEndian.Uint32(b[24:]) != uint32(len(data)) || binary.LittleEndian.Uint32(b[28:]) != sspiChannelBindingsSize {
		t.Fatalf("application data length/offset wrong: %x", b[:32])
	}
	if string(b[32:]) != string(data) {
		t.Fatalf("application data = %q", b[32:])
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
