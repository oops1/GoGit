package transport

import (
	"bytes"
	"errors"
)

var (
	spnegoOID = []byte{0x06, 0x06, 0x2b, 0x06, 0x01, 0x05, 0x05, 0x02}
	ntlmOID   = []byte{0x06, 0x0a, 0x2b, 0x06, 0x01, 0x04, 0x01, 0x82, 0x37, 0x02, 0x02, 0x0a}

	errSPNEGONoMechToken = errors.New("transport: spnego token carries no ntlm mech token")
	errSPNEGOTruncated   = errors.New("transport: spnego token is truncated")
)

func derLength(n int) []byte {
	if n < 0x80 {
		return []byte{byte(n)}
	}
	var body []byte
	for n > 0 {
		body = append([]byte{byte(n & 0xff)}, body...)
		n >>= 8
	}
	return append([]byte{byte(0x80 | len(body))}, body...)
}

func derTLV(tag byte, content []byte) []byte {
	out := append([]byte{tag}, derLength(len(content))...)
	return append(out, content...)
}

func spnegoNegTokenInit(ntlmNegotiate []byte) []byte {
	mechList := derTLV(0x30, ntlmOID)
	mechTypes := derTLV(0xa0, mechList)
	mechToken := derTLV(0xa2, derTLV(0x04, ntlmNegotiate))
	negInit := derTLV(0x30, append(mechTypes, mechToken...))
	inner := derTLV(0xa0, negInit)
	body := append(append([]byte(nil), spnegoOID...), inner...)
	return derTLV(0x60, body)
}

func spnegoNegTokenResp(ntlmToken []byte) []byte {
	responseToken := derTLV(0xa2, derTLV(0x04, ntlmToken))
	negResp := derTLV(0x30, responseToken)
	return derTLV(0xa1, negResp)
}

func derRead(data []byte) (tag byte, content, rest []byte, err error) {
	if len(data) < 2 {
		return 0, nil, nil, errSPNEGOTruncated
	}
	tag = data[0]
	length := int(data[1])
	offset := 2
	if data[1]&0x80 != 0 {
		count := int(data[1] & 0x7f)
		if count == 0 || count > 4 || len(data) < 2+count {
			return 0, nil, nil, errSPNEGOTruncated
		}
		length = 0
		for i := 0; i < count; i++ {
			length = length<<8 | int(data[2+i])
		}
		offset = 2 + count
	}
	if length < 0 || offset+length > len(data) {
		return 0, nil, nil, errSPNEGOTruncated
	}
	return tag, data[offset : offset+length], data[offset+length:], nil
}

func spnegoExtractNTLM(token []byte) ([]byte, error) {
	found := scanForNTLM(token)
	if found == nil {
		return nil, errSPNEGONoMechToken
	}
	return found, nil
}

func scanForNTLM(data []byte) []byte {
	for len(data) >= 2 {
		tag, content, rest, err := derRead(data)
		if err != nil {
			return nil
		}
		if tag == 0x04 && bytes.HasPrefix(content, ntlmSignature) {
			return append([]byte(nil), content...)
		}
		if tag&0x20 != 0 {
			if inner := scanForNTLM(content); inner != nil {
				return inner
			}
		}
		data = rest
	}
	return nil
}
