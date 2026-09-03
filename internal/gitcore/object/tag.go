package object

import (
	"bytes"
	"io"
	"slices"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/hash"
)

var signatureBanners = []string{
	"-----BEGIN PGP SIGNATURE-----",
	"-----BEGIN PGP MESSAGE-----",
	"-----BEGIN SIGNED MESSAGE-----",
	"-----BEGIN SSH SIGNATURE-----",
}

type Tag struct {
	Object     hash.ObjectID
	ObjectType Type
	Name       string
	Tagger     *Signature
	Extra      []ExtraHeader
	Message    string
}

func ParseTag(data []byte) (*Tag, error) {
	headers, message, err := splitHeaders(data)
	if err != nil {
		return nil, err
	}
	tag := new(Tag{Message: message})
	seen := make(headerSet)
	for _, header := range headers {
		switch header.Key {
		case headerObject, headerType, headerTag, headerTagger:
			if err := seen.add(header.Key); err != nil {
				return nil, err
			}
		}
		switch header.Key {
		case headerObject:
			tag.Object, err = parseHeaderID(header)
		case headerType:
			tag.ObjectType, err = ParseType(header.Value)
		case headerTag:
			tag.Name = header.Value
		case headerTagger:
			var tagger Signature
			if tagger, err = parseHeaderSignature(header); err == nil {
				tag.Tagger = new(tagger)
			}
		default:
			tag.Extra = append(tag.Extra, header)
		}
		if err != nil {
			return nil, err
		}
	}
	if err := seen.require(headerObject, headerType, headerTag); err != nil {
		return nil, err
	}
	return tag, nil
}

func (t *Tag) Type() Type {
	return TypeTag
}

func (t *Tag) Encode() []byte {
	var buf bytes.Buffer
	writeHeader(&buf, headerObject, t.Object.String())
	writeHeader(&buf, headerType, t.ObjectType.String())
	writeHeader(&buf, headerTag, t.Name)
	if t.Tagger != nil {
		writeHeader(&buf, headerTagger, t.Tagger.String())
	}
	for _, extra := range t.Extra {
		writeHeader(&buf, extra.Key, extra.Value)
	}
	buf.WriteByte('\n')
	buf.WriteString(t.Message)
	return buf.Bytes()
}

func (t *Tag) WriteTo(w io.Writer) (int64, error) {
	return writeAll(w, t.Encode())
}

func (t *Tag) ID() hash.ObjectID {
	return identify(t)
}

func (t *Tag) SplitMessage() (string, string) {
	for offset := 0; offset < len(t.Message); {
		line := t.Message[offset:]
		next := len(t.Message)
		if end := strings.IndexByte(line, '\n'); end >= 0 {
			line = line[:end]
			next = offset + end + 1
		}
		if slices.Contains(signatureBanners, line) {
			return t.Message[:offset], t.Message[offset:]
		}
		offset = next
	}
	return t.Message, ""
}
