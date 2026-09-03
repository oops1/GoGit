package pack

import (
	"math"
	"slices"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
)

func idBytes(values ...byte) hash.ObjectID {
	var id hash.ObjectID
	copy(id[:], values)
	return id
}

func entriesFor(ids []hash.ObjectID) []Entry {
	entries := make([]Entry, len(ids))
	for i, id := range ids {
		entries[i] = Entry{ID: id, Offset: int64(i) + 1, CRC32: uint32(i) + 1}
	}
	return entries
}

func indexFromIDs(t *testing.T, ids []hash.ObjectID) *Index {
	t.Helper()
	return indexOf(t, buildIndex(entriesFor(ids), hash.Zero))
}

func collectPrefix(index *Index, prefix []byte, bits int) []hash.ObjectID {
	return slices.Collect(index.Prefix(prefix, bits))
}

func sortedIDs(ids []hash.ObjectID) []hash.ObjectID {
	sorted := slices.Clone(ids)
	slices.SortFunc(sorted, func(a, b hash.ObjectID) int { return a.Compare(b) })
	return sorted
}

func TestPrefixFindsObjectsSharingAByteAlignedPrefix(t *testing.T) {
	inside1 := idBytes(0xab, 0xcd, 0x01)
	inside2 := idBytes(0xab, 0xcd, 0x02)
	outside := idBytes(0xab, 0xce, 0x00)
	index := indexFromIDs(t, []hash.ObjectID{inside1, inside2, outside})
	got := collectPrefix(index, []byte{0xab, 0xcd}, 16)
	want := sortedIDs([]hash.ObjectID{inside1, inside2})
	if !slices.Equal(got, want) {
		t.Fatalf("Prefix(abcd) = %v, want %v", got, want)
	}
}

func TestPrefixHandlesAnOddNibbleBoundary(t *testing.T) {
	inside1 := idBytes(0xab, 0xc3)
	inside2 := idBytes(0xab, 0xcf)
	outside := idBytes(0xab, 0xd0)
	index := indexFromIDs(t, []hash.ObjectID{inside1, inside2, outside})
	got := collectPrefix(index, []byte{0xab, 0xc0}, 12)
	want := sortedIDs([]hash.ObjectID{inside1, inside2})
	if !slices.Equal(got, want) {
		t.Fatalf("Prefix(abc) = %v, want %v", got, want)
	}
}

func TestPrefixMatchesEveryObjectWhenBitsIsZero(t *testing.T) {
	ids := []hash.ObjectID{idBytes(0x01), idBytes(0x02), idBytes(0xff)}
	index := indexFromIDs(t, ids)
	got := collectPrefix(index, nil, 0)
	want := sortedIDs(ids)
	if !slices.Equal(got, want) {
		t.Fatalf("Prefix with zero bits = %v, want %v", got, want)
	}
}

func TestPrefixMatchesExactlyOneObjectAtFullBitWidth(t *testing.T) {
	target := idBytes(0xab, 0xcd, 0xef)
	other := idBytes(0xab, 0xcd, 0xee)
	index := indexFromIDs(t, []hash.ObjectID{target, other})
	got := collectPrefix(index, target[:], hash.Size*8)
	if !slices.Equal(got, []hash.ObjectID{target}) {
		t.Fatalf("Prefix at full width = %v, want [%s]", got, target)
	}
}

func TestPrefixFindsNothingOutsideTheFanoutRange(t *testing.T) {
	index := indexFromIDs(t, []hash.ObjectID{idBytes(0x10), idBytes(0x20)})
	got := collectPrefix(index, []byte{0x99}, 8)
	if len(got) != 0 {
		t.Fatalf("Prefix(0x99) = %v, want none", got)
	}
}

func TestPrefixStopsWhenTheCallerStops(t *testing.T) {
	index := indexFromIDs(t, []hash.ObjectID{idBytes(0x01), idBytes(0x02), idBytes(0x03)})
	seen := 0
	for range index.Prefix(nil, 0) {
		seen++
		break
	}
	if seen != 1 {
		t.Fatalf("Prefix yielded %d entries after a break, want 1", seen)
	}
}

func TestPrefixStopsWhenLowerBoundFails(t *testing.T) {
	broken := brokenIndex(t, indexTablesAt, math.MaxInt64)
	if got := collectPrefix(broken, nil, 0); len(got) != 0 {
		t.Fatalf("Prefix on a broken index yielded %v", got)
	}
}

func TestPrefixStopsWhenIterationFails(t *testing.T) {
	ids := make([]hash.ObjectID, 8)
	for i := range ids {
		ids[i] = idBytes(byte((i + 1) * 0x10))
	}
	raw := buildIndex(entriesFor(ids), hash.Zero)
	index := indexOf(t, raw)
	brokenAt := index.names + 3*hash.Size
	index.source = brokenReaderAt{data: raw, from: brokenAt, to: brokenAt + hash.Size}
	got := collectPrefix(index, nil, 0)
	if len(got) != 3 {
		t.Fatalf("Prefix stopped after %d entries, want 3", len(got))
	}
}
