package index

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
)

func TestWriteReproducesEveryFixtureByteForByte(t *testing.T) {
	for _, name := range fixtureNames() {
		t.Run(name, func(t *testing.T) {
			raw := readFixture(t, name)
			idx := loadFixture(t, name)
			if got := encodeIndex(t, idx, idx.Version); !bytes.Equal(got, raw) {
				t.Fatalf("the rewritten index holds %d bytes, the fixture holds %d", len(got), len(raw))
			}
		})
	}
}

func TestWriteKeepsEntriesWhenTheVersionChanges(t *testing.T) {
	cases := []struct {
		fixture string
		version int
	}{
		{fixture: basicV2, version: Version4},
		{fixture: prefixV4, version: Version2},
		{fixture: longNameV2, version: Version4},
		{fixture: longNameV4, version: Version2},
		{fixture: flagsV3, version: Version4},
	}
	for _, testCase := range cases {
		t.Run(testCase.fixture, func(t *testing.T) {
			idx := loadFixture(t, testCase.fixture)
			again := reread(t, encodeIndex(t, idx, testCase.version))
			if !slices.Equal(paths(again), paths(idx)) {
				t.Fatalf("paths after the rewrite = %v", paths(again))
			}
			for at := range idx.Len() {
				if *again.At(at) != *idx.At(at) {
					t.Fatalf("entry %d changed: %+v against %+v", at, again.At(at), idx.At(at))
				}
			}
		})
	}
}

func TestWriteChoosesVersionThreeOnlyForExtendedEntries(t *testing.T) {
	cases := []struct {
		name    string
		entry   Entry
		version int
		want    int
	}{
		{name: "plain entry stays at two", entry: blobEntry("a", StageMerged), version: Version2, want: Version2},
		{name: "plain entry is demoted from three", entry: blobEntry("a", StageMerged), version: Version3, want: Version2},
		{name: "skip worktree lifts two to three", entry: Entry{Path: "a", Mode: object.ModeBlob, SkipWorktree: true}, version: Version2, want: Version3},
		{name: "intent to add lifts two to three", entry: Entry{Path: "a", Mode: object.ModeBlob, IntentToAdd: true}, version: Version2, want: Version3},
		{name: "skip worktree keeps four", entry: Entry{Path: "a", Mode: object.ModeBlob, SkipWorktree: true}, version: Version4, want: Version4},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			idx := New(Version2)
			idx.Add(testCase.entry)
			again := reread(t, encodeIndex(t, idx, testCase.version))
			if again.Version != testCase.want {
				t.Fatalf("the written index announces version %d, want %d", again.Version, testCase.want)
			}
			got := again.At(0)
			if got.Path != testCase.entry.Path || got.SkipWorktree != testCase.entry.SkipWorktree ||
				got.IntentToAdd != testCase.entry.IntentToAdd {
				t.Fatalf("the entry came back as %+v", got)
			}
		})
	}
}

func TestWriteUsesTheStoredVersionWhenNoneIsGiven(t *testing.T) {
	idx := New(Version4)
	idx.Add(blobEntry("a", StageMerged))
	if again := reread(t, encodeIndex(t, idx, 0)); again.Version != Version4 {
		t.Fatalf("the written index announces version %d, want %d", again.Version, Version4)
	}
	empty := New(0)
	empty.Add(blobEntry("a", StageMerged))
	if again := reread(t, encodeIndex(t, empty, 0)); again.Version != Version2 {
		t.Fatalf("the written index announces version %d, want %d", again.Version, Version2)
	}
}

func TestWriteRejectsUnknownVersion(t *testing.T) {
	for _, version := range []int{1, 5, -3} {
		err := New(Version2).Write(&bytes.Buffer{}, version)
		if !errors.Is(err, ErrUnsupportedVersion) {
			t.Fatalf("version %d: Write returned %v, want %v", version, err, ErrUnsupportedVersion)
		}
	}
}

func TestWriteReportsWriterFailure(t *testing.T) {
	err := loadFixture(t, basicV2).Write(failingWriter{err: errInjected}, Version2)
	if !errors.Is(err, errInjected) {
		t.Fatalf("Write returned %v, want %v", err, errInjected)
	}
}

func TestWriteKeepsAllFlagsAndStatData(t *testing.T) {
	entry := Entry{
		Path:  "dir/file.txt",
		Mode:  object.ModeExecutable,
		ID:    idOfByte(7),
		Stage: StageTheirs,
		Stat: Stat{
			CTime: time.Unix(1700000000, 123456789),
			MTime: time.Unix(1700000100, 987654321),
			Dev:   11,
			Ino:   22,
			UID:   33,
			GID:   44,
			Size:  55,
		},
		AssumeValid:  true,
		SkipWorktree: true,
		IntentToAdd:  true,
	}
	idx := New(Version3)
	idx.Add(entry)
	got := reread(t, encodeIndex(t, idx, Version3)).At(0)
	if got.Path != entry.Path || got.Mode != entry.Mode || got.ID != entry.ID || got.Stage != entry.Stage {
		t.Fatalf("the entry came back as %+v", got)
	}
	if !got.Stat.CTime.Equal(entry.Stat.CTime) || !got.Stat.MTime.Equal(entry.Stat.MTime) {
		t.Fatalf("the timestamps came back as %v and %v", got.Stat.CTime, got.Stat.MTime)
	}
	if got.Stat != entry.Stat {
		t.Fatalf("the stat data came back as %+v", got.Stat)
	}
	if !got.AssumeValid || !got.SkipWorktree || !got.IntentToAdd {
		t.Fatalf("the flags came back as (%v, %v, %v)", got.AssumeValid, got.SkipWorktree, got.IntentToAdd)
	}
}

func TestWriteStoresNameLengthMaskForLongNames(t *testing.T) {
	long := strings.Repeat("n", nameMask+7)
	for _, version := range []int{Version2, Version4} {
		idx := New(version)
		idx.Add(blobEntry("a", StageMerged))
		idx.Add(blobEntry(long, StageMerged))
		again := reread(t, encodeIndex(t, idx, version))
		if got := again.At(1).Path; got != long {
			t.Fatalf("version %d: the long name came back with %d bytes", version, len(got))
		}
	}
}

func TestWriteDropsExtensionsWithoutContent(t *testing.T) {
	idx := loadFixture(t, reucV2)
	idx.CacheTree = nil
	idx.ResolveUndo = nil
	idx.Untracked = nil
	idx.EndOfEntries = nil
	idx.OffsetTable = nil
	data := encodeIndex(t, idx, Version2)
	for _, name := range []string{extCacheTree, extResolveUndo, extUntracked, extEndOfEntries, extOffsetTable} {
		if bytes.Contains(data, []byte(name)) {
			t.Fatalf("the extension %s survived although its content was dropped", name)
		}
	}
	if again := reread(t, data); again.Len() != idx.Len() {
		t.Fatalf("the rewritten index holds %d entries", again.Len())
	}
}

func TestWriteDropsTheUntrackedCacheWhenItIsCleared(t *testing.T) {
	idx := loadFixture(t, untrackedV2)
	idx.Untracked = nil
	data := encodeIndex(t, idx, Version2)
	if bytes.Contains(data, []byte(extUntracked)) {
		t.Fatal("the untracked cache survived although its content was dropped")
	}
	if again := reread(t, data); again.Untracked != nil {
		t.Fatal("the untracked cache came back")
	}
}

func TestWriteDropsTheEndOfEntriesExtensionWhenItIsCleared(t *testing.T) {
	idx := loadFixture(t, offsetsV2)
	idx.EndOfEntries = nil
	data := encodeIndex(t, idx, Version2)
	if bytes.Contains(data, []byte(extEndOfEntries)) {
		t.Fatal("the end of index entries extension survived although its content was dropped")
	}
	if again := reread(t, data); again.OffsetTable == nil {
		t.Fatal("the offset table was dropped as well")
	}
}

func TestWriteDropsTheOffsetTableWhenItNoLongerCoversTheEntries(t *testing.T) {
	idx := loadFixture(t, offsetsV2)
	idx.Remove("f1.txt")
	data := encodeIndex(t, idx, Version2)
	if bytes.Contains(data, []byte(extOffsetTable)) {
		t.Fatal("a stale offset table was written")
	}
	again := reread(t, data)
	if again.OffsetTable != nil {
		t.Fatal("the offset table came back")
	}
	if again.EndOfEntries == nil {
		t.Fatal("the end of index entries extension was dropped as well")
	}
}

func TestWriteRefreshesTheEndOfEntriesExtension(t *testing.T) {
	idx := loadFixture(t, offsetsV2)
	idx.Remove("f8.txt")
	again := reread(t, encodeIndex(t, idx, Version2))
	if again.EndOfEntries.Offset == idx.EndOfEntries.Offset {
		t.Fatal("the end of index entries offset did not follow the shorter entry table")
	}
	if again.EndOfEntries.ID == idx.EndOfEntries.ID {
		t.Fatal("the end of index entries hash did not follow the changed extensions")
	}
}

func TestWriteFileStoresTheIndexAtomically(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "index")
	idx := loadFixture(t, basicV2)
	if err := idx.WriteFile(path, Version2); err != nil {
		t.Fatalf("WriteFile returned error %v", err)
	}
	stored, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile returned error %v", err)
	}
	if !bytes.Equal(stored, readFixture(t, basicV2)) {
		t.Fatal("WriteFile stored something other than the fixture")
	}
	if _, err := os.Stat(path + lockSuffix); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("the lock file survived WriteFile: %v", err)
	}
}

func TestWriteFileAcceptsARelativePath(t *testing.T) {
	idx := loadFixture(t, basicV2)
	t.Chdir(t.TempDir())
	if err := idx.WriteFile("index", Version2); err != nil {
		t.Fatalf("WriteFile returned error %v", err)
	}
	if _, err := os.Stat("index"); err != nil {
		t.Fatalf("Stat returned error %v", err)
	}
}

func TestWriteFileFailsWhenTheLockIsHeld(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "index")
	if err := os.WriteFile(path+lockSuffix, nil, 0o600); err != nil {
		t.Fatalf("WriteFile returned error %v", err)
	}
	err := loadFixture(t, basicV2).WriteFile(path, Version2)
	if !errors.Is(err, ErrLocked) {
		t.Fatalf("WriteFile returned %v, want %v", err, ErrLocked)
	}
}

func TestWriteFileFailsWhenTheVersionIsUnknown(t *testing.T) {
	err := New(Version2).WriteFile(filepath.Join(t.TempDir(), "index"), 9)
	if !errors.Is(err, ErrUnsupportedVersion) {
		t.Fatalf("WriteFile returned %v, want %v", err, ErrUnsupportedVersion)
	}
}

func TestWriteFileFailsWhenTheDirectoryIsMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "index")
	err := New(Version2).WriteFile(path, Version2)
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("WriteFile returned %v, want a missing directory error", err)
	}
}

func TestWriteFileFailsWhenTheLockCannotBeCreated(t *testing.T) {
	swapCreate(t, func(*os.Root, string, int) (*os.File, error) { return nil, errInjected })
	err := New(Version2).WriteFile(filepath.Join(t.TempDir(), "index"), Version2)
	if !errors.Is(err, errInjected) {
		t.Fatalf("WriteFile returned %v, want %v", err, errInjected)
	}
}

func TestWriteFileFailsWhenTheLockCannotBeWritten(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "index")
	swapFileWrite(t, func(*os.File, []byte) (int, error) { return 0, errInjected })
	err := New(Version2).WriteFile(path, Version2)
	if !errors.Is(err, errInjected) {
		t.Fatalf("WriteFile returned %v, want %v", err, errInjected)
	}
	if _, err := os.Stat(path + lockSuffix); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("the lock file survived the failed write: %v", err)
	}
}

func TestWriteFileFailsWhenTheLockCannotBeClosed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "index")
	swapFileClose(t, func(file *os.File) error {
		_ = file.Close()
		return errInjected
	})
	err := New(Version2).WriteFile(path, Version2)
	if !errors.Is(err, errInjected) {
		t.Fatalf("WriteFile returned %v, want %v", err, errInjected)
	}
	if _, err := os.Stat(path + lockSuffix); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("the lock file survived the failed close: %v", err)
	}
}

func TestWriteFileFailsWhenTheLockCannotBeRenamed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "index")
	swapRename(t, func(*os.Root, string, string) error { return errInjected })
	err := New(Version2).WriteFile(path, Version2)
	if !errors.Is(err, errInjected) {
		t.Fatalf("WriteFile returned %v, want %v", err, errInjected)
	}
	if _, err := os.Stat(path + lockSuffix); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("the lock file survived the failed rename: %v", err)
	}
}

func TestWriteFileAndReadFileAgree(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "index")
	source := loadFixture(t, prefixV4)
	if err := source.WriteFile(path, Version4); err != nil {
		t.Fatalf("WriteFile returned error %v", err)
	}
	loaded, err := ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile returned error %v", err)
	}
	if !slices.Equal(paths(loaded), paths(source)) {
		t.Fatalf("ReadFile returned %v", paths(loaded))
	}
	if loaded.CacheTree == nil || loaded.CacheTree.ID != source.CacheTree.ID {
		t.Fatal("the cache tree did not survive the round trip through the file system")
	}
}

func TestEncodeWritesAnEmptyIndex(t *testing.T) {
	data := encodeIndex(t, New(Version2), Version2)
	if len(data) != headerSize+hash.Size {
		t.Fatalf("an empty index holds %d bytes", len(data))
	}
	if again := reread(t, data); again.Len() != 0 {
		t.Fatalf("an empty index came back with %d entries", again.Len())
	}
}
