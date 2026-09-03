package pack

import (
	"bytes"
	"crypto/sha1"
	"encoding/binary"
	"fmt"
	"io"
	"iter"
	"os"

	"github.com/oops1/gogit/internal/gitcore/hash"
)

const (
	indexVersion     = 2
	fanoutEntries    = 256
	fanoutSize       = fanoutEntries * 4
	indexHeaderSize  = 8
	indexTablesAt    = indexHeaderSize + fanoutSize
	crcSize          = 4
	offsetSize       = 4
	largeOffsetSize  = 8
	indexTrailerSize = 2 * hash.Size
	largeOffsetFlag  = uint32(1) << 31
	namesChunk       = 512
)

var indexMagic = []byte{0xff, 0x74, 0x4f, 0x63}

type Entry struct {
	ID     hash.ObjectID
	Offset int64
	CRC32  uint32
}

type Index struct {
	source     io.ReaderAt
	closer     io.Closer
	path       string
	size       int64
	count      int
	fanout     [fanoutEntries]uint32
	names      int64
	crcs       int64
	offsets    int64
	larges     int64
	largeCount int64
	pack       hash.ObjectID
	self       hash.ObjectID
}

func OpenIndex(path string) (*Index, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("pack: open %s: %w", path, err)
	}
	index, err := NewIndexFile(file)
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("pack: %s: %w", path, err)
	}
	return index, nil
}

func NewIndexFile(file *os.File) (*Index, error) {
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	index, err := NewIndex(file, info.Size())
	if err != nil {
		return nil, err
	}
	index.closer = file
	index.path = file.Name()
	return index, nil
}

func NewIndex(source io.ReaderAt, size int64) (*Index, error) {
	if size < indexTablesAt+indexTrailerSize {
		return nil, fmt.Errorf("%w: pack index holds %d bytes", ErrTruncated, size)
	}
	index := &Index{source: source, size: size}
	var head [indexHeaderSize]byte
	if err := readFull(source, head[:], 0); err != nil {
		return nil, err
	}
	if !bytes.Equal(head[:len(indexMagic)], indexMagic) {
		return nil, fmt.Errorf("%w: version 1", ErrUnsupportedIndexVersion)
	}
	if version := binary.BigEndian.Uint32(head[len(indexMagic):]); version != indexVersion {
		return nil, fmt.Errorf("%w: version %d", ErrUnsupportedIndexVersion, version)
	}
	if err := index.readFanout(); err != nil {
		return nil, err
	}
	if err := index.layout(); err != nil {
		return nil, err
	}
	var trailer [indexTrailerSize]byte
	if err := readFull(source, trailer[:], size-indexTrailerSize); err != nil {
		return nil, err
	}
	index.pack = hash.ObjectID(trailer[:hash.Size])
	index.self = hash.ObjectID(trailer[hash.Size:])
	return index, nil
}

func (x *Index) readFanout() error {
	raw := make([]byte, fanoutSize)
	if err := readFull(x.source, raw, indexHeaderSize); err != nil {
		return err
	}
	var previous uint32
	for bucket := range x.fanout {
		value := binary.BigEndian.Uint32(raw[bucket*4:])
		if value < previous {
			return fmt.Errorf("%w: fanout bucket %d holds %d after %d", ErrCorruptIndex, bucket, value, previous)
		}
		x.fanout[bucket] = value
		previous = value
	}
	x.count = int(previous)
	return nil
}

func (x *Index) layout() error {
	entries := int64(x.count)
	x.names = indexTablesAt
	x.crcs = x.names + entries*hash.Size
	x.offsets = x.crcs + entries*crcSize
	x.larges = x.offsets + entries*offsetSize
	room := x.size - indexTrailerSize - x.larges
	if room < 0 {
		return fmt.Errorf("%w: %d objects need more than %d bytes", ErrTruncated, x.count, x.size)
	}
	if room%largeOffsetSize != 0 {
		return fmt.Errorf("%w: large offset table holds %d bytes", ErrCorruptIndex, room)
	}
	x.largeCount = room / largeOffsetSize
	return nil
}

func (x *Index) Path() string {
	return x.path
}

func (x *Index) Count() int {
	return x.count
}

func (x *Index) PackHash() hash.ObjectID {
	return x.pack
}

func (x *Index) Checksum() hash.ObjectID {
	return x.self
}

func (x *Index) Close() error {
	if x.closer == nil {
		return nil
	}
	return x.closer.Close()
}

func (x *Index) EntryAt(position int) (Entry, error) {
	if position < 0 || position >= x.count {
		return Entry{}, fmt.Errorf("%w: entry %d of %d", ErrOutOfRange, position, x.count)
	}
	id, err := x.idAt(position)
	if err != nil {
		return Entry{}, err
	}
	offset, err := x.offsetAt(position)
	if err != nil {
		return Entry{}, err
	}
	sum, err := x.crcAt(position)
	if err != nil {
		return Entry{}, err
	}
	return Entry{ID: id, Offset: offset, CRC32: sum}, nil
}

func (x *Index) Position(id hash.ObjectID) (int, bool, error) {
	low := 0
	if id[0] > 0 {
		low = int(x.fanout[id[0]-1])
	}
	high := int(x.fanout[id[0]])
	for low < high {
		middle := int(uint(low+high) >> 1)
		found, err := x.idAt(middle)
		if err != nil {
			return 0, false, err
		}
		switch found.Compare(id) {
		case 0:
			return middle, true, nil
		case -1:
			low = middle + 1
		default:
			high = middle
		}
	}
	return 0, false, nil
}

func (x *Index) Lookup(id hash.ObjectID) (int64, bool, error) {
	position, ok, err := x.Position(id)
	if err != nil || !ok {
		return 0, false, err
	}
	offset, err := x.offsetAt(position)
	if err != nil {
		return 0, false, err
	}
	return offset, true, nil
}

func (x *Index) Find(id hash.ObjectID) (int64, bool) {
	offset, ok, err := x.Lookup(id)
	if err != nil {
		return 0, false
	}
	return offset, ok
}

func (x *Index) Prefix(prefix []byte, bits int) iter.Seq[hash.ObjectID] {
	return func(yield func(hash.ObjectID) bool) {
		low, high := prefixBounds(prefix, bits)
		start, err := x.lowerBound(low)
		if err != nil {
			return
		}
		for position := start; position < x.count; position++ {
			id, err := x.idAt(position)
			if err != nil {
				return
			}
			if bytes.Compare(id[:], high[:]) > 0 {
				return
			}
			if !yield(id) {
				return
			}
		}
	}
}

func (x *Index) lowerBound(low hash.ObjectID) (int, error) {
	lo, hi := 0, x.count
	for lo < hi {
		middle := int(uint(lo+hi) >> 1)
		id, err := x.idAt(middle)
		if err != nil {
			return 0, err
		}
		if bytes.Compare(id[:], low[:]) < 0 {
			lo = middle + 1
		} else {
			hi = middle
		}
	}
	return lo, nil
}

func prefixBounds(prefix []byte, bits int) (hash.ObjectID, hash.ObjectID) {
	var low, high hash.ObjectID
	bits = min(max(bits, 0), hash.Size*8)
	fullBytes := bits / 8
	remBits := bits % 8
	copy(low[:fullBytes], prefix)
	copy(high[:fullBytes], prefix)
	if remBits > 0 {
		mask := byte(0xff) << (8 - remBits)
		var partial byte
		if fullBytes < len(prefix) {
			partial = prefix[fullBytes]
		}
		low[fullBytes] = partial & mask
		high[fullBytes] = partial | ^mask
		fullBytes++
	}
	for i := fullBytes; i < hash.Size; i++ {
		high[i] = 0xff
	}
	return low, high
}

func (x *Index) Objects() iter.Seq[hash.ObjectID] {
	return func(yield func(hash.ObjectID) bool) {
		block := make([]byte, namesChunk*hash.Size)
		for start := 0; start < x.count; start += namesChunk {
			size := min(namesChunk, x.count-start)
			chunk := block[:size*hash.Size]
			if err := readFull(x.source, chunk, x.names+int64(start)*hash.Size); err != nil {
				return
			}
			for i := range size {
				if !yield(hash.ObjectID(chunk[i*hash.Size : (i+1)*hash.Size])) {
					return
				}
			}
		}
	}
}

func (x *Index) Verify() error {
	if err := verifyChecksum(x.source, x.size, x.self, x.path); err != nil {
		return err
	}
	return x.verifyOrder()
}

func (x *Index) verifyOrder() error {
	previous := hash.Zero
	for position := range x.count {
		id, err := x.idAt(position)
		if err != nil {
			return err
		}
		if position > 0 && id.Compare(previous) <= 0 {
			return fmt.Errorf("%w: %s follows %s", ErrCorruptIndex, id, previous)
		}
		if position >= int(x.fanout[id[0]]) {
			return fmt.Errorf("%w: %s sits outside fanout bucket %d", ErrCorruptIndex, id, id[0])
		}
		previous = id
	}
	return nil
}

func (x *Index) idAt(position int) (hash.ObjectID, error) {
	var id hash.ObjectID
	if err := readFull(x.source, id[:], x.names+int64(position)*hash.Size); err != nil {
		return hash.Zero, err
	}
	return id, nil
}

func (x *Index) crcAt(position int) (uint32, error) {
	var raw [crcSize]byte
	if err := readFull(x.source, raw[:], x.crcs+int64(position)*crcSize); err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint32(raw[:]), nil
}

func (x *Index) offsetAt(position int) (int64, error) {
	var raw [offsetSize]byte
	if err := readFull(x.source, raw[:], x.offsets+int64(position)*offsetSize); err != nil {
		return 0, err
	}
	value := binary.BigEndian.Uint32(raw[:])
	if value&largeOffsetFlag == 0 {
		return int64(value), nil
	}
	slot := int64(value &^ largeOffsetFlag)
	if slot >= x.largeCount {
		return 0, fmt.Errorf("%w: large offset %d of %d", ErrCorruptIndex, slot, x.largeCount)
	}
	var wide [largeOffsetSize]byte
	if err := readFull(x.source, wide[:], x.larges+slot*largeOffsetSize); err != nil {
		return 0, err
	}
	offset := int64(binary.BigEndian.Uint64(wide[:]))
	if offset < 0 {
		return 0, fmt.Errorf("%w: large offset %d is negative", ErrBadOffset, slot)
	}
	return offset, nil
}

func verifyChecksum(source io.ReaderAt, size int64, want hash.ObjectID, path string) error {
	digest := sha1.New()
	if _, err := io.Copy(digest, io.NewSectionReader(source, 0, size-hash.Size)); err != nil {
		return fmt.Errorf("pack: read %s: %w", path, err)
	}
	var raw [hash.Size]byte
	digest.Sum(raw[:0])
	if got := hash.ObjectID(raw); got != want {
		return fmt.Errorf("%w: %s declares %s, computed %s", ErrChecksumMismatch, path, want, got)
	}
	return nil
}
