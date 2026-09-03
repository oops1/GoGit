package hash

import (
	"crypto/sha1"
	"errors"
	"fmt"
	stdhash "hash"
	"strconv"
)

type Format uint8

const (
	SHA1 Format = iota + 1
	SHA256
)

var (
	ErrUnsupportedFormat = errors.New("hash: unsupported object format")
	ErrNegativeSize      = errors.New("hash: negative object size")
	ErrSizeMismatch      = errors.New("hash: hashed size differs from declared size")
)

func (f Format) String() string {
	switch f {
	case SHA1:
		return "sha1"
	case SHA256:
		return "sha256"
	default:
		return "unknown"
	}
}

func (f Format) Size() int {
	switch f {
	case SHA1:
		return Size
	case SHA256:
		return 32
	default:
		return 0
	}
}

func (f Format) HexSize() int {
	return f.Size() * 2
}

func (f Format) Supported() bool {
	return f == SHA1
}

func ParseFormat(name string) (Format, error) {
	switch name {
	case "sha1":
		return SHA1, nil
	case "sha256":
		return SHA256, nil
	default:
		return 0, fmt.Errorf("%w: %q", ErrUnsupportedFormat, name)
	}
}

func Header(objectType string, size int64) []byte {
	head := make([]byte, 0, len(objectType)+22)
	head = append(head, objectType...)
	head = append(head, ' ')
	head = strconv.AppendInt(head, size, 10)
	return append(head, 0)
}

func SumSHA1(objectType string, data []byte) ObjectID {
	digest := sha1.New()
	_, _ = digest.Write(Header(objectType, int64(len(data))))
	_, _ = digest.Write(data)
	return finish(digest)
}

func Sum(format Format, objectType string, data []byte) (ObjectID, error) {
	if !format.Supported() {
		return Zero, fmt.Errorf("%w: %s", ErrUnsupportedFormat, format)
	}
	return SumSHA1(objectType, data), nil
}

type Hasher struct {
	digest  stdhash.Hash
	size    int64
	written int64
}

func NewHasher(format Format, objectType string, size int64) (*Hasher, error) {
	if !format.Supported() {
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedFormat, format)
	}
	if size < 0 {
		return nil, fmt.Errorf("%w: %d", ErrNegativeSize, size)
	}
	hasher := new(Hasher{digest: sha1.New(), size: size})
	_, _ = hasher.digest.Write(Header(objectType, size))
	return hasher, nil
}

func (h *Hasher) Write(chunk []byte) (int, error) {
	if h.written+int64(len(chunk)) > h.size {
		return 0, fmt.Errorf("%w: %d bytes declared, %d already hashed, %d more offered",
			ErrSizeMismatch, h.size, h.written, len(chunk))
	}
	written, _ := h.digest.Write(chunk)
	h.written += int64(written)
	return written, nil
}

func (h *Hasher) Written() int64 {
	return h.written
}

func (h *Hasher) Sum() (ObjectID, error) {
	if h.written != h.size {
		return Zero, fmt.Errorf("%w: %d bytes declared, %d hashed", ErrSizeMismatch, h.size, h.written)
	}
	return finish(h.digest), nil
}

func finish(digest stdhash.Hash) ObjectID {
	var id ObjectID
	copy(id[:], digest.Sum(nil))
	return id
}
