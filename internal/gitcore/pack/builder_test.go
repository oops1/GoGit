package pack

import (
	"bytes"
	"compress/zlib"
	"crypto/sha1"
	"encoding/binary"
	"hash/crc32"
	"slices"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
)

type packBuilder struct {
	body    []byte
	offsets []int64
	sums    []uint32
}

func newPackBuilder() *packBuilder {
	return &packBuilder{body: slices.Clone(packMagic)}
}

func (b *packBuilder) prepare() {
	if len(b.body) >= headerSize {
		return
	}
	b.body = binary.BigEndian.AppendUint32(b.body, packVersion)
	b.body = binary.BigEndian.AppendUint32(b.body, 0)
}

func (b *packBuilder) add(kind Kind, declared int64, base []byte, payload []byte) int64 {
	b.prepare()
	offset := int64(len(b.body))
	entry := encodeObjectHeader(kind, declared)
	entry = append(entry, base...)
	entry = append(entry, payload...)
	b.body = append(b.body, entry...)
	b.offsets = append(b.offsets, offset)
	b.sums = append(b.sums, crc32.ChecksumIEEE(entry))
	return offset
}

func (b *packBuilder) addObject(t testing.TB, kind Kind, content []byte) int64 {
	t.Helper()
	return b.add(kind, int64(len(content)), nil, deflate(t, content))
}

func (b *packBuilder) addOffsetDelta(t testing.TB, baseOffset int64, delta []byte) int64 {
	t.Helper()
	b.prepare()
	offset := int64(len(b.body))
	return b.add(KindOffsetDelta, int64(len(delta)), encodeBaseOffset(offset-baseOffset), deflate(t, delta))
}

func (b *packBuilder) addRefDelta(t testing.TB, base hash.ObjectID, delta []byte) int64 {
	t.Helper()
	return b.add(KindRefDelta, int64(len(delta)), base[:], deflate(t, delta))
}

func (b *packBuilder) addRaw(raw []byte) int64 {
	b.prepare()
	offset := int64(len(b.body))
	b.body = append(b.body, raw...)
	b.offsets = append(b.offsets, offset)
	b.sums = append(b.sums, crc32.ChecksumIEEE(raw))
	return offset
}

func (b *packBuilder) bytes() []byte {
	b.prepare()
	out := slices.Clone(b.body)
	binary.BigEndian.PutUint32(out[8:12], uint32(len(b.offsets)))
	return appendChecksum(out)
}

func appendChecksum(body []byte) []byte {
	sum := sha1.Sum(body)
	return append(slices.Clone(body), sum[:]...)
}

func encodeObjectHeader(kind Kind, size int64) []byte {
	current := byte(kind)<<kindShift | byte(size&sizeMask)
	size >>= sizeBits
	var out []byte
	for size > 0 {
		out = append(out, current|continuation)
		current = byte(size & payloadMask)
		size >>= payloadBits
	}
	return append(out, current)
}

func encodeBaseOffset(distance int64) []byte {
	var raw [16]byte
	last := len(raw) - 1
	raw[last] = byte(distance & payloadMask)
	for distance >>= payloadBits; distance > 0; distance >>= payloadBits {
		distance--
		last--
		raw[last] = continuation | byte(distance&payloadMask)
	}
	return raw[last:]
}

func deflate(t testing.TB, data []byte) []byte {
	t.Helper()
	var out bytes.Buffer
	writer := zlib.NewWriter(&out)
	if _, err := writer.Write(data); err != nil {
		t.Fatalf("Write returned error %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close returned error %v", err)
	}
	return out.Bytes()
}

func deltaSizes(source, target int64) []byte {
	return append(encodeDeltaSize(source), encodeDeltaSize(target)...)
}

func encodeDeltaSize(size int64) []byte {
	var out []byte
	for {
		current := byte(size & payloadMask)
		size >>= payloadBits
		if size == 0 {
			return append(out, current)
		}
		out = append(out, current|continuation)
	}
}

func insertOp(data []byte) []byte {
	return append([]byte{byte(len(data))}, data...)
}

func copyOp(offset, size uint32) []byte {
	opcode := byte(copyOpcode)
	var tail []byte
	for i := range copyOffsetBytes {
		if current := byte(offset >> (8 * uint(i))); current != 0 {
			opcode |= 1 << uint(i)
			tail = append(tail, current)
		}
	}
	if size == defaultCopySize {
		size = 0
	}
	for i := range copySizeBytes {
		if current := byte(size >> (8 * uint(i))); current != 0 {
			opcode |= 1 << uint(copySizeShift+i)
			tail = append(tail, current)
		}
	}
	return append([]byte{opcode}, tail...)
}

func buildIndex(entries []Entry, packHash hash.ObjectID) []byte {
	sorted := slices.Clone(entries)
	slices.SortFunc(sorted, func(a, b Entry) int { return a.ID.Compare(b.ID) })
	return buildIndexAsIs(sorted, packHash)
}

func buildIndexAsIs(entries []Entry, packHash hash.ObjectID) []byte {
	out := slices.Clone(indexMagic)
	out = binary.BigEndian.AppendUint32(out, indexVersion)
	var fanout [fanoutEntries]uint32
	for _, entry := range entries {
		for bucket := int(entry.ID[0]); bucket < fanoutEntries; bucket++ {
			fanout[bucket]++
		}
	}
	for _, value := range fanout {
		out = binary.BigEndian.AppendUint32(out, value)
	}
	for _, entry := range entries {
		out = append(out, entry.ID[:]...)
	}
	for _, entry := range entries {
		out = binary.BigEndian.AppendUint32(out, entry.CRC32)
	}
	var large []int64
	for _, entry := range entries {
		if entry.Offset < int64(largeOffsetFlag) {
			out = binary.BigEndian.AppendUint32(out, uint32(entry.Offset))
			continue
		}
		out = binary.BigEndian.AppendUint32(out, largeOffsetFlag|uint32(len(large)))
		large = append(large, entry.Offset)
	}
	for _, offset := range large {
		out = binary.BigEndian.AppendUint64(out, uint64(offset))
	}
	out = append(out, packHash[:]...)
	return appendChecksum(out)
}

func packIndexPair(t testing.TB, builder *packBuilder, ids []hash.ObjectID) ([]byte, []byte) {
	t.Helper()
	raw := builder.bytes()
	entries := make([]Entry, 0, len(ids))
	for i, id := range ids {
		entries = append(entries, Entry{ID: id, Offset: builder.offsets[i], CRC32: builder.sums[i]})
	}
	return raw, buildIndex(entries, hash.ObjectID(raw[len(raw)-hash.Size:]))
}
