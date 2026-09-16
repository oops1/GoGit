package credential

import (
	"crypto/subtle"
	"encoding/binary"
	"unicode/utf16"
	"unicode/utf8"
)

func utf16LEFromUTF8(text []byte) []byte {
	out := make([]byte, 0, 2*len(text))
	var units [2]uint16
	for i := 0; i < len(text); {
		r, size := utf8.DecodeRune(text[i:])
		i += size
		for _, u := range utf16.AppendRune(units[:0], r) {
			out = binary.LittleEndian.AppendUint16(out, u)
		}
	}
	return out
}

func utf8FromUTF16LE(blob []byte) []byte {
	units := make([]uint16, len(blob)/2)
	for i := range units {
		units[i] = binary.LittleEndian.Uint16(blob[2*i:])
	}
	runes := utf16.Decode(units)
	out := make([]byte, 0, utf8.UTFMax*len(runes))
	for _, r := range runes {
		out = utf8.AppendRune(out, r)
	}
	clear(units)
	clear(runes)
	return out
}

func secretsEqual(a, b []byte) bool {
	return subtle.ConstantTimeCompare(a, b) == 1
}
