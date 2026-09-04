package pack

import (
	"errors"
	"fmt"
	"io"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
)

const (
	headerSize  = 12
	packVersion = 2
	packSuffix  = ".pack"
	indexSuffix = ".idx"

	maxInflateRatio = 1032
	maxPrealloc     = 1 << 20
)

var packMagic = []byte{'P', 'A', 'C', 'K'}

const (
	kindShift    = 4
	kindMask     = 0x07
	sizeMask     = 0x0f
	sizeBits     = 4
	continuation = 0x80
	payloadMask  = 0x7f
	payloadBits  = 7

	maxObjectHeaderSize = 1 + 10 + hash.Size
	maxBaseOffsetStep   = (1<<63-1)>>payloadBits - 1
)

type Kind uint8

const (
	KindCommit      Kind = 1
	KindTree        Kind = 2
	KindBlob        Kind = 3
	KindTag         Kind = 4
	KindOffsetDelta Kind = 6
	KindRefDelta    Kind = 7
)

func (k Kind) IsDelta() bool {
	return k == KindOffsetDelta || k == KindRefDelta
}

func (k Kind) Type() object.Type {
	if k.IsDelta() {
		return 0
	}
	return object.Type(k)
}

func (k Kind) String() string {
	switch k {
	case KindOffsetDelta:
		return "ofs-delta"
	case KindRefDelta:
		return "ref-delta"
	default:
		return k.Type().String()
	}
}

type ObjectHeader struct {
	Offset     int64
	DataOffset int64
	Kind       Kind
	Size       int64
	BaseOffset int64
	BaseID     hash.ObjectID
}

func readObjectHeader(source io.ByteReader, offset int64) (ObjectHeader, int64, error) {
	head := ObjectHeader{Offset: offset}
	current, err := source.ReadByte()
	if err != nil {
		return head, 0, fmt.Errorf("%w: object header at %d: %v", ErrTruncated, offset, err)
	}
	read := int64(1)
	head.Kind = Kind((current >> kindShift) & kindMask)
	head.Size = int64(current & sizeMask)
	for shift := uint(sizeBits); current&continuation != 0; shift += payloadBits {
		if current, err = source.ReadByte(); err != nil {
			return head, read, fmt.Errorf("%w: object size at %d: %v", ErrTruncated, offset, err)
		}
		read++
		if shift >= 64 {
			return head, read, fmt.Errorf("%w: object size at %d does not fit in 64 bits", ErrBadObjectHeader, offset)
		}
		head.Size |= int64(current&payloadMask) << shift
	}
	if head.Size < 0 {
		return head, read, fmt.Errorf("%w: negative object size at %d", ErrBadObjectHeader, offset)
	}
	tail, err := readObjectBase(source, &head)
	read += tail
	if err != nil {
		return head, read, err
	}
	head.DataOffset = offset + read
	return head, read, nil
}

func readObjectBase(source io.ByteReader, head *ObjectHeader) (int64, error) {
	switch head.Kind {
	case KindCommit, KindTree, KindBlob, KindTag:
		return 0, nil
	case KindOffsetDelta:
		read, base, err := readBaseOffset(source, head.Offset)
		head.BaseOffset = base
		return read, err
	case KindRefDelta:
		read, id, err := readBaseName(source, head.Offset)
		head.BaseID = id
		return read, err
	default:
		return 0, fmt.Errorf("%w: kind %d at %d", ErrUnknownObjectKind, uint8(head.Kind), head.Offset)
	}
}

func readBaseOffset(source io.ByteReader, offset int64) (int64, int64, error) {
	current, err := source.ReadByte()
	if err != nil {
		return 0, 0, fmt.Errorf("%w: delta base offset at %d: %v", ErrTruncated, offset, err)
	}
	read := int64(1)
	distance := int64(current & payloadMask)
	for current&continuation != 0 {
		if current, err = source.ReadByte(); err != nil {
			return read, 0, fmt.Errorf("%w: delta base offset at %d: %w", ErrTruncated, offset, err)
		}
		read++
		if distance > maxBaseOffsetStep {
			return read, 0, fmt.Errorf("%w: delta base offset at %d does not fit in 64 bits", ErrBadObjectHeader, offset)
		}
		distance = ((distance + 1) << payloadBits) | int64(current&payloadMask)
	}
	base := offset - distance
	if distance == 0 || base < headerSize {
		return read, 0, fmt.Errorf("%w: delta at %d refers to %d", ErrBadOffset, offset, base)
	}
	return read, base, nil
}

func readBaseName(source io.ByteReader, offset int64) (int64, hash.ObjectID, error) {
	var id hash.ObjectID
	for i := range id {
		current, err := source.ReadByte()
		if err != nil {
			return int64(i), hash.Zero, fmt.Errorf("%w: delta base name at %d: %w", ErrTruncated, offset, err)
		}
		id[i] = current
	}
	return hash.Size, id, nil
}

func readFull(source io.ReaderAt, into []byte, offset int64) error {
	read, err := source.ReadAt(into, offset)
	switch {
	case read == len(into):
		return nil
	case errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF):
		return fmt.Errorf("%w: %d bytes at %d: %v", ErrTruncated, len(into), offset, err)
	default:
		return fmt.Errorf("pack: read %d bytes at %d: %w", len(into), offset, err)
	}
}
