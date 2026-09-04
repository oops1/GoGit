package index

import (
	"bytes"
	"encoding/binary"
	"errors"
	"slices"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
)

func TestResolveUndoSurvivesEncoding(t *testing.T) {
	raw := readFixture(t, reucV2)
	start := bytes.Index(raw, []byte(extResolveUndo)) + extensionHeader
	idx := loadFixture(t, reucV2)
	encoded := encodeResolveUndo(idx.ResolveUndo)
	if !bytes.Equal(encoded, raw[start:start+len(encoded)]) {
		t.Fatal("the resolve undo extension does not encode back to the bytes it was read from")
	}
}

func TestResolveUndoKeepsMissingStages(t *testing.T) {
	entries := []ResolveUndoEntry{{
		Path:  "a",
		Modes: [3]object.Mode{object.ModeBlob, 0, object.ModeExecutable},
		IDs:   [3]hash.ObjectID{idOfByte(1), {}, idOfByte(3)},
	}}
	parsed, err := parseResolveUndo(encodeResolveUndo(entries))
	if err != nil {
		t.Fatalf("parseResolveUndo returned error %v", err)
	}
	if !slices.Equal(parsed, entries) {
		t.Fatalf("parseResolveUndo returned %+v", parsed)
	}
}

func TestParseResolveUndoRejectsBrokenData(t *testing.T) {
	cases := []struct {
		name string
		data []byte
		want error
	}{
		{name: "path without a terminator", data: []byte("a"), want: ErrTruncated},
		{name: "mode without a terminator", data: []byte("a\x00100644"), want: ErrTruncated},
		{name: "mode is not octal", data: []byte("a\x00zz\x000\x000\x00"), want: ErrMalformed},
		{name: "object id is missing", data: []byte("a\x00100644\x000\x000\x00"), want: ErrTruncated},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := parseResolveUndo(testCase.data)
			if !errors.Is(err, testCase.want) {
				t.Fatalf("parseResolveUndo returned %v, want %v", err, testCase.want)
			}
		})
	}
}

func TestParseResolveUndoAcceptsAnEmptyExtension(t *testing.T) {
	entries, err := parseResolveUndo(nil)
	if err != nil || entries != nil {
		t.Fatalf("parseResolveUndo returned (%v, %v)", entries, err)
	}
}

func TestUntrackedCacheKeepsItsRawBytes(t *testing.T) {
	raw := readFixture(t, untrackedV2)
	start := bytes.Index(raw, []byte(extUntracked)) + extensionHeader
	size := binary.BigEndian.Uint32(raw[start-4:])
	idx := loadFixture(t, untrackedV2)
	if !bytes.Equal(idx.Untracked.Raw, raw[start:start+int(size)]) {
		t.Fatal("the untracked cache did not keep the bytes it was read from")
	}
	if idx.Untracked.InfoExcludeStat.Size == 0 {
		t.Fatal("the stat data of info/exclude was not read")
	}
	if !idx.Untracked.ExcludesFileID.IsZero() {
		t.Fatal("a hash was read for a missing core.excludesfile")
	}
	if idx.Untracked.DirFlags == 0 {
		t.Fatal("the directory flags were not read")
	}
}

func TestParseUntrackedRejectsBrokenData(t *testing.T) {
	full := loadFixture(t, untrackedV2).Untracked.Raw
	cases := []struct {
		name string
		data []byte
		want error
	}{
		{name: "empty", data: nil, want: ErrTruncated},
		{name: "identity longer than the extension", data: []byte{0x7f}, want: ErrTruncated},
		{name: "header cut short", data: full[:40], want: ErrTruncated},
		{name: "per directory name cut short", data: cutAfterHeader(full), want: ErrTruncated},
		{name: "directory count is missing", data: cutAtDirectoryCount(full), want: ErrTruncated},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := parseUntracked(testCase.data)
			if !errors.Is(err, testCase.want) {
				t.Fatalf("parseUntracked returned %v, want %v", err, testCase.want)
			}
		})
	}
}

func untrackedHeaderEnd(data []byte) int {
	identSize, pos, _ := decodeVarint(data)
	return pos + int(identSize) + 2*untrackedStatSize + untrackedFlags + 2*hash.Size
}

func cutAfterHeader(data []byte) []byte {
	return data[:untrackedHeaderEnd(data)]
}

func cutAtDirectoryCount(data []byte) []byte {
	start := untrackedHeaderEnd(data)
	end := bytes.IndexByte(data[start:], 0)
	return data[:start+end+1]
}

func TestParseEndOfEntriesRejectsAWrongSize(t *testing.T) {
	if _, err := parseEndOfEntries(make([]byte, 8)); !errors.Is(err, ErrMalformed) {
		t.Fatalf("parseEndOfEntries returned %v, want %v", err, ErrMalformed)
	}
	parsed, err := parseEndOfEntries(append(binary.BigEndian.AppendUint32(nil, 42), idOfByte(3).Bytes()...))
	if err != nil {
		t.Fatalf("parseEndOfEntries returned error %v", err)
	}
	if parsed.Offset != 42 || parsed.ID != idOfByte(3) {
		t.Fatalf("parseEndOfEntries returned %+v", parsed)
	}
}

func TestParseOffsetTableRejectsARaggedTable(t *testing.T) {
	for _, data := range [][]byte{make([]byte, 3), make([]byte, 9)} {
		if _, err := parseOffsetTable(data); !errors.Is(err, ErrMalformed) {
			t.Fatalf("parseOffsetTable returned %v, want %v", err, ErrMalformed)
		}
	}
}

func TestOffsetTableCoversCountsEveryBlock(t *testing.T) {
	cases := []struct {
		name    string
		blocks  []OffsetBlock
		entries int
		want    bool
	}{
		{name: "matching table", blocks: []OffsetBlock{{Count: 2}, {Count: 3}}, entries: 5, want: true},
		{name: "table with an empty block", blocks: []OffsetBlock{{Count: 0}, {Count: 5}}, entries: 5},
		{name: "table that covers too few entries", blocks: []OffsetBlock{{Count: 2}}, entries: 5},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			table := &OffsetTable{Version: 1, Blocks: testCase.blocks}
			if got := table.covers(testCase.entries); got != testCase.want {
				t.Fatalf("covers = %v, want %v", got, testCase.want)
			}
		})
	}
}

func TestEncodeOffsetTableUsesTheOffsetsOfTheWrittenEntries(t *testing.T) {
	table := &OffsetTable{Version: 1, Blocks: []OffsetBlock{{Count: 2}, {Count: 1}}}
	data := encodeOffsetTable(table, []int{12, 80, 140})
	parsed, err := parseOffsetTable(data)
	if err != nil {
		t.Fatalf("parseOffsetTable returned error %v", err)
	}
	want := []OffsetBlock{{Offset: 12, Count: 2}, {Offset: 140, Count: 1}}
	if !slices.Equal(parsed.Blocks, want) {
		t.Fatalf("the offset table came back as %+v", parsed.Blocks)
	}
}

func TestEnsureExtensionKeepsTheOrderGitUses(t *testing.T) {
	idx := New(Version2)
	idx.extensions = []extension{{signature: extOffsetTable}, {signature: extUntracked}, {signature: extEndOfEntries}}
	idx.ensureExtension(extCacheTree)
	idx.ensureExtension(extResolveUndo)
	idx.ensureExtension("ZZZZ")
	idx.ensureExtension(extCacheTree)
	var got []string
	for _, ext := range idx.extensions {
		got = append(got, ext.signature)
	}
	want := []string{extOffsetTable, extCacheTree, extResolveUndo, extUntracked, "ZZZZ", extEndOfEntries}
	if !slices.Equal(got, want) {
		t.Fatalf("the extensions are ordered %v, want %v", got, want)
	}
}

func TestEnsureExtensionAppendsWhenNothingRanksHigher(t *testing.T) {
	idx := New(Version2)
	idx.ensureExtension(extCacheTree)
	if idx.extensionAt(extCacheTree) != 0 {
		t.Fatalf("the cache tree extension sits at %d", idx.extensionAt(extCacheTree))
	}
	idx.ensureExtension(extEndOfEntries)
	if idx.extensionAt(extEndOfEntries) != 1 {
		t.Fatalf("the end of index entries extension sits at %d", idx.extensionAt(extEndOfEntries))
	}
}

func TestOptionalExtensionFollowsTheCaseOfTheFirstLetter(t *testing.T) {
	if !optionalExtension(extCacheTree) {
		t.Fatal("TREE was reported as required")
	}
	if optionalExtension(extSplitIndex) {
		t.Fatal("link was reported as optional")
	}
}
