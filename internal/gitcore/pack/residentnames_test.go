package pack

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
)

var errNameTableRead = errors.New("pack: injected name table failure")

type countingSource struct {
	data      *bytes.Reader
	reads     int
	failBelow int
	failOnce  bool
}

func (c *countingSource) ReadAt(p []byte, offset int64) (int, error) {
	c.reads++
	if c.failOnce && len(p) > c.failBelow {
		c.failOnce = false
		return 0, errNameTableRead
	}
	return c.data.ReadAt(p, offset)
}

func withoutResidentNames(t *testing.T) {
	t.Helper()
	previous := residentNamesLimit
	residentNamesLimit = 0
	t.Cleanup(func() { residentNamesLimit = previous })
}

func indexOverCountingSource(t *testing.T, raw []byte) (*Index, *countingSource) {
	t.Helper()
	source := &countingSource{data: bytes.NewReader(raw)}
	index, err := NewIndex(source, int64(len(raw)))
	if err != nil {
		t.Fatalf("NewIndex returned error %v", err)
	}
	source.reads = 0
	return index, source
}

func residentFixture(t *testing.T) ([]byte, []Entry, *Index) {
	t.Helper()
	entries, modern := fixtureEntries(t, fixtureName(t, offsetPack))
	raw, err := io.ReadAll(io.NewSectionReader(modern.source, 0, modern.size))
	if err != nil {
		t.Fatalf("ReadAll returned error %v", err)
	}
	return raw, entries, modern
}

func TestFindReadsTheNameTableOnceAndThenSearchesInMemory(t *testing.T) {
	raw, entries, _ := residentFixture(t)
	index, source := indexOverCountingSource(t, raw)

	for range 50 {
		for _, entry := range entries {
			position, ok, err := index.Position(entry.ID)
			if err != nil || !ok {
				t.Fatalf("Position(%s) = %d, %v, %v", entry.ID, position, ok, err)
			}
		}
		if _, ok, err := index.Position(hash.ObjectID{0xee}); ok || err != nil {
			t.Fatalf("Position of an unknown object = %v, %v", ok, err)
		}
	}

	if source.reads != 1 {
		t.Fatalf("the index source was read %d times, want only the one that loads the name table", source.reads)
	}
}

func TestResidentNamesGiveTheSameAnswersAsReadingEachName(t *testing.T) {
	raw, entries, _ := residentFixture(t)
	resident, _ := indexOverCountingSource(t, raw)
	withoutResidentNames(t)
	reading, _ := indexOverCountingSource(t, raw)
	probes := []hash.ObjectID{{0x00}, {0xff}, {0xee}}
	for _, entry := range entries {
		probes = append(probes, entry.ID)
	}

	for _, probe := range probes {
		wantPosition, wantOK, wantErr := reading.Position(probe)
		position, ok, err := resident.Position(probe)
		if position != wantPosition || ok != wantOK || !errors.Is(err, wantErr) {
			t.Fatalf("Position(%s) = %d, %v, %v; reading each name gives %d, %v, %v", probe, position, ok, err, wantPosition, wantOK, wantErr)
		}
	}
}

func TestVersionOneIndexKeepsItsNamesResidentToo(t *testing.T) {
	entries, modern := fixtureEntries(t, fixtureName(t, offsetPack))
	raw := encodeIndexVersionOne(entries, modern.PackHash())
	index, source := indexOverCountingSource(t, raw)

	for _, entry := range entries {
		if position, ok, err := index.Position(entry.ID); err != nil || !ok {
			t.Fatalf("Position(%s) = %d, %v, %v", entry.ID, position, ok, err)
		}
	}

	if source.reads != 1 {
		t.Fatalf("the version 1 index was read %d times, want only the one that loads the names", source.reads)
	}
}

func TestIndexFallsBackToReadingEachNameWhenTheTableCannotBeLoaded(t *testing.T) {
	raw, entries, _ := residentFixture(t)
	index, source := indexOverCountingSource(t, raw)
	source.failOnce = true
	source.failBelow = hash.Size

	for _, entry := range entries {
		if _, ok, err := index.Position(entry.ID); err != nil || !ok {
			t.Fatalf("Position(%s) = %v, %v after the table failed to load", entry.ID, ok, err)
		}
	}

	if source.reads <= len(entries) {
		t.Fatalf("the index was read %d times, want a read per probe once the table could not be loaded", source.reads)
	}
}

func TestIndexTooLargeToKeepResidentIsReadNameByName(t *testing.T) {
	raw, entries, _ := residentFixture(t)
	previous := residentNamesLimit
	residentNamesLimit = hash.Size
	t.Cleanup(func() { residentNamesLimit = previous })
	index, source := indexOverCountingSource(t, raw)

	for _, entry := range entries {
		if _, ok, err := index.Position(entry.ID); err != nil || !ok {
			t.Fatalf("Position(%s) = %v, %v", entry.ID, ok, err)
		}
	}

	if source.reads <= len(entries) {
		t.Fatalf("the index was read %d times, want a read per probe above the resident limit", source.reads)
	}
}

func TestAnIndexWithoutObjectsHasNoNamesToKeepResident(t *testing.T) {
	index := indexOf(t, buildIndex(nil, hash.ObjectID{1}))

	if _, ok, err := index.Position(hash.ObjectID{0x01}); ok || err != nil {
		t.Fatalf("Position in an empty index = %v, %v", ok, err)
	}
	if index.residentNames() != nil {
		t.Fatal("an empty index kept a name table")
	}
}
