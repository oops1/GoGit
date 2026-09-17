package transport

import (
	"bytes"
	"errors"
	"testing"
)

func TestDerLengthEncodesShortAndLongForms(t *testing.T) {
	cases := map[int]string{
		0:     "00",
		127:   "7f",
		128:   "8180",
		256:   "820100",
		65535: "82ffff",
	}
	for n, want := range cases {
		if got := hexOf(derLength(n)); got != want {
			t.Errorf("derLength(%d) = %s, want %s", n, got, want)
		}
	}
}

func TestSPNEGONegTokenInitCarriesNTLMNegotiate(t *testing.T) {
	neg := buildNegotiateMessage()
	token := spnegoNegTokenInit(neg)
	if token[0] != 0x60 {
		t.Fatalf("token[0] = %02x, want application tag 0x60", token[0])
	}
	if !bytes.Contains(token, spnegoOID) {
		t.Fatalf("token missing the SPNEGO OID")
	}
	if !bytes.Contains(token, ntlmOID) {
		t.Fatalf("token missing the NTLM OID")
	}
	extracted, err := spnegoExtractNTLM(token)
	if err != nil {
		t.Fatalf("spnegoExtractNTLM returned %v", err)
	}
	if !bytes.Equal(extracted, neg) {
		t.Fatalf("extracted = %x, want %x", extracted, neg)
	}
}

func TestSPNEGONegTokenRespRoundTrips(t *testing.T) {
	challenge := buildTestChallenge(t, flagNegotiateTargetInfo, mustHex(t, "00000000"))
	token := spnegoNegTokenResp(challenge)
	if token[0] != 0xa1 {
		t.Fatalf("token[0] = %02x, want context tag 0xa1", token[0])
	}
	extracted, err := spnegoExtractNTLM(token)
	if err != nil {
		t.Fatalf("spnegoExtractNTLM returned %v", err)
	}
	if !bytes.Equal(extracted, challenge) {
		t.Fatalf("extracted = %x, want %x", extracted, challenge)
	}
}

func TestSPNEGOExtractNTLMFailsWithoutMechToken(t *testing.T) {
	token := derTLV(0xa1, derTLV(0x30, derTLV(0xa0, derTLV(0x0a, []byte{0x01}))))
	if _, err := spnegoExtractNTLM(token); !errors.Is(err, errSPNEGONoMechToken) {
		t.Fatalf("err = %v, want %v", err, errSPNEGONoMechToken)
	}
}

func TestDerReadRejectsTruncatedInput(t *testing.T) {
	cases := [][]byte{
		{0x04},
		{0x04, 0x05, 0x01},
		{0x04, 0x84, 0x01},
		{0x04, 0x85, 0x01, 0x02, 0x03, 0x04, 0x05},
	}
	for i, c := range cases {
		if _, _, _, err := derRead(c); err == nil {
			t.Errorf("case %d: derRead succeeded on %x", i, c)
		}
	}
}

func hexOf(b []byte) string {
	const digits = "0123456789abcdef"
	out := make([]byte, 0, len(b)*2)
	for _, x := range b {
		out = append(out, digits[x>>4], digits[x&0xf])
	}
	return string(out)
}
