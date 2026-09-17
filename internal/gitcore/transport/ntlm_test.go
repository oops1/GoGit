package transport

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"testing"
	"time"
)

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("decode %q: %v", s, err)
	}
	return b
}

const (
	nlmpUser     = "User"
	nlmpDomain   = "Domain"
	nlmpPassword = "Password"
)

var nlmpServerChallenge = []byte{0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd, 0xef}
var nlmpClientChallenge = []byte{0xaa, 0xaa, 0xaa, 0xaa, 0xaa, 0xaa, 0xaa, 0xaa}

func TestNTOWFv2MatchesMSNLMPVector(t *testing.T) {
	got := ntowfV2(nlmpUser, nlmpDomain, []byte(nlmpPassword))
	want := mustHex(t, "0c868a403bfd7a93a3001ef22ef02e3f")
	if !bytes.Equal(got, want) {
		t.Fatalf("NTOWFv2 = %x, want %x", got, want)
	}
}

func TestLMv2ResponseMatchesMSNLMPVector(t *testing.T) {
	ntowf := ntowfV2(nlmpUser, nlmpDomain, []byte(nlmpPassword))
	got := lmV2Response(ntowf, nlmpServerChallenge, nlmpClientChallenge)
	want := mustHex(t, "86c35097ac9cec102554764a57cccc19aaaaaaaaaaaaaaaa")
	if !bytes.Equal(got, want) {
		t.Fatalf("LMv2 = %x, want %x", got, want)
	}
}

func TestNTLMv2ProofAndSessionKeyAreReproducibleForMSNLMPTemp(t *testing.T) {
	ntowf := ntowfV2(nlmpUser, nlmpDomain, []byte(nlmpPassword))
	targetInfo := mustHex(t, "0002000c0044006f006d00610069006e0001000c00530065007200760065007200000000")
	temp := ntlmV2Temp(make([]byte, 8), nlmpClientChallenge, targetInfo)
	if got := hex.EncodeToString(temp); got != msnlmpTemp {
		t.Fatalf("temp = %s, want the MS-NLMP 4.2.4.1.3 temp %s", got, msnlmpTemp)
	}
	response, sessionKey := ntlmV2Response(ntowf, nlmpServerChallenge, temp)
	wantProof := mustHex(t, "7c39e215f0a5c7d967df28d6b5e99f10")
	if !bytes.Equal(response[:16], wantProof) {
		t.Fatalf("NTProofStr = %x, want %x", response[:16], wantProof)
	}
	if !bytes.Equal(response[16:], temp) {
		t.Fatalf("NTLMv2 response tail = %x, want the temp %x", response[16:], temp)
	}
	wantKey := mustHex(t, "576d18abe248a9131c02c0e87970db40")
	if !bytes.Equal(sessionKey, wantKey) {
		t.Fatalf("SessionBaseKey = %x, want %x", sessionKey, wantKey)
	}
}

const msnlmpTemp = "01010000000000000000000000000000aaaaaaaaaaaaaaaa000000000002000c0044006f006d00610069006e0001000c0053006500720076006500720000000000000000"

func TestSplitNTLMUser(t *testing.T) {
	cases := []struct {
		raw          string
		domain, user string
	}{
		{`CORP\alice`, "CORP", "alice"},
		{"bob@corp.example", "corp.example", "bob"},
		{"carol", "", "carol"},
	}
	for _, c := range cases {
		domain, user := splitNTLMUser(c.raw)
		if domain != c.domain || user != c.user {
			t.Errorf("splitNTLMUser(%q) = (%q, %q), want (%q, %q)", c.raw, domain, user, c.domain, c.user)
		}
	}
}

func TestBuildNegotiateMessageHasSignatureAndType(t *testing.T) {
	msg := buildNegotiateMessage()
	if !bytes.Equal(msg[:8], ntlmSignature) {
		t.Fatalf("signature = %x", msg[:8])
	}
	if binary.LittleEndian.Uint32(msg[8:]) != ntlmNegotiate {
		t.Fatalf("type = %d, want %d", binary.LittleEndian.Uint32(msg[8:]), ntlmNegotiate)
	}
	if flags := binary.LittleEndian.Uint32(msg[12:]); flags&flagNegotiateVersion != 0 {
		t.Fatalf("negotiate should not set the version flag, flags = %08x", flags)
	}
}

func buildTestChallenge(t *testing.T, flags uint32, targetInfo []byte) []byte {
	t.Helper()
	msg := make([]byte, 48)
	copy(msg, ntlmSignature)
	binary.LittleEndian.PutUint32(msg[8:], ntlmChallenge)
	binary.LittleEndian.PutUint32(msg[20:], flags)
	copy(msg[24:32], nlmpServerChallenge)
	binary.LittleEndian.PutUint16(msg[40:], uint16(len(targetInfo)))
	binary.LittleEndian.PutUint16(msg[42:], uint16(len(targetInfo)))
	binary.LittleEndian.PutUint32(msg[44:], uint32(len(msg)))
	return append(msg, targetInfo...)
}

func TestParseChallengeMessageReadsServerChallengeAndTargetInfo(t *testing.T) {
	targetInfo := mustHex(t, "0002000c0044006f006d00610069006e00000000")
	raw := buildTestChallenge(t, flagNegotiateTargetInfo|flagNegotiateUnicode, targetInfo)
	c, err := parseChallengeMessage(raw)
	if err != nil {
		t.Fatalf("parseChallengeMessage returned %v", err)
	}
	if !bytes.Equal(c.serverChallenge, nlmpServerChallenge) {
		t.Fatalf("serverChallenge = %x", c.serverChallenge)
	}
	if !bytes.Equal(c.targetInfo, targetInfo) {
		t.Fatalf("targetInfo = %x, want %x", c.targetInfo, targetInfo)
	}
}

func TestParseChallengeMessageRejectsBadInput(t *testing.T) {
	cases := map[string][]byte{
		"short":         make([]byte, 10),
		"bad signature": append([]byte("BADSIG\x00\x00"), make([]byte, 40)...),
	}
	for name, msg := range cases {
		if _, err := parseChallengeMessage(msg); err == nil {
			t.Errorf("%s: parseChallengeMessage succeeded", name)
		}
	}
	wrong := make([]byte, 48)
	copy(wrong, ntlmSignature)
	binary.LittleEndian.PutUint32(wrong[8:], ntlmNegotiate)
	if _, err := parseChallengeMessage(wrong); !errors.Is(err, errNTLMWrongType) {
		t.Fatalf("wrong type error = %v, want %v", err, errNTLMWrongType)
	}
}

func TestParseChallengeMessageRejectsOutOfRangeTargetInfo(t *testing.T) {
	msg := make([]byte, 48)
	copy(msg, ntlmSignature)
	binary.LittleEndian.PutUint32(msg[8:], ntlmChallenge)
	binary.LittleEndian.PutUint16(msg[40:], 100)
	binary.LittleEndian.PutUint32(msg[44:], 40)
	if _, err := parseChallengeMessage(msg); !errors.Is(err, errNTLMFieldOutOfRange) {
		t.Fatalf("error = %v, want %v", err, errNTLMFieldOutOfRange)
	}
}

func TestBuildAuthenticateMessageIsParseableAndCarriesMIC(t *testing.T) {
	targetInfo := mustHex(t, "0002000c0044006f006d00610069006e0007000800"+hexTimestamp(t)+"00000000")
	challenge := challengeMessage{
		flags:           flagNegotiateTargetInfo | flagNegotiateUnicode | flagNegotiateKeyExchange,
		serverChallenge: nlmpServerChallenge,
		targetInfo:      targetInfo,
	}
	in := authenticateInputs{
		creds:           newNTLMCredentials(`CORP\alice`, []byte("hunter2")),
		challenge:       challenge,
		clientChallenge: nlmpClientChallenge,
		timestamp:       windowsTimestamp(time.Unix(1700000000, 0)),
		sessionKey:      randomBytes(16),
		workstation:     "WORKSTATION",
	}
	neg := buildNegotiateMessage()
	msg := buildAuthenticateMessage(in, neg, buildTestChallenge(t, challenge.flags, targetInfo))
	if !bytes.Equal(msg[:8], ntlmSignature) {
		t.Fatalf("signature = %x", msg[:8])
	}
	if binary.LittleEndian.Uint32(msg[8:]) != ntlmAuthenticate {
		t.Fatalf("type = %d", binary.LittleEndian.Uint32(msg[8:]))
	}
	mic := msg[micFieldOffset : micFieldOffset+16]
	if bytes.Equal(mic, make([]byte, 16)) {
		t.Fatalf("MIC field is all zero, want a computed MIC")
	}
	ntOff := binary.LittleEndian.Uint32(msg[24:])
	ntLen := binary.LittleEndian.Uint16(msg[20:])
	if int(ntOff)+int(ntLen) > len(msg) {
		t.Fatalf("nt response field %d+%d exceeds message length %d", ntOff, ntLen, len(msg))
	}
}

func hexTimestamp(t *testing.T) string {
	t.Helper()
	return hex.EncodeToString(windowsTimestamp(time.Unix(1700000000, 0)))
}
