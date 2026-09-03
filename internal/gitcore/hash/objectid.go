package hash

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
)

const (
	Size    = 20
	HexSize = Size * 2
)

type ObjectID [Size]byte

var Zero ObjectID

var (
	ErrInvalidLength = errors.New("hash: invalid object id length")
	ErrInvalidHex    = errors.New("hash: object id is not hexadecimal")
)

func (id ObjectID) String() string {
	var text [HexSize]byte
	hex.Encode(text[:], id[:])
	return string(text[:])
}

func (id ObjectID) IsZero() bool {
	return id == Zero
}

func (id ObjectID) Bytes() []byte {
	return bytes.Clone(id[:])
}

func (id ObjectID) Compare(other ObjectID) int {
	return bytes.Compare(id[:], other[:])
}

func (id ObjectID) MarshalText() ([]byte, error) {
	text := make([]byte, HexSize)
	hex.Encode(text, id[:])
	return text, nil
}

func (id *ObjectID) UnmarshalText(text []byte) error {
	parsed, err := FromHex(text)
	if err != nil {
		return err
	}
	*id = parsed
	return nil
}

func Parse(text string) (ObjectID, error) {
	return FromHex([]byte(text))
}

func FromHex(text []byte) (ObjectID, error) {
	var id ObjectID
	if len(text) != HexSize {
		return Zero, fmt.Errorf("%w: %d instead of %d", ErrInvalidLength, len(text), HexSize)
	}
	if _, err := hex.Decode(id[:], text); err != nil {
		return Zero, fmt.Errorf("%w: %w", ErrInvalidHex, err)
	}
	return id, nil
}

func FromBytes(raw []byte) (ObjectID, error) {
	var id ObjectID
	if len(raw) != Size {
		return Zero, fmt.Errorf("%w: %d instead of %d", ErrInvalidLength, len(raw), Size)
	}
	copy(id[:], raw)
	return id, nil
}
