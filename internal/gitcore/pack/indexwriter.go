package pack

import (
	"encoding/binary"
	"fmt"
	"io"
	"slices"

	"github.com/oops1/gogit/internal/gitcore/hash"
)

func WriteIndex(dst io.Writer, entries []Entry, packChecksum hash.ObjectID) error {
	sorted := slices.Clone(entries)
	slices.SortFunc(sorted, func(a, b Entry) int { return a.ID.Compare(b.ID) })
	writer := newPackWriter(dst)
	if _, err := writer.Write(encodeIndexBody(sorted)); err != nil {
		return fmt.Errorf("pack: write index: %w", err)
	}
	if _, err := writer.Write(packChecksum[:]); err != nil {
		return fmt.Errorf("pack: write index pack checksum: %w", err)
	}
	if _, err := writer.finish(); err != nil {
		return fmt.Errorf("pack: write index checksum: %w", err)
	}
	return nil
}

func encodeIndexBody(sorted []Entry) []byte {
	out := make([]byte, 0, indexTablesAt+len(sorted)*(hash.Size+crcSize+offsetSize))
	out = append(out, indexMagic...)
	out = binary.BigEndian.AppendUint32(out, indexVersion)
	out = appendIndexFanout(out, sorted)
	for _, entry := range sorted {
		out = append(out, entry.ID[:]...)
	}
	for _, entry := range sorted {
		out = binary.BigEndian.AppendUint32(out, entry.CRC32)
	}
	return appendIndexOffsets(out, sorted)
}

func appendIndexFanout(out []byte, sorted []Entry) []byte {
	var fanout [fanoutEntries]uint32
	for _, entry := range sorted {
		for bucket := int(entry.ID[0]); bucket < fanoutEntries; bucket++ {
			fanout[bucket]++
		}
	}
	for _, count := range fanout {
		out = binary.BigEndian.AppendUint32(out, count)
	}
	return out
}

func appendIndexOffsets(out []byte, sorted []Entry) []byte {
	var large []int64
	for _, entry := range sorted {
		if entry.Offset >= 0 && entry.Offset < int64(largeOffsetFlag) {
			out = binary.BigEndian.AppendUint32(out, uint32(entry.Offset))
			continue
		}
		out = binary.BigEndian.AppendUint32(out, largeOffsetFlag|uint32(len(large)))
		large = append(large, entry.Offset)
	}
	for _, offset := range large {
		out = binary.BigEndian.AppendUint64(out, uint64(offset))
	}
	return out
}
