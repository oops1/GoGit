package pack

import (
	"bufio"
	"bytes"
	"crypto/sha1"
	"encoding/binary"
	"fmt"
	stdhash "hash"
	"hash/crc32"
	"io"

	"github.com/oops1/gogit/internal/gitcore/hash"
)

type ObjectEntry struct {
	Header         ObjectHeader
	Data           []byte
	CRC32          uint32
	CompressedSize int64
}

type Reader struct {
	source  *packCounter
	version uint32
	count   int
	read    int
	trailer hash.ObjectID
	done    bool
}

func NewReader(source io.Reader) (*Reader, error) {
	counter := newPackCounter(source)
	var head [headerSize]byte
	if _, err := io.ReadFull(counter, head[:]); err != nil {
		return nil, fmt.Errorf("%w: packfile header: %v", ErrTruncated, err)
	}
	if !bytes.Equal(head[:len(packMagic)], packMagic) {
		return nil, fmt.Errorf("%w: %q", ErrBadMagic, head[:len(packMagic)])
	}
	version := binary.BigEndian.Uint32(head[4:8])
	if version != packVersion {
		return nil, fmt.Errorf("%w: version %d", ErrUnsupportedVersion, version)
	}
	return &Reader{
		source:  counter,
		version: version,
		count:   int(binary.BigEndian.Uint32(head[8:12])),
	}, nil
}

func (r *Reader) Version() uint32 {
	return r.version
}

func (r *Reader) Count() int {
	return r.count
}

func (r *Reader) Offset() int64 {
	return r.source.position
}

func (r *Reader) Trailer() hash.ObjectID {
	return r.trailer
}

func (r *Reader) NextObject() (ObjectEntry, error) {
	if r.done {
		return ObjectEntry{}, io.EOF
	}
	if r.read == r.count {
		if err := r.finish(); err != nil {
			return ObjectEntry{}, err
		}
		return ObjectEntry{}, io.EOF
	}
	offset := r.source.position
	r.source.startSum()
	head, _, err := readObjectHeader(r.source, offset)
	if err != nil {
		return ObjectEntry{}, err
	}
	var data bytes.Buffer
	if err := inflateBuffered(r.source, &data, head.Size); err != nil {
		return ObjectEntry{}, fmt.Errorf("pack: object at %d: %w", offset, err)
	}
	r.read++
	return ObjectEntry{
		Header:         head,
		Data:           data.Bytes(),
		CRC32:          r.source.stopSum(),
		CompressedSize: r.source.position - offset,
	}, nil
}

func (r *Reader) finish() error {
	computed := r.source.digestSum()
	r.source.hashing = false
	r.done = true
	var trailer [hash.Size]byte
	if _, err := io.ReadFull(r.source, trailer[:]); err != nil {
		return fmt.Errorf("%w: packfile trailer: %v", ErrTruncated, err)
	}
	r.trailer = hash.ObjectID(trailer)
	if r.trailer != computed {
		return fmt.Errorf("%w: packfile declares %s, computed %s", ErrChecksumMismatch, r.trailer, computed)
	}
	return nil
}

type packCounter struct {
	source   *bufio.Reader
	position int64
	digest   stdhash.Hash
	sum      stdhash.Hash32
	summing  bool
	hashing  bool
	single   [1]byte
}

func newPackCounter(source io.Reader) *packCounter {
	return &packCounter{
		source:  bufio.NewReader(source),
		digest:  sha1.New(),
		sum:     crc32.NewIEEE(),
		hashing: true,
	}
}

func (c *packCounter) Read(into []byte) (int, error) {
	read, err := c.source.Read(into)
	c.consume(into[:read])
	return read, err
}

func (c *packCounter) ReadByte() (byte, error) {
	value, err := c.source.ReadByte()
	if err != nil {
		return 0, err
	}
	c.single[0] = value
	c.consume(c.single[:])
	return value, nil
}

func (c *packCounter) consume(chunk []byte) {
	c.position += int64(len(chunk))
	if c.hashing {
		_, _ = c.digest.Write(chunk)
	}
	if c.summing {
		_, _ = c.sum.Write(chunk)
	}
}

func (c *packCounter) startSum() {
	c.sum.Reset()
	c.summing = true
}

func (c *packCounter) stopSum() uint32 {
	c.summing = false
	return c.sum.Sum32()
}

func (c *packCounter) digestSum() hash.ObjectID {
	var raw [hash.Size]byte
	c.digest.Sum(raw[:0])
	return hash.ObjectID(raw)
}
