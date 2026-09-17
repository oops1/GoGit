package transport

import (
	"bytes"
	"encoding/binary"
	"testing"
	"time"
)

func TestAVPairsWithFlagsAndBindingAppendsChannelBinding(t *testing.T) {
	info := appendAVPair(nil, 0x0002, utf16le("CORP"))
	info = appendAVPair(info, avEOL, nil)
	binding := bytes.Repeat([]byte{0x11}, 16)
	out := avPairsWithFlagsAndBinding(info, binding)
	if !bytes.Contains(out, binding) {
		t.Fatalf("channel binding was not appended to the AV pairs")
	}
	flagsAV := appendAVPair(nil, avFlags, uint32ToLE(avFlagsHasMIC))
	if !bytes.Contains(out, flagsAV) {
		t.Fatalf("MsvAvFlags with the MIC bit was not appended")
	}
}

func TestAVPairsWithFlagsAndBindingStopsOnAMalformedPair(t *testing.T) {
	malformed := []byte{0x02, 0x00, 0x10, 0x00, 0x01}
	out := avPairsWithFlagsAndBinding(malformed, nil)
	if len(out) == 0 {
		t.Fatalf("expected at least the trailing flags and EOL pairs")
	}
}

func TestBuildAuthenticateMessageWithoutKeyExchangeOmitsTheSessionKey(t *testing.T) {
	targetInfo := appendAVPair(nil, avEOL, nil)
	challenge := challengeMessage{
		flags:           flagNegotiateTargetInfo | flagNegotiateUnicode,
		serverChallenge: nlmpServerChallenge,
		targetInfo:      targetInfo,
	}
	in := authenticateInputs{
		creds:           newNTLMCredentials("alice", []byte("pw")),
		challenge:       challenge,
		clientChallenge: nlmpClientChallenge,
		timestamp:       windowsTimestamp(time.Unix(1700000000, 0)),
		sessionKey:      randomBytes(16),
	}
	msg := buildAuthenticateMessage(in, buildNegotiateMessage(), buildTestChallenge(t, challenge.flags, targetInfo))
	if keyLen := binary.LittleEndian.Uint16(msg[52:]); keyLen != 0 {
		t.Fatalf("encrypted session key length = %d, want 0 without key exchange", keyLen)
	}
	flags := binary.LittleEndian.Uint32(msg[60:])
	if flags&flagNegotiateKeyExchange != 0 {
		t.Fatalf("key exchange flag should be cleared, flags = %08x", flags)
	}
}

func TestRC4BytesReturnsNilForAnInvalidKey(t *testing.T) {
	if out := rc4Bytes(nil, []byte("data")); out != nil {
		t.Fatalf("rc4Bytes with an empty key = %x, want nil", out)
	}
}

func TestScanForNTLMSkipsPrimitiveSiblings(t *testing.T) {
	octet := derTLV(0x04, append([]byte("NTLMSSP\x00"), 0x02))
	seq := append(derTLV(0x02, []byte{0x01}), octet...)
	token := derTLV(0x30, seq)
	got := scanForNTLM(token)
	if !bytes.HasPrefix(got, ntlmSignature) {
		t.Fatalf("scanForNTLM = %x, want the NTLM octet string", got)
	}
}
