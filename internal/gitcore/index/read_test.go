package index

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
)

func TestReadReturnsEntriesOfVersionTwoFixture(t *testing.T) {
	idx := loadFixture(t, basicV2)
	if idx.Version != Version2 {
		t.Fatalf("Version = %d, want %d", idx.Version, Version2)
	}
	want := []string{"a.txt", "b.txt", "lib/deep/note.txt", "lib/library.txt"}
	if got := paths(idx); !slices.Equal(got, want) {
		t.Fatalf("paths = %v, want %v", got, want)
	}
	first := idx.At(0)
	if first.Mode != object.ModeBlob {
		t.Fatalf("mode of %q = %s, want %s", first.Path, first.Mode, object.ModeBlob)
	}
	if first.ID != mustParseID(t, "4a58007052a65fbc2fc3f910f2855f45a4058e74") {
		t.Fatalf("id of %q = %s", first.Path, first.ID)
	}
	if first.Stat.Size != 6 {
		t.Fatalf("size of %q = %d, want 6", first.Path, first.Stat.Size)
	}
	if first.Stat.MTime.Unix() == 0 {
		t.Fatalf("mtime of %q was not read", first.Path)
	}
}

func TestReadReturnsExtendedFlagsOfVersionThreeFixture(t *testing.T) {
	idx := loadFixture(t, flagsV3)
	if idx.Version != Version3 {
		t.Fatalf("Version = %d, want %d", idx.Version, Version3)
	}
	cases := []struct {
		path         string
		assumeValid  bool
		skipWorktree bool
		intentToAdd  bool
	}{
		{path: "a.txt"},
		{path: "b.txt", skipWorktree: true},
		{path: "c.txt", intentToAdd: true},
		{path: "keep.txt", assumeValid: true},
	}
	for _, want := range cases {
		entry, ok := idx.Get(want.path, StageMerged)
		if !ok {
			t.Fatalf("Get(%q) found nothing", want.path)
		}
		if entry.AssumeValid != want.assumeValid ||
			entry.SkipWorktree != want.skipWorktree ||
			entry.IntentToAdd != want.intentToAdd {
			t.Fatalf("flags of %q = (%v, %v, %v), want (%v, %v, %v)", want.path,
				entry.AssumeValid, entry.SkipWorktree, entry.IntentToAdd,
				want.assumeValid, want.skipWorktree, want.intentToAdd)
		}
	}
}

func TestReadExpandsPrefixCompressedNamesOfVersionFour(t *testing.T) {
	idx := loadFixture(t, prefixV4)
	if idx.Version != Version4 {
		t.Fatalf("Version = %d, want %d", idx.Version, Version4)
	}
	want := []string{"a.txt", "ab.txt", "b.txt", "lib/deep/note.txt", "lib/library.txt", "lib/library2.txt"}
	if got := paths(idx); !slices.Equal(got, want) {
		t.Fatalf("paths = %v, want %v", got, want)
	}
}

func TestReadKeepsNamesLongerThanTheFlagMask(t *testing.T) {
	for _, name := range []string{longNameV2, longNameV4} {
		idx := loadFixture(t, name)
		longest := idx.At(idx.Len() - 1)
		if len(longest.Path) != 5000 {
			t.Fatalf("%s: the longest name holds %d bytes, want 5000", name, len(longest.Path))
		}
		if strings.Trim(longest.Path, "n") != "" {
			t.Fatalf("%s: the longest name is not the expected filler", name)
		}
	}
}

func TestReadReturnsModesOfSymlinksAndSubmodules(t *testing.T) {
	idx := loadFixture(t, longNameV2)
	cases := map[string]object.Mode{
		"exec.sh": object.ModeExecutable,
		"link":    object.ModeSymlink,
		"module":  object.ModeSubmodule,
	}
	for path, want := range cases {
		entry, ok := idx.Get(path, StageMerged)
		if !ok {
			t.Fatalf("Get(%q) found nothing", path)
		}
		if entry.Mode != want {
			t.Fatalf("mode of %q = %s, want %s", path, entry.Mode, want)
		}
	}
}

func TestReadReturnsConflictStages(t *testing.T) {
	idx := loadFixture(t, conflictV2)
	conflicts := idx.Conflicts("conflict.txt")
	if len(conflicts) != 3 {
		t.Fatalf("Conflicts returned %d entries, want 3", len(conflicts))
	}
	for at, entry := range conflicts {
		if entry.Stage != Stage(at+1) {
			t.Fatalf("stage %d of the conflict = %d", at, entry.Stage)
		}
	}
	if !idx.HasConflicts() {
		t.Fatal("HasConflicts returned false for a conflicted index")
	}
	if got := idx.Conflicts("keep.txt"); got != nil {
		t.Fatalf("Conflicts(\"keep.txt\") = %v, want nil", got)
	}
}

func TestReadReturnsCacheTree(t *testing.T) {
	idx := loadFixture(t, basicV2)
	if idx.CacheTree == nil {
		t.Fatal("the cache tree was not read")
	}
	if !idx.CacheTree.Valid() || idx.CacheTree.EntryCount != 4 {
		t.Fatalf("the root covers %d entries", idx.CacheTree.EntryCount)
	}
	deep := idx.CacheTree.Lookup("lib/deep")
	if deep == nil {
		t.Fatal("Lookup(\"lib/deep\") found nothing")
	}
	if deep.EntryCount != 1 || len(deep.Subtrees) != 0 {
		t.Fatalf("lib/deep covers %d entries and %d subtrees", deep.EntryCount, len(deep.Subtrees))
	}
	if idx.CacheTree.Lookup("lib/missing") != nil {
		t.Fatal("Lookup found a subtree that does not exist")
	}
}

func TestReadReturnsInvalidCacheTreeRoot(t *testing.T) {
	idx := loadFixture(t, flagsV3)
	if idx.CacheTree.Valid() {
		t.Fatal("the cache tree root is valid after an intent-to-add entry was staged")
	}
	if !idx.CacheTree.ID.IsZero() {
		t.Fatalf("an invalid cache tree carries the object id %s", idx.CacheTree.ID)
	}
}

func TestReadReturnsResolveUndoEntries(t *testing.T) {
	idx := loadFixture(t, reucV2)
	if len(idx.ResolveUndo) != 1 {
		t.Fatalf("the resolve undo extension holds %d entries, want 1", len(idx.ResolveUndo))
	}
	undo := idx.ResolveUndo[0]
	if undo.Path != "conflict.txt" {
		t.Fatalf("the resolve undo entry names %q", undo.Path)
	}
	for stage, mode := range undo.Modes {
		if mode != object.ModeBlob {
			t.Fatalf("resolve undo mode %d = %s", stage, mode)
		}
		if undo.IDs[stage].IsZero() {
			t.Fatalf("resolve undo object id %d is empty", stage)
		}
	}
}

func TestReadReturnsUntrackedCache(t *testing.T) {
	idx := loadFixture(t, untrackedV2)
	if idx.Untracked == nil {
		t.Fatal("the untracked cache was not read")
	}
	if idx.Untracked.ExcludePerDir != ".gitignore" {
		t.Fatalf("the per directory exclude file is %q", idx.Untracked.ExcludePerDir)
	}
	if len(idx.Untracked.Ident) != 1 || !strings.HasPrefix(idx.Untracked.Ident[0], "Location ") {
		t.Fatalf("the untracked cache identity is %v", idx.Untracked.Ident)
	}
	if idx.Untracked.InfoExcludeID.IsZero() {
		t.Fatal("the hash of info/exclude is empty")
	}
	if idx.Untracked.Directories == 0 {
		t.Fatal("the untracked cache holds no directory blocks")
	}
}

func TestReadReturnsOffsetTableAndEndOfEntries(t *testing.T) {
	idx := loadFixture(t, offsetsV2)
	if idx.OffsetTable == nil || len(idx.OffsetTable.Blocks) != 2 {
		t.Fatalf("the offset table is %+v", idx.OffsetTable)
	}
	if idx.OffsetTable.Version != 1 {
		t.Fatalf("the offset table announces version %d", idx.OffsetTable.Version)
	}
	if idx.OffsetTable.Blocks[0].Offset != headerSize {
		t.Fatalf("the first block starts at %d", idx.OffsetTable.Blocks[0].Offset)
	}
	total := uint32(0)
	for _, block := range idx.OffsetTable.Blocks {
		total += block.Count
	}
	if total != uint32(idx.Len()) {
		t.Fatalf("the offset table covers %d of %d entries", total, idx.Len())
	}
	if idx.EndOfEntries == nil {
		t.Fatal("the end of index entries extension was not read")
	}
	raw := readFixture(t, offsetsV2)
	if got := bytes.Index(raw, []byte(extOffsetTable)); uint32(got) != idx.EndOfEntries.Offset {
		t.Fatalf("the end of index entries points at %d, the extensions start at %d", idx.EndOfEntries.Offset, got)
	}
	if idx.EndOfEntries.ID.IsZero() {
		t.Fatal("the end of index entries extension carries no hash")
	}
}

func TestReadRejectsSplitIndex(t *testing.T) {
	_, err := Read(bytes.NewReader(readFixture(t, splitV2)))
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("Read returned %v, want %v", err, ErrUnsupported)
	}
	if !strings.Contains(err.Error(), "shared index") {
		t.Fatalf("Read returned %v, which does not explain the split index", err)
	}
}

func TestReadRejectsShortFile(t *testing.T) {
	_, err := Read(bytes.NewReader([]byte("DIRC")))
	if !errors.Is(err, ErrTruncated) {
		t.Fatalf("Read returned %v, want %v", err, ErrTruncated)
	}
}

func TestReadRejectsForeignSignature(t *testing.T) {
	data := bytes.Clone(readFixture(t, basicV2))
	copy(data, "DIRD")
	_, err := Read(bytes.NewReader(rewriteChecksum(data)))
	if !errors.Is(err, ErrBadSignature) {
		t.Fatalf("Read returned %v, want %v", err, ErrBadSignature)
	}
}

func TestReadRejectsUnknownVersion(t *testing.T) {
	for _, version := range []uint32{0, 1, 5, 1 << 20} {
		data := bytes.Clone(readFixture(t, basicV2))
		binary.BigEndian.PutUint32(data[4:], version)
		_, err := Read(bytes.NewReader(rewriteChecksum(data)))
		if !errors.Is(err, ErrUnsupportedVersion) {
			t.Fatalf("version %d: Read returned %v, want %v", version, err, ErrUnsupportedVersion)
		}
	}
}

func TestReadRejectsBrokenChecksum(t *testing.T) {
	data := bytes.Clone(readFixture(t, basicV2))
	data[len(data)-1] ^= 0xff
	_, err := Read(bytes.NewReader(data))
	if !errors.Is(err, ErrChecksum) {
		t.Fatalf("Read returned %v, want %v", err, ErrChecksum)
	}
}

func TestReadAcceptsIndexWrittenWithoutChecksum(t *testing.T) {
	data := bytes.Clone(readFixture(t, basicV2))
	clear(data[len(data)-hash.Size:])
	idx, err := Read(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("Read returned error %v", err)
	}
	if !idx.SkipHash {
		t.Fatal("SkipHash is false although the trailer is empty")
	}
	if got := encodeIndex(t, idx, 0); !bytes.Equal(got, data) {
		t.Fatal("an index without a checksum does not round trip")
	}
}

func TestReadRejectsImpossibleEntryCount(t *testing.T) {
	data := bytes.Clone(readFixture(t, basicV2))
	binary.BigEndian.PutUint32(data[8:], 1<<20)
	_, err := Read(bytes.NewReader(rewriteChecksum(data)))
	if !errors.Is(err, ErrTruncated) {
		t.Fatalf("Read returned %v, want %v", err, ErrTruncated)
	}
}

func TestReadRejectsTruncatedEntry(t *testing.T) {
	data := readFixture(t, basicV2)
	_, err := Read(bytes.NewReader(rewriteChecksum(data[:headerSize+40])))
	if !errors.Is(err, ErrTruncated) {
		t.Fatalf("Read returned %v, want %v", err, ErrTruncated)
	}
}

func TestReadRejectsAnEntryThatStartsTooLateInTheFile(t *testing.T) {
	name := strings.Repeat("n", 40)
	entries := []Entry{
		blobEntry(name+"1", StageMerged),
		blobEntry(name+"2", StageMerged),
		blobEntry(name+"3", StageMerged),
		blobEntry(name+"4", StageMerged),
	}
	data := buildIndex(t, Version2, entries...)
	stride := (baseEntrySize + len(name) + 1 + entryAlignment) &^ (entryAlignment - 1)
	cut := headerSize + 3*stride + 30
	_, err := Read(bytes.NewReader(rewriteChecksum(append(bytes.Clone(data[:cut]), make([]byte, hash.Size)...))))
	if !errors.Is(err, ErrTruncated) {
		t.Fatalf("Read returned %v, want %v", err, ErrTruncated)
	}
}

func TestReadRejectsAnEntryWhosePaddingLeavesTheFile(t *testing.T) {
	data := buildIndex(t, Version2, blobEntry("ab", StageMerged))
	cut := headerSize + baseEntrySize + len("ab\x00")
	_, err := Read(bytes.NewReader(rewriteChecksum(append(bytes.Clone(data[:cut]), make([]byte, hash.Size)...))))
	if !errors.Is(err, ErrTruncated) {
		t.Fatalf("Read returned %v, want %v", err, ErrTruncated)
	}
}

func TestReadRejectsEntryWithoutRoomForExtendedFlags(t *testing.T) {
	data := buildIndex(t, Version3, Entry{Path: "a", Mode: object.ModeBlob, SkipWorktree: true})
	cut := headerSize + baseEntrySize + 1
	_, err := Read(bytes.NewReader(rewriteChecksum(data[:cut+hash.Size])))
	if !errors.Is(err, ErrTruncated) {
		t.Fatalf("Read returned %v, want %v", err, ErrTruncated)
	}
}

func TestReadRejectsUnknownEntryFlags(t *testing.T) {
	data := bytes.Clone(buildIndex(t, Version3, Entry{Path: "a", Mode: object.ModeBlob, SkipWorktree: true}))
	binary.BigEndian.PutUint16(data[headerSize+baseEntrySize:], 0x0001)
	_, err := Read(bytes.NewReader(rewriteChecksum(data)))
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("Read returned %v, want %v", err, ErrUnsupported)
	}
}

func TestReadRejectsEntryNameWithoutTerminator(t *testing.T) {
	data := bytes.Clone(buildIndex(t, Version2, blobEntry("abcdefg", StageMerged)))
	start := headerSize + baseEntrySize
	for at := start; at < len(data)-hash.Size; at++ {
		data[at] = 'x'
	}
	_, err := Read(bytes.NewReader(rewriteChecksum(data)))
	if !errors.Is(err, ErrTruncated) {
		t.Fatalf("Read returned %v, want %v", err, ErrTruncated)
	}
}

func TestReadRejectsEntryNameThatOverflowsTheRecord(t *testing.T) {
	data := bytes.Clone(buildIndex(t, Version2, blobEntry("a", StageMerged)))
	binary.BigEndian.PutUint16(data[headerSize+statSize+hash.Size:], 200)
	_, err := Read(bytes.NewReader(rewriteChecksum(data)))
	if !errors.Is(err, ErrTruncated) {
		t.Fatalf("Read returned %v, want %v", err, ErrTruncated)
	}
}

func TestReadRejectsLongNameWithoutTerminator(t *testing.T) {
	data := bytes.Clone(buildIndex(t, Version2, blobEntry(strings.Repeat("n", 5000), StageMerged)))
	body := data[:len(data)-hash.Size]
	for at := headerSize + baseEntrySize; at < len(body); at++ {
		body[at] = 'n'
	}
	_, err := Read(bytes.NewReader(rewriteChecksum(data)))
	if !errors.Is(err, ErrTruncated) {
		t.Fatalf("Read returned %v, want %v", err, ErrTruncated)
	}
}

func TestReadRejectsVersionFourPrefixLongerThanThePreviousName(t *testing.T) {
	data := bytes.Clone(buildIndex(t, Version4, blobEntry("a", StageMerged), blobEntry("ab", StageMerged)))
	data[headerSize+baseEntrySize+len("a\x00")+1+baseEntrySize] = 9
	_, err := Read(bytes.NewReader(rewriteChecksum(data)))
	if !errors.Is(err, ErrMalformed) {
		t.Fatalf("Read returned %v, want %v", err, ErrMalformed)
	}
}

func TestReadRejectsVersionFourNameWithoutTerminator(t *testing.T) {
	data := bytes.Clone(buildIndex(t, Version4, blobEntry("a", StageMerged)))
	body := data[:len(data)-hash.Size]
	for at := headerSize + baseEntrySize + 1; at < len(body); at++ {
		body[at] = 'x'
	}
	_, err := Read(bytes.NewReader(rewriteChecksum(data)))
	if !errors.Is(err, ErrTruncated) {
		t.Fatalf("Read returned %v, want %v", err, ErrTruncated)
	}
}

func TestReadRejectsVersionFourNameLengthMismatch(t *testing.T) {
	data := bytes.Clone(buildIndex(t, Version4, blobEntry("abc", StageMerged)))
	binary.BigEndian.PutUint16(data[headerSize+statSize+hash.Size:], 2)
	_, err := Read(bytes.NewReader(rewriteChecksum(data)))
	if !errors.Is(err, ErrMalformed) {
		t.Fatalf("Read returned %v, want %v", err, ErrMalformed)
	}
}

func TestReadRejectsVersionFourEntryWithoutPrefixNumber(t *testing.T) {
	data := buildIndex(t, Version4, blobEntry("abc", StageMerged))
	cut := headerSize + baseEntrySize
	_, err := Read(bytes.NewReader(rewriteChecksum(data[:cut+hash.Size])))
	if !errors.Is(err, ErrTruncated) {
		t.Fatalf("Read returned %v, want %v", err, ErrTruncated)
	}
}

func TestReadRejectsUnsortedEntries(t *testing.T) {
	cases := []struct {
		name    string
		entries []Entry
		swap    bool
	}{
		{name: "descending paths", entries: []Entry{blobEntry("a", StageMerged), blobEntry("b", StageMerged)}, swap: true},
		{name: "descending stages", entries: []Entry{
			blobEntry("a", StageOurs), blobEntry("a", StageTheirs),
		}, swap: true},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			idx := New(Version2)
			for _, entry := range testCase.entries {
				idx.Add(entry)
			}
			idx.entries[0], idx.entries[1] = idx.entries[1], idx.entries[0]
			_, err := Read(bytes.NewReader(encodeIndex(t, idx, Version2)))
			if !errors.Is(err, ErrUnsorted) {
				t.Fatalf("Read returned %v, want %v", err, ErrUnsorted)
			}
		})
	}
}

func TestReadRejectsDuplicateEntries(t *testing.T) {
	idx := New(Version2)
	idx.Add(blobEntry("a", StageMerged))
	idx.entries = append(idx.entries, idx.entries[0])
	_, err := Read(bytes.NewReader(encodeIndex(t, idx, Version2)))
	if !errors.Is(err, ErrUnsorted) {
		t.Fatalf("Read returned %v, want %v", err, ErrUnsorted)
	}
}

func TestReadRejectsMergedEntryBesideStagedEntry(t *testing.T) {
	idx := New(Version2)
	idx.Add(blobEntry("a", StageMerged))
	idx.Add(blobEntry("a", StageOurs))
	_, err := Read(bytes.NewReader(encodeIndex(t, idx, Version2)))
	if !errors.Is(err, ErrUnsorted) {
		t.Fatalf("Read returned %v, want %v", err, ErrUnsorted)
	}
}

func TestReadRejectsTruncatedExtensionHeader(t *testing.T) {
	data := readFixture(t, basicV2)
	body := data[:len(data)-hash.Size]
	cut := bytes.Index(body, []byte(extCacheTree)) + 4
	_, err := Read(bytes.NewReader(rewriteChecksum(append(bytes.Clone(body[:cut]), make([]byte, hash.Size)...))))
	if !errors.Is(err, ErrTruncated) {
		t.Fatalf("Read returned %v, want %v", err, ErrTruncated)
	}
}

func TestReadRejectsExtensionLongerThanTheFile(t *testing.T) {
	data := bytes.Clone(readFixture(t, basicV2))
	at := bytes.Index(data, []byte(extCacheTree))
	binary.BigEndian.PutUint32(data[at+4:], 1<<20)
	_, err := Read(bytes.NewReader(rewriteChecksum(data)))
	if !errors.Is(err, ErrTruncated) {
		t.Fatalf("Read returned %v, want %v", err, ErrTruncated)
	}
}

func TestReadKeepsUnknownOptionalExtension(t *testing.T) {
	data := withExtension(t, readFixture(t, basicV2), "ZZZZ", []byte("payload"))
	idx, err := Read(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("Read returned error %v", err)
	}
	if at := idx.extensionAt("ZZZZ"); at < 0 {
		t.Fatal("the unknown extension was dropped")
	}
	if got := encodeIndex(t, idx, 0); !bytes.Equal(got, data) {
		t.Fatal("an unknown optional extension is not written back unchanged")
	}
}

func TestReadRejectsUnknownRequiredExtension(t *testing.T) {
	data := withExtension(t, readFixture(t, basicV2), "zzzz", []byte("payload"))
	_, err := Read(bytes.NewReader(data))
	if !errors.Is(err, ErrUnsupportedExtension) {
		t.Fatalf("Read returned %v, want %v", err, ErrUnsupportedExtension)
	}
}

func withExtension(tb testing.TB, data []byte, name string, payload []byte) []byte {
	tb.Helper()
	body := bytes.Clone(data[:len(data)-hash.Size])
	body = append(body, name...)
	body = binary.BigEndian.AppendUint32(body, uint32(len(payload)))
	body = append(body, payload...)
	return rewriteChecksum(append(body, make([]byte, hash.Size)...))
}

func TestReadPropagatesReaderFailure(t *testing.T) {
	_, err := Read(failingReader{err: errInjected})
	if !errors.Is(err, errInjected) {
		t.Fatalf("Read returned %v, want %v", err, errInjected)
	}
}

func TestReadFileReturnsIndexWithTimestamp(t *testing.T) {
	path := filepath.Join(t.TempDir(), "index")
	if err := os.WriteFile(path, readFixture(t, basicV2), 0o600); err != nil {
		t.Fatalf("WriteFile returned error %v", err)
	}
	idx, err := ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile returned error %v", err)
	}
	if idx.Timestamp.IsZero() {
		t.Fatal("ReadFile left the timestamp empty")
	}
	if idx.Len() != 4 {
		t.Fatalf("ReadFile returned %d entries, want 4", idx.Len())
	}
}

func TestReadFileFailsWhenTheIndexIsMissing(t *testing.T) {
	_, err := ReadFile(filepath.Join(t.TempDir(), "index"))
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("ReadFile returned %v, want a missing file error", err)
	}
}

func TestReadFileFailsWhenTheContentIsNotAnIndex(t *testing.T) {
	path := filepath.Join(t.TempDir(), "index")
	if err := os.WriteFile(path, bytes.Repeat([]byte("x"), 64), 0o600); err != nil {
		t.Fatalf("WriteFile returned error %v", err)
	}
	_, err := ReadFile(path)
	if !errors.Is(err, ErrBadSignature) {
		t.Fatalf("ReadFile returned %v, want %v", err, ErrBadSignature)
	}
}

func TestReadFileFailsWhenStatFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "index")
	if err := os.WriteFile(path, readFixture(t, basicV2), 0o600); err != nil {
		t.Fatalf("WriteFile returned error %v", err)
	}
	swapStat(t, func(*os.File) (fs.FileInfo, error) { return nil, errInjected })
	_, err := ReadFile(path)
	if !errors.Is(err, errInjected) {
		t.Fatalf("ReadFile returned %v, want %v", err, errInjected)
	}
}
