package pack

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
)

var errRead = errors.New("source read failed")

type brokenReaderAt struct {
	data []byte
	from int64
	to   int64
}

func (b brokenReaderAt) ReadAt(into []byte, offset int64) (int, error) {
	if offset >= b.from && offset < b.to {
		return 0, errRead
	}
	return bytes.NewReader(b.data).ReadAt(into, offset)
}

func fixtureIndexBytes(t *testing.T) []byte {
	t.Helper()
	return readFixture(t, fixtureIndexPath(t, fixtureName(t, offsetPack)))
}

func indexOf(t *testing.T, raw []byte) *Index {
	t.Helper()
	index, err := NewIndex(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatalf("NewIndex returned error %v", err)
	}
	return index
}

func brokenIndex(t *testing.T, from, to int64) *Index {
	t.Helper()
	raw := fixtureIndexBytes(t)
	index := indexOf(t, raw)
	index.source = brokenReaderAt{data: raw, from: from, to: to}
	return index
}

func TestOpenIndexReadsEveryFixture(t *testing.T) {
	for _, name := range fixtureNames(t) {
		t.Run(name, func(t *testing.T) {
			index := openFixtureIndex(t, name)
			records := verifyRecords(t, name)
			if index.Count() != len(records) {
				t.Fatalf("Count = %d, git counted %d", index.Count(), len(records))
			}
			if err := index.Verify(); err != nil {
				t.Fatalf("Verify returned error %v", err)
			}
			if index.Path() != fixtureIndexPath(t, name) {
				t.Fatalf("Path = %q, want %q", index.Path(), fixtureIndexPath(t, name))
			}
			packfile := openFixturePack(t, name)
			if index.PackHash() != packfile.Checksum() {
				t.Fatalf("PackHash = %s, the packfile trailer is %s", index.PackHash(), packfile.Checksum())
			}
			if index.Checksum() == hash.Zero {
				t.Fatal("Checksum is the zero object name")
			}
		})
	}
}

func TestFindAgreesWithShowIndex(t *testing.T) {
	for _, name := range fixtureNames(t) {
		t.Run(name, func(t *testing.T) {
			index := openFixtureIndex(t, name)
			wanted := showIndexEntries(t, name)
			if len(wanted) != index.Count() {
				t.Fatalf("show-index lists %d objects, the index holds %d", len(wanted), index.Count())
			}
			for id, want := range wanted {
				offset, ok := index.Find(id)
				if !ok {
					t.Fatalf("Find(%s) found nothing", id)
				}
				if offset != want.Offset {
					t.Errorf("Find(%s) = %d, git says %d", id, offset, want.Offset)
				}
				position, ok, err := index.Position(id)
				if err != nil || !ok {
					t.Fatalf("Position(%s) returned (%v, %v)", id, ok, err)
				}
				entry, err := index.EntryAt(position)
				if err != nil {
					t.Fatalf("EntryAt(%d) returned error %v", position, err)
				}
				if entry != want {
					t.Errorf("EntryAt(%d) = %+v, git says %+v", position, entry, want)
				}
			}
		})
	}
}

func TestFindMissesUnknownObject(t *testing.T) {
	index := openFixtureIndex(t, fixtureName(t, offsetPack))
	unknown := idOfByte(0xff)
	if offset, ok := index.Find(unknown); ok {
		t.Fatalf("Find found the unknown object at %d", offset)
	}
	if _, ok := index.Find(hash.Zero); ok {
		t.Fatal("Find found the zero object name")
	}
}

func TestObjectsYieldsSortedNames(t *testing.T) {
	name := fixtureName(t, offsetPack)
	index := openFixtureIndex(t, name)
	ids := slices.Collect(index.Objects())
	if len(ids) != index.Count() {
		t.Fatalf("Objects yielded %d names, want %d", len(ids), index.Count())
	}
	if !slices.IsSortedFunc(ids, func(a, b hash.ObjectID) int { return a.Compare(b) }) {
		t.Fatal("Objects yielded unsorted names")
	}
	for _, record := range verifyRecords(t, name) {
		if !slices.Contains(ids, record.id) {
			t.Fatalf("Objects skipped %s", record.id)
		}
	}
}

func TestObjectsStopsWhenTheCallerStops(t *testing.T) {
	index := openFixtureIndex(t, fixtureName(t, offsetPack))
	seen := 0
	for range index.Objects() {
		seen++
		break
	}
	if seen != 1 {
		t.Fatalf("Objects yielded %d names after a break, want 1", seen)
	}
}

func TestObjectsStopsOnReadError(t *testing.T) {
	index := brokenIndex(t, indexTablesAt, math.MaxInt64)
	if ids := slices.Collect(index.Objects()); len(ids) != 0 {
		t.Fatalf("Objects yielded %d names from a broken index", len(ids))
	}
}

func TestEntryAtRejectsPositionsOutsideTheIndex(t *testing.T) {
	index := openFixtureIndex(t, fixtureName(t, offsetPack))
	for _, position := range []int{-1, index.Count()} {
		if _, err := index.EntryAt(position); !errors.Is(err, ErrOutOfRange) {
			t.Fatalf("EntryAt(%d) returned %v, want %v", position, err, ErrOutOfRange)
		}
	}
}

func TestNewIndexRejectsBrokenFiles(t *testing.T) {
	valid := fixtureIndexBytes(t)
	shortIndex := slices.Clone(valid[:indexTablesAt+indexTrailerSize-1])
	badMagic := slices.Clone(valid)
	badMagic[0] = 'x'
	badVersion := slices.Clone(valid)
	binary.BigEndian.PutUint32(badVersion[4:], 3)
	badFanout := slices.Clone(valid)
	binary.BigEndian.PutUint32(badFanout[indexHeaderSize:], 1<<30)
	hugeCount := slices.Clone(valid)
	for bucket := range fanoutEntries {
		binary.BigEndian.PutUint32(hugeCount[indexHeaderSize+bucket*4:], 1<<20)
	}
	oddLarge := slices.Concat(valid[:len(valid)-indexTrailerSize], []byte{0}, valid[len(valid)-indexTrailerSize:])

	cases := []struct {
		name string
		raw  []byte
		want error
	}{
		{"tooShort", shortIndex, ErrTruncated},
		{"version1", badMagic, ErrUnsupportedIndexVersion},
		{"version3", badVersion, ErrUnsupportedIndexVersion},
		{"unsortedFanout", badFanout, ErrCorruptIndex},
		{"countPastTheFile", hugeCount, ErrTruncated},
		{"oddLargeOffsetTable", oddLarge, ErrCorruptIndex},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewIndex(bytes.NewReader(tc.raw), int64(len(tc.raw))); !errors.Is(err, tc.want) {
				t.Fatalf("NewIndex returned %v, want %v", err, tc.want)
			}
		})
	}
}

func TestNewIndexPropagatesReadErrors(t *testing.T) {
	raw := fixtureIndexBytes(t)
	size := int64(len(raw))
	cases := []struct {
		name string
		from int64
		to   int64
	}{
		{"header", 0, 1},
		{"fanout", indexHeaderSize, indexHeaderSize + 1},
		{"trailer", size - indexTrailerSize, size},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			source := brokenReaderAt{data: raw, from: tc.from, to: tc.to}
			if _, err := NewIndex(source, size); !errors.Is(err, errRead) {
				t.Fatalf("NewIndex returned %v, want %v", err, errRead)
			}
		})
	}
}

func TestIndexLookupPropagatesReadErrors(t *testing.T) {
	index := brokenIndex(t, indexTablesAt, math.MaxInt64)
	id := verifyRecords(t, fixtureName(t, offsetPack))[0].id
	if _, _, err := index.Position(id); !errors.Is(err, errRead) {
		t.Fatalf("Position returned %v, want %v", err, errRead)
	}
	if _, ok := index.Find(id); ok {
		t.Fatal("Find succeeded on a broken index")
	}
	if _, _, err := index.Lookup(id); !errors.Is(err, errRead) {
		t.Fatalf("Lookup returned %v, want %v", err, errRead)
	}
	if err := index.Verify(); !errors.Is(err, errRead) {
		t.Fatalf("Verify returned %v, want %v", err, errRead)
	}
}

func TestIndexReadsEveryTableWhenLookingUpEntries(t *testing.T) {
	sound := indexOf(t, fixtureIndexBytes(t))
	count := int64(sound.Count())
	cases := []struct {
		name string
		from int64
		to   int64
	}{
		{"names", sound.names, sound.names + count*hash.Size},
		{"offsets", sound.offsets, sound.offsets + count*offsetSize},
		{"checksums", sound.crcs, sound.crcs + count*crcSize},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			index := brokenIndex(t, tc.from, tc.to)
			if _, err := index.EntryAt(0); !errors.Is(err, errRead) {
				t.Fatalf("EntryAt returned %v, want %v", err, errRead)
			}
		})
	}
}

func TestIndexLookupReportsOffsetReadErrors(t *testing.T) {
	sound := indexOf(t, fixtureIndexBytes(t))
	index := brokenIndex(t, sound.offsets, sound.offsets+int64(sound.Count())*offsetSize)
	id := verifyRecords(t, fixtureName(t, offsetPack))[0].id
	if _, _, err := index.Lookup(id); !errors.Is(err, errRead) {
		t.Fatalf("Lookup returned %v, want %v", err, errRead)
	}
}

func TestVerifyDetectsDamage(t *testing.T) {
	damaged := fixtureIndexBytes(t)
	damaged[indexTablesAt] ^= 0xff
	if err := indexOf(t, damaged).Verify(); !errors.Is(err, ErrChecksumMismatch) {
		t.Fatalf("Verify returned %v, want %v", err, ErrChecksumMismatch)
	}
}

func TestVerifyReportsReadFailure(t *testing.T) {
	index := brokenIndex(t, 0, 1)
	if err := index.Verify(); !errors.Is(err, errRead) {
		t.Fatalf("Verify returned %v, want %v", err, errRead)
	}
}

func TestVerifyDetectsUnsortedNames(t *testing.T) {
	raw := buildIndexAsIs([]Entry{
		{ID: idOfByte(0x20), Offset: 40},
		{ID: idOfByte(0x10), Offset: 12},
	}, hash.Zero)
	if err := indexOf(t, raw).Verify(); !errors.Is(err, ErrCorruptIndex) {
		t.Fatalf("Verify returned %v, want %v", err, ErrCorruptIndex)
	}
}

func TestVerifyDetectsNamesOutsideTheirFanoutBucket(t *testing.T) {
	raw := buildIndexAsIs([]Entry{{ID: idOfByte(0x10), Offset: 12}}, hash.Zero)
	binary.BigEndian.PutUint32(raw[indexHeaderSize+0x10*4:], 0)
	raw = appendChecksum(raw[:len(raw)-hash.Size])
	if err := indexOf(t, raw).Verify(); !errors.Is(err, ErrCorruptIndex) {
		t.Fatalf("Verify returned %v, want %v", err, ErrCorruptIndex)
	}
}

func TestLargeOffsetsAreRead(t *testing.T) {
	want := int64(1) << 33
	raw := buildIndex([]Entry{
		{ID: idOfByte(0x01), Offset: 12, CRC32: 1},
		{ID: idOfByte(0x02), Offset: want, CRC32: 2},
	}, hash.Zero)
	entry, err := indexOf(t, raw).EntryAt(1)
	if err != nil {
		t.Fatalf("EntryAt returned error %v", err)
	}
	if entry.Offset != want {
		t.Fatalf("EntryAt gave offset %d, want %d", entry.Offset, want)
	}
}

func TestLargeOffsetsAreValidated(t *testing.T) {
	raw := buildIndex([]Entry{{ID: idOfByte(0x01), Offset: 1 << 33}}, hash.Zero)
	offsets := indexTablesAt + hash.Size + crcSize
	missingSlot := slices.Clone(raw)
	binary.BigEndian.PutUint32(missingSlot[offsets:], largeOffsetFlag|7)
	negative := slices.Clone(raw)
	binary.BigEndian.PutUint64(negative[offsets+offsetSize:], 1<<63)

	cases := []struct {
		name string
		raw  []byte
		want error
	}{
		{"slotPastTheTable", missingSlot, ErrCorruptIndex},
		{"negativeOffset", negative, ErrBadOffset},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := indexOf(t, tc.raw).EntryAt(0); !errors.Is(err, tc.want) {
				t.Fatalf("EntryAt returned %v, want %v", err, tc.want)
			}
		})
	}
}

func TestLargeOffsetReadErrorIsReported(t *testing.T) {
	raw := buildIndex([]Entry{{ID: idOfByte(0x01), Offset: 1 << 33}}, hash.Zero)
	index := indexOf(t, raw)
	index.source = brokenReaderAt{data: raw, from: index.larges, to: index.larges + largeOffsetSize}
	if _, err := index.EntryAt(0); !errors.Is(err, errRead) {
		t.Fatalf("EntryAt returned %v, want %v", err, errRead)
	}
}

func TestOpenIndexReportsMissingFile(t *testing.T) {
	if _, err := OpenIndex(filepath.Join(t.TempDir(), "absent.idx")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("OpenIndex returned %v, want %v", err, os.ErrNotExist)
	}
}

func TestOpenIndexReportsBrokenFile(t *testing.T) {
	path := writeTemp(t, filepath.Join(t.TempDir(), "broken.idx"),
		bytes.Repeat([]byte{0}, indexTablesAt+indexTrailerSize))
	if _, err := OpenIndex(path); !errors.Is(err, ErrUnsupportedIndexVersion) {
		t.Fatalf("OpenIndex returned %v, want %v", err, ErrUnsupportedIndexVersion)
	}
}

func TestNewIndexFileRejectsClosedFile(t *testing.T) {
	file, err := os.Open(fixtureIndexPath(t, fixtureName(t, offsetPack)))
	if err != nil {
		t.Fatalf("Open returned error %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("Close returned error %v", err)
	}
	if _, err := NewIndexFile(file); err == nil {
		t.Fatal("NewIndexFile accepted a closed file")
	}
}

func TestIndexWithoutFileClosesQuietly(t *testing.T) {
	if err := indexOf(t, fixtureIndexBytes(t)).Close(); err != nil {
		t.Fatalf("Close returned error %v", err)
	}
}

func idOfByte(value byte) hash.ObjectID {
	var id hash.ObjectID
	id[0] = value
	id[hash.Size-1] = value
	return id
}

func TestNewIndexReportsShortReads(t *testing.T) {
	raw := fixtureIndexBytes(t)
	short := bytes.NewReader(raw[:len(raw)-5])
	if _, err := NewIndex(short, int64(len(raw))); !errors.Is(err, ErrTruncated) {
		t.Fatalf("NewIndex returned %v, want %v", err, ErrTruncated)
	}
}
