package object

import (
	"io"

	"github.com/oops1/gogit/internal/gitcore/hash"
)

type Blob struct {
	Data []byte
}

func ParseBlob(data []byte) (*Blob, error) {
	return &Blob{Data: data}, nil
}

func (b *Blob) Type() Type {
	return TypeBlob
}

func (b *Blob) Encode() []byte {
	return b.Data
}

func (b *Blob) WriteTo(w io.Writer) (int64, error) {
	return writeAll(w, b.Data)
}

func (b *Blob) ID() hash.ObjectID {
	return identify(b)
}
