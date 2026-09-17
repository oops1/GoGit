package transport

import (
	"crypto/hmac"
	"crypto/md5"
	"crypto/rand"
	"crypto/rc4"
	"encoding/binary"
	"errors"
	"strings"
	"time"
	"unicode/utf16"

	"golang.org/x/crypto/md4"
)

var ntlmSignature = []byte("NTLMSSP\x00")

const (
	ntlmNegotiate    uint32 = 1
	ntlmChallenge    uint32 = 2
	ntlmAuthenticate uint32 = 3
)

const (
	flagNegotiateUnicode        uint32 = 0x00000001
	flagRequestTarget           uint32 = 0x00000004
	flagNegotiateSign           uint32 = 0x00000010
	flagNegotiateNTLM           uint32 = 0x00000200
	flagNegotiateAlwaysSign     uint32 = 0x00008000
	flagNegotiateExtendedSecure uint32 = 0x00080000
	flagNegotiateTargetInfo     uint32 = 0x00800000
	flagNegotiateVersion        uint32 = 0x02000000
	flagNegotiate128            uint32 = 0x20000000
	flagNegotiateKeyExchange    uint32 = 0x40000000
	flagNegotiate56             uint32 = 0x80000000
)

const (
	avEOL          uint16 = 0x0000
	avTimestamp    uint16 = 0x0007
	avFlags        uint16 = 0x0006
	avChannelBind  uint16 = 0x000a
	avFlagsHasMIC  uint32 = 0x00000002
	micFieldOffset        = 72
)

var (
	errNTLMMessageTooShort = errors.New("transport: ntlm message is too short")
	errNTLMBadSignature    = errors.New("transport: ntlm message has a wrong signature")
	errNTLMWrongType       = errors.New("transport: ntlm message has an unexpected type")
	errNTLMFieldOutOfRange = errors.New("transport: ntlm message field points outside the buffer")
)

type ntlmCredentials struct {
	user     string
	domain   string
	password []byte
}

func splitNTLMUser(raw string) (domain, user string) {
	if before, after, ok := strings.Cut(raw, `\`); ok {
		return before, after
	}
	if before, after, ok := strings.Cut(raw, "@"); ok {
		return after, before
	}
	return "", raw
}

func newNTLMCredentials(username string, password []byte) ntlmCredentials {
	domain, user := splitNTLMUser(username)
	return ntlmCredentials{user: user, domain: domain, password: password}
}

func utf16le(s string) []byte {
	units := utf16.Encode([]rune(s))
	out := make([]byte, len(units)*2)
	for i, u := range units {
		binary.LittleEndian.PutUint16(out[i*2:], u)
	}
	return out
}

func md4sum(data []byte) []byte {
	h := md4.New()
	_, _ = h.Write(data)
	return h.Sum(nil)
}

func hmacMD5(key, data []byte) []byte {
	h := hmac.New(md5.New, key)
	_, _ = h.Write(data)
	return h.Sum(nil)
}

func ntowfV2(user, domain string, password []byte) []byte {
	return hmacMD5(md4sum(utf16le(string(password))), utf16le(strings.ToUpper(user)+domain))
}

func ntlmV2Temp(timestamp, clientChallenge, targetInfo []byte) []byte {
	temp := make([]byte, 0, 28+len(targetInfo)+4)
	temp = append(temp, 0x01, 0x01, 0, 0, 0, 0, 0, 0)
	temp = append(temp, timestamp...)
	temp = append(temp, clientChallenge...)
	temp = append(temp, 0, 0, 0, 0)
	temp = append(temp, targetInfo...)
	temp = append(temp, 0, 0, 0, 0)
	return temp
}

func ntlmV2Response(ntowf, serverChallenge, temp []byte) (response, sessionBaseKey []byte) {
	proof := hmacMD5(ntowf, append(append([]byte(nil), serverChallenge...), temp...))
	return append(append([]byte(nil), proof...), temp...), hmacMD5(ntowf, proof)
}

func lmV2Response(ntowf, serverChallenge, clientChallenge []byte) []byte {
	mac := hmacMD5(ntowf, append(append([]byte(nil), serverChallenge...), clientChallenge...))
	return append(mac, clientChallenge...)
}

func windowsTimestamp(t time.Time) []byte {
	const epochDelta = 116444736000000000
	ticks := uint64(t.UTC().UnixNano()/100) + epochDelta
	out := make([]byte, 8)
	binary.LittleEndian.PutUint64(out, ticks)
	return out
}

type avPairs struct {
	timestamp []byte
}

func parseAVPairs(info []byte) avPairs {
	var pairs avPairs
	for len(info) >= 4 {
		id := binary.LittleEndian.Uint16(info)
		length := int(binary.LittleEndian.Uint16(info[2:]))
		info = info[4:]
		if length > len(info) {
			break
		}
		value := info[:length]
		info = info[length:]
		if id == avEOL {
			break
		}
		if id == avTimestamp {
			pairs.timestamp = append([]byte(nil), value...)
		}
	}
	return pairs
}

func avPairsWithFlagsAndBinding(info, channelBinding []byte) []byte {
	out := make([]byte, 0, len(info)+24)
	for len(info) >= 4 {
		id := binary.LittleEndian.Uint16(info)
		length := int(binary.LittleEndian.Uint16(info[2:]))
		if 4+length > len(info) {
			break
		}
		if id == avEOL {
			break
		}
		out = append(out, info[:4+length]...)
		info = info[4+length:]
	}
	out = appendAVPair(out, avFlags, uint32ToLE(avFlagsHasMIC))
	if channelBinding != nil {
		out = appendAVPair(out, avChannelBind, channelBinding)
	}
	out = appendAVPair(out, avEOL, nil)
	return out
}

func appendAVPair(dst []byte, id uint16, value []byte) []byte {
	header := make([]byte, 4)
	binary.LittleEndian.PutUint16(header, id)
	binary.LittleEndian.PutUint16(header[2:], uint16(len(value)))
	dst = append(dst, header...)
	return append(dst, value...)
}

func uint32ToLE(v uint32) []byte {
	out := make([]byte, 4)
	binary.LittleEndian.PutUint32(out, v)
	return out
}

func negotiateFlags() uint32 {
	return flagNegotiateUnicode | flagRequestTarget | flagNegotiateSign | flagNegotiateNTLM |
		flagNegotiateAlwaysSign | flagNegotiateExtendedSecure | flagNegotiateTargetInfo |
		flagNegotiate128 | flagNegotiate56 | flagNegotiateKeyExchange
}

func buildNegotiateMessage() []byte {
	msg := make([]byte, 32)
	copy(msg, ntlmSignature)
	binary.LittleEndian.PutUint32(msg[8:], ntlmNegotiate)
	binary.LittleEndian.PutUint32(msg[12:], negotiateFlags()&^flagNegotiateVersion)
	return msg
}

type challengeMessage struct {
	flags           uint32
	serverChallenge []byte
	targetInfo      []byte
}

func parseChallengeMessage(msg []byte) (challengeMessage, error) {
	if len(msg) < 48 {
		return challengeMessage{}, errNTLMMessageTooShort
	}
	if string(msg[:8]) != string(ntlmSignature) {
		return challengeMessage{}, errNTLMBadSignature
	}
	if binary.LittleEndian.Uint32(msg[8:]) != ntlmChallenge {
		return challengeMessage{}, errNTLMWrongType
	}
	c := challengeMessage{
		flags:           binary.LittleEndian.Uint32(msg[20:]),
		serverChallenge: append([]byte(nil), msg[24:32]...),
	}
	infoLen := int(binary.LittleEndian.Uint16(msg[40:]))
	infoOff := int(binary.LittleEndian.Uint32(msg[44:]))
	if infoLen > 0 {
		if infoOff < 0 || infoOff+infoLen > len(msg) {
			return challengeMessage{}, errNTLMFieldOutOfRange
		}
		c.targetInfo = append([]byte(nil), msg[infoOff:infoOff+infoLen]...)
	}
	return c, nil
}

type authenticateInputs struct {
	creds           ntlmCredentials
	challenge       challengeMessage
	clientChallenge []byte
	timestamp       []byte
	channelBinding  []byte
	sessionKey      []byte
	workstation     string
}

func buildAuthenticateMessage(in authenticateInputs, negotiateMsg, challengeMsg []byte) []byte {
	ntowf := ntowfV2(in.creds.user, in.creds.domain, in.creds.password)
	pairs := parseAVPairs(in.challenge.targetInfo)
	timestamp := in.timestamp
	if len(pairs.timestamp) == 8 {
		timestamp = pairs.timestamp
	}
	targetInfo := avPairsWithFlagsAndBinding(in.challenge.targetInfo, in.channelBinding)
	temp := ntlmV2Temp(timestamp, in.clientChallenge, targetInfo)
	ntResponse, sessionBaseKey := ntlmV2Response(ntowf, in.challenge.serverChallenge, temp)
	lmResponse := lmV2Response(ntowf, in.challenge.serverChallenge, in.clientChallenge)

	flags := negotiateFlags() &^ flagNegotiateVersion
	keyExchangeKey := sessionBaseKey
	exportedKey := keyExchangeKey
	var encryptedKey []byte
	if in.challenge.flags&flagNegotiateKeyExchange != 0 {
		exportedKey = in.sessionKey
		encryptedKey = rc4Bytes(keyExchangeKey, exportedKey)
	} else {
		flags &^= flagNegotiateKeyExchange
	}

	msg := assembleAuthenticate(in.creds, in.workstation, lmResponse, ntResponse, encryptedKey, flags)
	mic := hmacMD5(exportedKey, concat(negotiateMsg, challengeMsg, msg))
	copy(msg[micFieldOffset:micFieldOffset+16], mic)
	return msg
}

func assembleAuthenticate(creds ntlmCredentials, workstation string, lmResponse, ntResponse, encryptedKey []byte, flags uint32) []byte {
	domain := utf16le(creds.domain)
	user := utf16le(creds.user)
	station := utf16le(workstation)
	const headerLen = micFieldOffset + 16
	payload := make([]byte, 0, len(lmResponse)+len(ntResponse)+len(domain)+len(user)+len(station)+len(encryptedKey))
	msg := make([]byte, headerLen)
	copy(msg, ntlmSignature)
	binary.LittleEndian.PutUint32(msg[8:], ntlmAuthenticate)

	place := func(fieldOff int, data []byte) {
		offset := headerLen + len(payload)
		binary.LittleEndian.PutUint16(msg[fieldOff:], uint16(len(data)))
		binary.LittleEndian.PutUint16(msg[fieldOff+2:], uint16(len(data)))
		binary.LittleEndian.PutUint32(msg[fieldOff+4:], uint32(offset))
		payload = append(payload, data...)
	}
	place(12, lmResponse)
	place(20, ntResponse)
	place(28, domain)
	place(36, user)
	place(44, station)
	place(52, encryptedKey)
	binary.LittleEndian.PutUint32(msg[60:], flags)
	return append(msg, payload...)
}

func concat(parts ...[]byte) []byte {
	var total int
	for _, p := range parts {
		total += len(p)
	}
	out := make([]byte, 0, total)
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

func rc4Bytes(key, data []byte) []byte {
	cipher, err := rc4.NewCipher(key)
	if err != nil {
		return nil
	}
	out := make([]byte, len(data))
	cipher.XORKeyStream(out, data)
	return out
}

func randomBytes(n int) []byte {
	buf := make([]byte, n)
	_, _ = rand.Read(buf)
	return buf
}
