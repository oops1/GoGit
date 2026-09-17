//go:build !windows

package transport

import (
	"testing"
	"time"
)

func TestIntegratedGeneratorUsesKerberosOnlyForNegotiate(t *testing.T) {
	f := newKerberosFixture(t, time.Hour)
	useKerberosFiles(t, writeKrb5Conf(t, refusingKDC(t)), f.writeCCache(t, true, time.Now().Add(time.Hour)))
	if _, ok := newIntegratedGenerator(schemeNTLM, "git.example.com", nil); ok {
		t.Fatalf("NTLM must not use integrated credentials off Windows")
	}
	if _, ok := newIntegratedGenerator(schemeNegotiate, "", nil); ok {
		t.Fatalf("an empty host must not produce a Kerberos token")
	}
	gen, ok := newIntegratedGenerator(schemeNegotiate, "git.example.com", nil)
	if !ok {
		t.Fatalf("Negotiate with a usable credential cache must produce a Kerberos generator")
	}
	gen.close()
}
