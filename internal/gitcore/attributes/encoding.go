package attributes

import (
	"bytes"
	"encoding/binary"
	"errors"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"golang.org/x/text/encoding/ianaindex"
)

const (
	defaultEncoding = "UTF-8"
	utfPrefix       = "utf"
	latin1Alias     = "latin-1"
	latin1Name      = "ISO-8859-1"
	maxUnicode      = 0x10ffff
	lowSurrogate    = 0xdc00
	utf16LEBOMName  = "UTF-16LE-BOM"
	utf16BEBOMName  = "UTF-16BE-BOM"
)

var (
	ErrInvalidEncoding = errors.New("attributes: working-tree-encoding must name an encoding")
	ErrEncodingBOM     = errors.New("attributes: the byte order mark does not suit the working-tree-encoding")
	ErrEncodingFailed  = errors.New("attributes: the content cannot be converted by the working-tree-encoding")
)

var (
	utf16BEBOM = []byte{0xfe, 0xff}
	utf16LEBOM = []byte{0xff, 0xfe}
	utf32BEBOM = []byte{0, 0, 0xfe, 0xff}
	utf32LEBOM = []byte{0xff, 0xfe, 0, 0}
)

func utfSuffix(name string) (string, bool) {
	if len(name) < len(utfPrefix) || !strings.EqualFold(name[:len(utfPrefix)], utfPrefix) {
		return "", false
	}
	return strings.TrimPrefix(name[len(utfPrefix):], "-"), true
}

func sameUTFEncoding(a, b string) bool {
	suffixA, okA := utfSuffix(a)
	suffixB, okB := utfSuffix(b)
	return okA && okB && strings.EqualFold(suffixA, suffixB)
}

func encodingName(v Value) (string, error) {
	switch {
	case v.kind == Set || v.kind == Unset:
		return "", ErrInvalidEncoding
	case v.kind != Valued || v.text == "":
		return "", nil
	case sameUTFEncoding(v.text, defaultEncoding) || strings.EqualFold(v.text, defaultEncoding):
		return "", nil
	}
	return v.text, nil
}

func hasBOM(data []byte, boms ...[]byte) bool {
	for _, bom := range boms {
		if bytes.HasPrefix(data, bom) {
			return true
		}
	}
	return false
}

func validateEncoding(name string, data []byte) error {
	if _, ok := utfSuffix(name); !ok {
		return nil
	}
	sixteen := sameUTFEncoding("UTF-16BE", name) || sameUTFEncoding("UTF-16LE", name)
	thirtyTwo := sameUTFEncoding("UTF-32BE", name) || sameUTFEncoding("UTF-32LE", name)
	if sixteen && hasBOM(data, utf16BEBOM, utf16LEBOM) || thirtyTwo && hasBOM(data, utf32BEBOM, utf32LEBOM) {
		return ErrEncodingBOM
	}
	if sameUTFEncoding(name, "UTF-16") && !hasBOM(data, utf16BEBOM, utf16LEBOM) ||
		sameUTFEncoding(name, "UTF-32") && !hasBOM(data, utf32BEBOM, utf32LEBOM) {
		return ErrEncodingBOM
	}
	return nil
}

type byteOrder interface {
	binary.ByteOrder
	binary.AppendByteOrder
}

type utfCodec struct {
	width int
	order byteOrder
	bom   []byte
}

func utfCodecOf(name string, reading bool) (utfCodec, bool) {
	switch {
	case sameUTFEncoding(name, utf16LEBOMName) && reading:
		return utfCodec{width: 2, order: binary.BigEndian, bom: utf16BEBOM}, true
	case sameUTFEncoding(name, utf16LEBOMName):
		return utfCodec{width: 2, order: binary.LittleEndian, bom: utf16LEBOM}, true
	case sameUTFEncoding(name, utf16BEBOMName) && !reading:
		return utfCodec{width: 2, order: binary.BigEndian, bom: utf16BEBOM}, true
	}
	switch strings.ToUpper(name) {
	case "UTF-16":
		return utfCodec{width: 2, order: binary.BigEndian, bom: utf16BEBOM}, true
	case "UTF-16BE":
		return utfCodec{width: 2, order: binary.BigEndian}, true
	case "UTF-16LE":
		return utfCodec{width: 2, order: binary.LittleEndian}, true
	case "UTF-32":
		return utfCodec{width: 4, order: binary.BigEndian, bom: utf32BEBOM}, true
	case "UTF-32BE":
		return utfCodec{width: 4, order: binary.BigEndian}, true
	case "UTF-32LE":
		return utfCodec{width: 4, order: binary.LittleEndian}, true
	}
	return utfCodec{}, false
}

func (c utfCodec) unit(data []byte) rune {
	if c.width == 2 {
		return rune(c.order.Uint16(data))
	}
	return rune(c.order.Uint32(data))
}

func (c utfCodec) decode(data []byte) ([]byte, bool) {
	if c.bom != nil {
		c, data = c.followBOM(data)
	}
	if len(data)%c.width != 0 {
		return nil, false
	}
	out := make([]byte, 0, len(data))
	for len(data) > 0 {
		r := c.unit(data)
		data = data[c.width:]
		switch {
		case c.width == 2 && utf16.IsSurrogate(r):
			if r >= lowSurrogate || len(data) == 0 {
				return nil, false
			}
			low := c.unit(data)
			if low < lowSurrogate || !utf16.IsSurrogate(low) {
				return nil, false
			}
			r = utf16.DecodeRune(r, low)
			data = data[c.width:]
		case r < 0 || r > maxUnicode || utf16.IsSurrogate(r):
			return nil, false
		}
		out = utf8.AppendRune(out, r)
	}
	return out, true
}

func (c utfCodec) followBOM(data []byte) (utfCodec, []byte) {
	little, big := utf16LEBOM, utf16BEBOM
	if c.width == 4 {
		little, big = utf32LEBOM, utf32BEBOM
	}
	c.bom = nil
	switch {
	case bytes.HasPrefix(data, big):
		c.order = binary.BigEndian
		return c, data[len(big):]
	case bytes.HasPrefix(data, little):
		c.order = binary.LittleEndian
		return c, data[len(little):]
	}
	return c, data
}

func (c utfCodec) encode(data []byte) []byte {
	out := append(make([]byte, 0, len(c.bom)+len(data)*c.width), c.bom...)
	for _, r := range string(data) {
		if c.width == 4 {
			out = c.order.AppendUint32(out, uint32(r))
			continue
		}
		if r >= 0x10000 {
			high, low := utf16.EncodeRune(r)
			out = c.order.AppendUint16(out, uint16(high))
			out = c.order.AppendUint16(out, uint16(low))
			continue
		}
		out = c.order.AppendUint16(out, uint16(r))
	}
	return out
}

func ianaEncodingName(name string) string {
	if strings.EqualFold(name, latin1Alias) {
		return latin1Name
	}
	return name
}

func decodeToUTF8(name string, data []byte) ([]byte, bool) {
	if codec, ok := utfCodecOf(name, true); ok {
		return codec.decode(data)
	}
	enc, err := ianaindex.IANA.Encoding(ianaEncodingName(name))
	if err != nil || enc == nil {
		return nil, false
	}
	decoded, decodeErr := enc.NewDecoder().Bytes(data)
	back, encodeErr := enc.NewEncoder().Bytes(decoded)
	if decodeErr != nil || encodeErr != nil || !bytes.Equal(back, data) {
		return nil, false
	}
	return decoded, true
}

func encodeFromUTF8(name string, data []byte) ([]byte, bool) {
	if !utf8.Valid(data) {
		return nil, false
	}
	if codec, ok := utfCodecOf(name, false); ok {
		return codec.encode(data), true
	}
	enc, err := ianaindex.IANA.Encoding(ianaEncodingName(name))
	if err != nil || enc == nil {
		return nil, false
	}
	encoded, err := enc.NewEncoder().Bytes(data)
	if err != nil {
		return nil, false
	}
	return encoded, true
}
