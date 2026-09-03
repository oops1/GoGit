package pack

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"iter"
	"slices"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
)

func readerOf(t *testing.T, raw []byte) *Reader {
	t.Helper()
	reader, err := NewReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("NewReader returned error %v", err)
	}
	return reader
}

func readAll(t *testing.T, reader *Reader) iter.Seq2[ObjectEntry, error] {
	t.Helper()
	return func(yield func(ObjectEntry, error) bool) {
		for {
			entry, err := reader.NextObject()
			if errors.Is(err, io.EOF) {
				return
			}
			if !yield(entry, err) || err != nil {
				return
			}
		}
	}
}

func TestReaderWalksEveryFixture(t *testing.T) {
	for _, name := range fixtureNames(t) {
		t.Run(name, func(t *testing.T) {
			raw := readFixture(t, fixturePackPath(t, name))
			reader := readerOf(t, raw)
			if reader.Version() != packVersion {
				t.Fatalf("Version = %d, want %d", reader.Version(), packVersion)
			}
			wanted := showIndexEntries(t, name)
			byOffset := make(map[int64]Entry, len(wanted))
			for _, entry := range wanted {
				byOffset[entry.Offset] = entry
			}
			records := make(map[int64]packRecord)
			for _, record := range verifyRecords(t, name) {
				records[record.offset] = record
			}
			seen := 0
			for entry, err := range readAll(t, reader) {
				if err != nil {
					t.Fatalf("NextObject returned error %v", err)
				}
				want, ok := byOffset[entry.Header.Offset]
				if !ok {
					t.Fatalf("git does not list an object at %d", entry.Header.Offset)
				}
				if entry.CRC32 != want.CRC32 {
					t.Errorf("object at %d has checksum %08x, git says %08x",
						entry.Header.Offset, entry.CRC32, want.CRC32)
				}
				record := records[entry.Header.Offset]
				if entry.CompressedSize != record.packed {
					t.Errorf("object at %d takes %d bytes, git says %d",
						entry.Header.Offset, entry.CompressedSize, record.packed)
				}
				if int64(len(entry.Data)) != record.size {
					t.Errorf("object at %d holds %d bytes, git says %d",
						entry.Header.Offset, len(entry.Data), record.size)
				}
				seen++
			}
			if seen != reader.Count() {
				t.Fatalf("the reader walked %d objects, the header declares %d", seen, reader.Count())
			}
			if reader.Trailer() != hash.ObjectID(raw[len(raw)-hash.Size:]) {
				t.Fatalf("Trailer = %s, the packfile ends with %x", reader.Trailer(), raw[len(raw)-hash.Size:])
			}
			if reader.Offset() != int64(len(raw)) {
				t.Fatalf("Offset = %d after the trailer, want %d", reader.Offset(), len(raw))
			}
		})
	}
}

func TestReaderRebuildsUndeltifiedObjects(t *testing.T) {
	name := fixtureName(t, offsetPack)
	reader := readerOf(t, readFixture(t, fixturePackPath(t, name)))
	byOffset := make(map[int64]packRecord)
	for _, record := range verifyRecords(t, name) {
		byOffset[record.offset] = record
	}
	plain := 0
	for entry, err := range readAll(t, reader) {
		if err != nil {
			t.Fatalf("NextObject returned error %v", err)
		}
		if entry.Header.Kind.IsDelta() {
			continue
		}
		want := byOffset[entry.Header.Offset]
		if got := hash.SumSHA1(entry.Header.Kind.Type().String(), entry.Data); got != want.id {
			t.Errorf("object at %d is %s, git says %s", entry.Header.Offset, got, want.id)
		}
		plain++
	}
	if plain == 0 {
		t.Fatal("the fixture holds no undeltified objects")
	}
}

func TestReaderReportsEndOfFileForever(t *testing.T) {
	reader := readerOf(t, readFixture(t, fixturePackPath(t, fixtureName(t, offsetPack))))
	for range readAll(t, reader) {
	}
	for range 2 {
		if _, err := reader.NextObject(); !errors.Is(err, io.EOF) {
			t.Fatalf("NextObject returned %v, want %v", err, io.EOF)
		}
	}
}

func TestNewReaderRejectsBrokenHeaders(t *testing.T) {
	valid := readFixture(t, fixturePackPath(t, fixtureName(t, offsetPack)))
	badMagic := slices.Clone(valid)
	badMagic[0] = 'x'
	badVersion := slices.Clone(valid)
	binary.BigEndian.PutUint32(badVersion[4:], 3)

	cases := []struct {
		name string
		raw  []byte
		want error
	}{
		{"tooShort", valid[:headerSize-1], ErrTruncated},
		{"badMagic", badMagic, ErrBadMagic},
		{"version3", badVersion, ErrUnsupportedVersion},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewReader(bytes.NewReader(tc.raw)); !errors.Is(err, tc.want) {
				t.Fatalf("NewReader returned %v, want %v", err, tc.want)
			}
		})
	}
}

func TestReaderRejectsTruncatedStreams(t *testing.T) {
	builder := newPackBuilder()
	base := builder.addObject(t, KindBlob, []byte("base object"))
	builder.addOffsetDelta(t, base, slices.Concat(deltaSizes(11, 11), copyOp(0, 11)))
	builder.addRefDelta(t, idOfByte(0x11), slices.Concat(deltaSizes(0, 1), insertOp([]byte("a"))))
	full := builder.bytes()

	cases := []struct {
		name string
		raw  []byte
		want error
	}{
		{"noObjects", full[:headerSize], ErrTruncated},
		{"halfStream", full[:headerSize+1], ErrDecompress},
		{"halfCompressedData", full[:headerSize+6], ErrDecompress},
		{"halfTrailer", full[:len(full)-1], ErrTruncated},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reader := readerOf(t, tc.raw)
			var last error
			for _, err := range readAll(t, reader) {
				last = err
			}
			if !errors.Is(last, tc.want) {
				t.Fatalf("the reader stopped with %v, want %v", last, tc.want)
			}
		})
	}
}

func TestReaderRejectsTruncatedObjectHeaders(t *testing.T) {
	cases := []struct {
		name  string
		entry []byte
	}{
		{"size", []byte{0xb0 | continuation}},
		{"deltaOffset", []byte{0x60}},
		{"deltaName", []byte{0x70 | 0x01, 0x01, 0x02}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			head := slices.Concat(packMagic, binary.BigEndian.AppendUint32(nil, packVersion),
				binary.BigEndian.AppendUint32(nil, 1))
			reader := readerOf(t, slices.Concat(head, tc.entry))
			if _, err := reader.NextObject(); !errors.Is(err, ErrTruncated) {
				t.Fatalf("NextObject returned %v, want %v", err, ErrTruncated)
			}
		})
	}
}

func TestReaderDetectsBrokenTrailer(t *testing.T) {
	builder := newPackBuilder()
	builder.addObject(t, KindBlob, []byte("content"))
	raw := builder.bytes()
	raw[len(raw)-1] ^= 0xff
	reader := readerOf(t, raw)
	var last error
	for _, err := range readAll(t, reader) {
		last = err
	}
	if !errors.Is(last, ErrChecksumMismatch) {
		t.Fatalf("the reader stopped with %v, want %v", last, ErrChecksumMismatch)
	}
}

func TestReaderIgnoresBytesAfterTheTrailer(t *testing.T) {
	builder := newPackBuilder()
	builder.addObject(t, KindBlob, []byte("first"))
	raw := builder.bytes()
	reader := readerOf(t, slices.Concat(raw, []byte("trailing framing bytes")))
	seen := 0
	for _, err := range readAll(t, reader) {
		if err != nil {
			t.Fatalf("NextObject returned error %v", err)
		}
		seen++
	}
	if seen != 1 {
		t.Fatalf("the reader walked %d objects, want 1", seen)
	}
	if reader.Trailer() != hash.ObjectID(raw[len(raw)-hash.Size:]) {
		t.Fatalf("Trailer = %s, want the packfile trailer", reader.Trailer())
	}
}

func TestReaderReadsDeltaHeaders(t *testing.T) {
	builder := newPackBuilder()
	base := builder.addObject(t, KindBlob, []byte("base object"))
	offsetDelta := builder.addOffsetDelta(t, base, slices.Concat(deltaSizes(11, 11), copyOp(0, 11)))
	refDelta := builder.addRefDelta(t, idOfByte(0x11), slices.Concat(deltaSizes(0, 1), insertOp([]byte("a"))))
	reader := readerOf(t, builder.bytes())
	var heads []ObjectHeader
	for entry, err := range readAll(t, reader) {
		if err != nil {
			t.Fatalf("NextObject returned error %v", err)
		}
		heads = append(heads, entry.Header)
	}
	if len(heads) != 3 {
		t.Fatalf("the reader walked %d objects, want 3", len(heads))
	}
	if heads[1].Offset != offsetDelta || heads[1].BaseOffset != base {
		t.Fatalf("the offset delta at %d points at %d, want %d at %d",
			heads[1].Offset, heads[1].BaseOffset, base, offsetDelta)
	}
	if heads[2].Offset != refDelta || heads[2].BaseID != idOfByte(0x11) {
		t.Fatalf("the reference delta at %d points at %s", heads[2].Offset, heads[2].BaseID)
	}
}

func TestReaderRejectsTruncatedDeltaOffsets(t *testing.T) {
	head := slices.Concat(packMagic, binary.BigEndian.AppendUint32(nil, packVersion),
		binary.BigEndian.AppendUint32(nil, 1))
	reader := readerOf(t, slices.Concat(head, []byte{0x60, continuation | 0x01}))
	if _, err := reader.NextObject(); !errors.Is(err, ErrTruncated) {
		t.Fatalf("NextObject returned %v, want %v", err, ErrTruncated)
	}
}
