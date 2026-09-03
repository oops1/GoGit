package pack

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
)

type Pack struct {
	source   io.ReaderAt
	closer   io.Closer
	path     string
	size     int64
	version  uint32
	count    int
	trailer  hash.ObjectID
	settings settings
}

func OpenPack(path string, opts ...Option) (*Pack, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("pack: open %s: %w", path, err)
	}
	packfile, err := NewPackFile(file, opts...)
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("pack: %s: %w", path, err)
	}
	return packfile, nil
}

func NewPackFile(file *os.File, opts ...Option) (*Pack, error) {
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	packfile, err := NewPack(file, info.Size(), opts...)
	if err != nil {
		return nil, err
	}
	packfile.closer = file
	packfile.path = file.Name()
	return packfile, nil
}

func NewPack(source io.ReaderAt, size int64, opts ...Option) (*Pack, error) {
	if size < headerSize+hash.Size {
		return nil, fmt.Errorf("%w: packfile holds %d bytes", ErrTruncated, size)
	}
	var head [headerSize]byte
	if err := readFull(source, head[:], 0); err != nil {
		return nil, err
	}
	if !bytes.Equal(head[:len(packMagic)], packMagic) {
		return nil, fmt.Errorf("%w: %q", ErrBadMagic, head[:len(packMagic)])
	}
	version := binary.BigEndian.Uint32(head[4:8])
	if version != packVersion {
		return nil, fmt.Errorf("%w: version %d", ErrUnsupportedVersion, version)
	}
	packfile := &Pack{
		source:   source,
		size:     size,
		version:  version,
		count:    int(binary.BigEndian.Uint32(head[8:12])),
		settings: newSettings(opts),
	}
	var trailer [hash.Size]byte
	if err := readFull(source, trailer[:], size-hash.Size); err != nil {
		return nil, err
	}
	packfile.trailer = hash.ObjectID(trailer)
	return packfile, nil
}

func (p *Pack) Path() string {
	return p.path
}

func (p *Pack) Size() int64 {
	return p.size
}

func (p *Pack) Count() int {
	return p.count
}

func (p *Pack) Version() uint32 {
	return p.version
}

func (p *Pack) Checksum() hash.ObjectID {
	return p.trailer
}

func (p *Pack) Close() error {
	if p.closer == nil {
		return nil
	}
	return p.closer.Close()
}

func (p *Pack) Verify() error {
	return verifyChecksum(p.source, p.size, p.trailer, p.path)
}

func (p *Pack) HeaderAt(offset int64) (ObjectHeader, error) {
	if offset < headerSize || offset >= p.size-hash.Size {
		return ObjectHeader{}, fmt.Errorf("%w: %d leaves the packfile of %d bytes", ErrBadOffset, offset, p.size)
	}
	var raw [maxObjectHeaderSize]byte
	read, err := p.source.ReadAt(raw[:], offset)
	if read == 0 {
		return ObjectHeader{}, fmt.Errorf("pack: read object header at %d: %w", offset, err)
	}
	head, _, err := readObjectHeader(bytes.NewReader(raw[:read]), offset)
	if err != nil {
		return ObjectHeader{}, err
	}
	return head, nil
}

func (p *Pack) ObjectAt(offset int64) (object.Type, []byte, error) {
	return p.objectAt(offset, 0)
}

func (p *Pack) objectAt(offset int64, depth int) (object.Type, []byte, error) {
	if depth > p.settings.maxDepth {
		return 0, nil, fmt.Errorf("%w: %d links reached at %d", ErrDeltaChainTooDeep, depth, offset)
	}
	key := cacheKey{pack: p.trailer, offset: offset}
	if kind, data, ok := p.settings.cache.get(key); ok {
		return kind, data, nil
	}
	head, err := p.HeaderAt(offset)
	if err != nil {
		return 0, nil, err
	}
	kind, data, err := p.contentOf(head, depth)
	if err != nil {
		return 0, nil, err
	}
	p.settings.cache.put(key, kind, data)
	return kind, data, nil
}

func (p *Pack) contentOf(head ObjectHeader, depth int) (object.Type, []byte, error) {
	if head.Kind.IsDelta() {
		return p.applyDeltaAt(head, depth)
	}
	data, err := p.rawAt(head)
	if err != nil {
		return 0, nil, err
	}
	return head.Kind.Type(), data, nil
}

func (p *Pack) rawAt(head ObjectHeader) ([]byte, error) {
	if err := p.checkSize(head); err != nil {
		return nil, err
	}
	data := make([]byte, head.Size)
	if err := inflateExact(p.dataReader(head), data); err != nil {
		return nil, fmt.Errorf("pack: object at %d: %w", head.Offset, err)
	}
	return data, nil
}

func (p *Pack) applyDeltaAt(head ObjectHeader, depth int) (object.Type, []byte, error) {
	kind, base, err := p.baseOf(head, depth+1)
	if err != nil {
		return 0, nil, err
	}
	if err := p.checkSize(head); err != nil {
		return 0, nil, err
	}
	buffer := acquirePayload(head.Size)
	defer releasePayload(buffer)
	if err := inflateExact(p.dataReader(head), buffer.data); err != nil {
		return 0, nil, fmt.Errorf("pack: delta at %d: %w", head.Offset, err)
	}
	data, err := ApplyDelta(base, buffer.data)
	if err != nil {
		return 0, nil, fmt.Errorf("pack: delta at %d: %w", head.Offset, err)
	}
	return kind, data, nil
}

func (p *Pack) baseOf(head ObjectHeader, depth int) (object.Type, []byte, error) {
	if head.Kind == KindOffsetDelta {
		return p.objectAt(head.BaseOffset, depth)
	}
	if index := p.settings.index; index != nil {
		offset, ok, err := index.Lookup(head.BaseID)
		if err != nil {
			return 0, nil, err
		}
		if ok {
			return p.objectAt(offset, depth)
		}
	}
	if p.settings.bases != nil {
		return p.settings.bases.ResolveBase(head.BaseID, depth)
	}
	return 0, nil, fmt.Errorf("%w: %s for the delta at %d", ErrBaseNotFound, head.BaseID, head.Offset)
}

func (p *Pack) checkSize(head ObjectHeader) error {
	room := p.size - hash.Size - head.DataOffset
	if room < 0 {
		return fmt.Errorf("%w: object data at %d starts past the trailer", ErrTruncated, head.DataOffset)
	}
	if head.Size/maxInflateRatio > room {
		return fmt.Errorf("%w: %d bytes at %d", ErrObjectTooLarge, head.Size, head.Offset)
	}
	return nil
}

func (p *Pack) dataReader(head ObjectHeader) io.Reader {
	return io.NewSectionReader(p.source, head.DataOffset, p.size-hash.Size-head.DataOffset)
}
