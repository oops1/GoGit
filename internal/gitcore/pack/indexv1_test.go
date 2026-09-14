package pack

import (
	"bytes"
	"crypto/sha1"
	"encoding/binary"
	"errors"
	"path/filepath"
	"slices"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
)

func encodeIndexVersionOne(entries []Entry, packChecksum hash.ObjectID) []byte {
	sorted := slices.Clone(entries)
	slices.SortFunc(sorted, func(a, b Entry) int { return a.ID.Compare(b.ID) })
	out := appendIndexFanout(nil, sorted)
	for _, entry := range sorted {
		out = binary.BigEndian.AppendUint32(out, uint32(entry.Offset))
		out = append(out, entry.ID[:]...)
	}
	out = append(out, packChecksum[:]...)
	sum := sha1.Sum(out)
	return append(out, sum[:]...)
}

func fixtureEntries(t *testing.T, name string) ([]Entry, *Index) {
	t.Helper()
	index := openFixtureIndex(t, name)
	entries := make([]Entry, index.Count())
	for position := range entries {
		entry, err := index.EntryAt(position)
		if err != nil {
			t.Fatalf("EntryAt(%d) returned error %v", position, err)
		}
		entries[position] = entry
	}
	return entries, index
}

func TestVersionOneIndexAgreesWithVersionTwo(t *testing.T) {
	entries, modern := fixtureEntries(t, fixtureName(t, offsetPack))
	legacy := indexOf(t, encodeIndexVersionOne(entries, modern.PackHash()))

	if legacy.Version() != indexVersionOne || modern.Version() != indexVersion {
		t.Fatalf("versions = %d and %d, want 1 and 2", legacy.Version(), modern.Version())
	}
	if legacy.Count() != modern.Count() || legacy.PackHash() != modern.PackHash() {
		t.Fatalf("the version 1 index lists %d objects of %s, want %d of %s", legacy.Count(), legacy.PackHash(), modern.Count(), modern.PackHash())
	}
	if err := legacy.Verify(); err != nil {
		t.Fatalf("Verify returned error %v", err)
	}
	for position, want := range entries {
		got, err := legacy.EntryAt(position)
		if err != nil || got.ID != want.ID || got.Offset != want.Offset || got.CRC32 != 0 {
			t.Fatalf("EntryAt(%d) = (%+v, %v), want %s at %d without a checksum", position, got, err, want.ID, want.Offset)
		}
		if offset, ok := legacy.Find(want.ID); !ok || offset != want.Offset {
			t.Fatalf("Find(%s) = (%d, %v), want %d", want.ID, offset, ok, want.Offset)
		}
	}
	if !slices.Equal(slices.Collect(legacy.Objects()), slices.Collect(modern.Objects())) {
		t.Fatal("Objects of the version 1 index differ from version 2")
	}
}

func TestVersionOneIndexKeepsOffsetsPastTwoGigabytes(t *testing.T) {
	id := idOfByte(7)
	far := int64(largeOffsetFlag) + 5
	index := indexOf(t, encodeIndexVersionOne([]Entry{{ID: id, Offset: far}}, hash.Zero))

	if offset, ok, err := index.Lookup(id); !ok || err != nil || offset != far {
		t.Fatalf("Lookup = (%d, %v, %v), want %d", offset, ok, err, far)
	}
}

func TestVersionOneIndexRejectsWrongSizes(t *testing.T) {
	raw := encodeIndexVersionOne([]Entry{{ID: idOfByte(1), Offset: 12}, {ID: idOfByte(2), Offset: 40}}, hash.Zero)
	trailer := raw[len(raw)-indexTrailerSize:]
	for _, tc := range []struct {
		name string
		raw  []byte
		want error
	}{
		{"shorterThanTheFanout", raw[:fanoutSize], ErrTruncated},
		{"missingAnEntry", slices.Concat(raw[:fanoutSize+entrySizeOne], trailer), ErrTruncated},
		{"strayBytes", slices.Concat(raw[:len(raw)-indexTrailerSize], []byte{0}, trailer), ErrCorruptIndex},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewIndex(bytes.NewReader(tc.raw), int64(len(tc.raw))); !errors.Is(err, tc.want) {
				t.Fatalf("NewIndex returned %v, want %v", err, tc.want)
			}
		})
	}
}

func TestStoreServesObjectsThroughAVersionOneIndex(t *testing.T) {
	name := fixtureName(t, offsetPack)
	entries, modern := fixtureEntries(t, name)
	dir := t.TempDir()
	writeTemp(t, filepath.Join(dir, name+packSuffix), readFixture(t, fixturePackPath(t, name)))
	writeTemp(t, filepath.Join(dir, name+indexSuffix), encodeIndexVersionOne(entries, modern.PackHash()))
	store := openStore(t, dir)
	reference := openFixtureStore(t)

	for _, entry := range entries {
		kind, data, ok, err := store.Get(entry.ID)
		if !ok || err != nil {
			t.Fatalf("Get(%s) returned (%v, %v)", entry.ID, ok, err)
		}
		wantKind, wantData, _, _ := reference.Get(entry.ID)
		if kind != wantKind || !bytes.Equal(data, wantData) {
			t.Fatalf("Get(%s) differs from the version 2 store", entry.ID)
		}
	}
}
