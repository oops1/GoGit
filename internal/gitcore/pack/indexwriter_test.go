package pack

import (
	"bytes"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
)

func syntheticID(t testing.TB, seed byte) hash.ObjectID {
	t.Helper()
	var id hash.ObjectID
	for i := range id {
		id[i] = seed + byte(i)
	}
	return id
}

func TestWriteIndexRoundTripsAndFindsEveryObject(t *testing.T) {
	entries := []Entry{
		{ID: syntheticID(t, 0x10), Offset: 12, CRC32: 0x11111111},
		{ID: syntheticID(t, 0x30), Offset: 512, CRC32: 0x22222222},
		{ID: syntheticID(t, 0x05), Offset: 4096, CRC32: 0x33333333},
	}
	checksum := syntheticID(t, 0xaa)
	indexBytes := requireWriteIndex(t, entries, checksum)
	index := openWrittenIndex(t, indexBytes)
	defer func() { _ = index.Close() }()

	if index.Count() != len(entries) {
		t.Fatalf("Count = %d, want %d", index.Count(), len(entries))
	}
	if index.PackHash() != checksum {
		t.Fatalf("PackHash = %s, want %s", index.PackHash(), checksum)
	}
	if err := index.Verify(); err != nil {
		t.Fatalf("Verify returned error %v", err)
	}
	for _, entry := range entries {
		offset, ok, err := index.Lookup(entry.ID)
		if err != nil || !ok {
			t.Fatalf("Lookup(%s) returned (%v, %v)", entry.ID, ok, err)
		}
		if offset != entry.Offset {
			t.Errorf("Lookup(%s) = %d, want %d", entry.ID, offset, entry.Offset)
		}
		position, ok, err := index.Position(entry.ID)
		if err != nil || !ok {
			t.Fatalf("Position(%s) returned (%v, %v)", entry.ID, ok, err)
		}
		found, err := index.EntryAt(position)
		if err != nil {
			t.Fatalf("EntryAt(%d) returned error %v", position, err)
		}
		if found.CRC32 != entry.CRC32 {
			t.Errorf("EntryAt(%d).CRC32 = %08x, want %08x", position, found.CRC32, entry.CRC32)
		}
	}
}

func TestWriteIndexBuildsLargeOffsetTableAboveTwoGigabytes(t *testing.T) {
	const twoGiB = int64(1) << 31
	entries := []Entry{
		{ID: syntheticID(t, 0x01), Offset: 100, CRC32: 1},
		{ID: syntheticID(t, 0x02), Offset: twoGiB, CRC32: 2},
		{ID: syntheticID(t, 0x03), Offset: twoGiB + 5_000_000_000, CRC32: 3},
		{ID: syntheticID(t, 0x04), Offset: twoGiB - 1, CRC32: 4},
	}
	checksum := syntheticID(t, 0xbb)
	indexBytes := requireWriteIndex(t, entries, checksum)
	index := openWrittenIndex(t, indexBytes)
	defer func() { _ = index.Close() }()

	if err := index.Verify(); err != nil {
		t.Fatalf("Verify returned error %v", err)
	}
	if index.largeCount == 0 {
		t.Fatal("expected the large offset table to hold at least one entry")
	}
	for _, entry := range entries {
		offset, ok, err := index.Lookup(entry.ID)
		if err != nil || !ok {
			t.Fatalf("Lookup(%s) returned (%v, %v)", entry.ID, ok, err)
		}
		if offset != entry.Offset {
			t.Errorf("Lookup(%s) = %d, want %d", entry.ID, offset, entry.Offset)
		}
	}
}

func TestWriteIndexSortsEntriesByID(t *testing.T) {
	entries := []Entry{
		{ID: syntheticID(t, 0x30), Offset: 1, CRC32: 1},
		{ID: syntheticID(t, 0x10), Offset: 2, CRC32: 2},
		{ID: syntheticID(t, 0x20), Offset: 3, CRC32: 3},
	}
	indexBytes := requireWriteIndex(t, entries, syntheticID(t, 0xcc))
	index := openWrittenIndex(t, indexBytes)
	defer func() { _ = index.Close() }()

	previous := hash.Zero
	for position := range index.Count() {
		id, err := index.idAt(position)
		if err != nil {
			t.Fatalf("idAt(%d) returned error %v", position, err)
		}
		if position > 0 && id.Compare(previous) <= 0 {
			t.Fatalf("entry %d (%s) is not strictly greater than the previous entry %s", position, id, previous)
		}
		previous = id
	}
}

func TestWriteIndexRejectsAFailingWriterAtEveryStage(t *testing.T) {
	entries := []Entry{
		{ID: syntheticID(t, 0x10), Offset: 1, CRC32: 1},
		{ID: syntheticID(t, 0x20), Offset: 2, CRC32: 2},
	}
	checksum := syntheticID(t, 0xdd)
	var full bytes.Buffer
	if err := WriteIndex(&full, entries, checksum); err != nil {
		t.Fatalf("WriteIndex returned error %v", err)
	}
	bodyLen := int64(len(full.Bytes())) - hash.Size - hash.Size

	for _, limit := range []int64{0, bodyLen, bodyLen + hash.Size} {
		writer := &limitedWriter{limit: limit}
		if err := WriteIndex(writer, entries, checksum); err == nil {
			t.Fatalf("WriteIndex with a limit of %d returned no error", limit)
		}
	}
}
