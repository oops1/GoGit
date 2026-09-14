package commitgraph

import (
	"bytes"
	"crypto/sha1"
	"encoding/binary"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
)

func id(first byte, rest byte) hash.ObjectID {
	var out hash.ObjectID
	out[0] = first
	out[hash.Size-1] = rest
	return out
}

func chunkBody(t *testing.T, data []byte, want uint32) []byte {
	t.Helper()
	count := int(data[6])
	for i := range count {
		entry := data[headerSize+i*tocEntrySize:]
		if binary.BigEndian.Uint32(entry) != want {
			continue
		}
		start := binary.BigEndian.Uint64(entry[4:])
		end := binary.BigEndian.Uint64(entry[4+tocEntrySize:])
		return data[start:end]
	}
	return nil
}

func dataRow(t *testing.T, data []byte, position int) (hash.ObjectID, uint32, uint32, uint32, uint32) {
	t.Helper()
	body := chunkBody(t, data, chunkData)
	row := body[position*(hash.Size+dataTail):]
	tree, err := hash.FromBytes(row[:hash.Size])
	if err != nil {
		t.Fatal(err)
	}
	words := row[hash.Size:]
	return tree, binary.BigEndian.Uint32(words), binary.BigEndian.Uint32(words[4:]), binary.BigEndian.Uint32(words[8:]), binary.BigEndian.Uint32(words[12:])
}

func linearHistory() []Commit {
	return []Commit{
		{ID: id(0x30, 3), Tree: id(0xA3, 0), Parents: []hash.ObjectID{id(0x20, 2)}, Time: 300},
		{ID: id(0x10, 1), Tree: id(0xA1, 0), Time: 100},
		{ID: id(0x20, 2), Tree: id(0xA2, 0), Parents: []hash.ObjectID{id(0x10, 1)}, Time: 200},
	}
}

func TestAGraphStartsWithItsHeaderAndEndsWithItsChecksum(t *testing.T) {
	data, err := Encode(hash.SHA1, linearHistory())
	if err != nil {
		t.Fatal(err)
	}

	if got := string(data[:4]); got != "CGPH" {
		t.Fatalf("signature = %q", got)
	}
	if data[4] != version || data[5] != hashVersion || data[6] != 3 || data[7] != 0 {
		t.Fatalf("header = % x", data[:headerSize])
	}
	body := data[:len(data)-hash.Size]
	if sum := sha1.Sum(body); !bytes.Equal(sum[:], data[len(body):]) {
		t.Fatal("the trailer is not the checksum of the file")
	}
	if chunkBody(t, data, chunkEdges) != nil {
		t.Fatal("a history without octopus merges has no edge chunk")
	}
}

func TestTheLookupListsCommitsInHashOrderAndTheFanoutCountsThem(t *testing.T) {
	data, err := Encode(hash.SHA1, linearHistory())
	if err != nil {
		t.Fatal(err)
	}

	ids := chunkBody(t, data, chunkOIDLookup)
	for i, want := range []hash.ObjectID{id(0x10, 1), id(0x20, 2), id(0x30, 3)} {
		if !bytes.Equal(ids[i*hash.Size:(i+1)*hash.Size], want[:]) {
			t.Fatalf("lookup %d = % x", i, ids[i*hash.Size:(i+1)*hash.Size])
		}
	}
	fan := chunkBody(t, data, chunkOIDFanout)
	for first, want := range map[int]uint32{0x0F: 0, 0x10: 1, 0x1F: 1, 0x20: 2, 0x30: 3, 0xFF: 3} {
		if got := binary.BigEndian.Uint32(fan[first*wordSize:]); got != want {
			t.Fatalf("fanout[%#x] = %d, want %d", first, got, want)
		}
	}
}

func TestEachCommitRecordsItsTreeParentsGenerationAndTime(t *testing.T) {
	data, err := Encode(hash.SHA1, linearHistory())
	if err != nil {
		t.Fatal(err)
	}

	tree, first, second, genHigh, timeLow := dataRow(t, data, 0)
	if tree != id(0xA1, 0) || first != noParent || second != noParent || genHigh>>2 != 1 || timeLow != 100 {
		t.Fatalf("root row = %v %#x %#x %#x %d", tree, first, second, genHigh, timeLow)
	}
	_, first, second, genHigh, timeLow = dataRow(t, data, 2)
	if first != 1 || second != noParent || genHigh>>2 != 3 || timeLow != 300 {
		t.Fatalf("tip row = %#x %#x %#x %d", first, second, genHigh, timeLow)
	}
}

func TestADescendantSortedBeforeItsAncestorsStillGetsTheHigherGeneration(t *testing.T) {
	child, parent := id(0x01, 1), id(0x02, 2)

	data, err := Encode(hash.SHA1, []Commit{{ID: child, Parents: []hash.ObjectID{parent}}, {ID: parent}})
	if err != nil {
		t.Fatal(err)
	}

	_, first, _, childGen, _ := dataRow(t, data, 0)
	_, _, _, parentGen, _ := dataRow(t, data, 1)
	if first != 1 || childGen>>2 != 2 || parentGen>>2 != 1 {
		t.Fatalf("child parent = %d, generations = %d over %d", first, childGen>>2, parentGen>>2)
	}
}

func TestAMergeAndAnOctopusSpellTheirParentsTheWayGitDoes(t *testing.T) {
	a, b, c, d := id(0x01, 1), id(0x02, 2), id(0x03, 3), id(0x04, 4)
	merge, octopus := id(0x05, 5), id(0x06, 6)
	commits := []Commit{
		{ID: a}, {ID: b}, {ID: c}, {ID: d},
		{ID: merge, Parents: []hash.ObjectID{a, b}},
		{ID: octopus, Parents: []hash.ObjectID{merge, c, d, a}, Time: 1 << 33},
	}

	data, err := Encode(hash.SHA1, commits)
	if err != nil {
		t.Fatal(err)
	}

	_, first, second, genHigh, _ := dataRow(t, data, 4)
	if first != 0 || second != 1 || genHigh>>2 != 2 {
		t.Fatalf("merge row = %#x %#x %#x", first, second, genHigh)
	}
	_, first, second, genHigh, timeLow := dataRow(t, data, 5)
	if first != 4 || second != octopusFlag || genHigh>>2 != 3 || genHigh&timeHighMask != 2 || timeLow != 0 {
		t.Fatalf("octopus row = %#x %#x %#x %d", first, second, genHigh, timeLow)
	}
	edges := chunkBody(t, data, chunkEdges)
	want := []uint32{2, 3, octopusFlag}
	for i, value := range want {
		if got := binary.BigEndian.Uint32(edges[i*wordSize:]); got != value {
			t.Fatalf("edge %d = %#x, want %#x", i, got, value)
		}
	}
}

func TestEncodingRefusesWhatGitCouldNotRead(t *testing.T) {
	loop := []Commit{
		{ID: id(1, 1), Parents: []hash.ObjectID{id(2, 2)}},
		{ID: id(2, 2), Parents: []hash.ObjectID{id(1, 1)}},
	}
	for _, tt := range []struct {
		name    string
		format  hash.Format
		commits []Commit
		want    error
	}{
		{"a sha256 repository", hash.SHA256, nil, ErrUnsupportedFormat},
		{"a commit listed twice", hash.SHA1, []Commit{{ID: id(1, 1)}, {ID: id(1, 1)}}, ErrDuplicateCommit},
		{"a parent left out", hash.SHA1, []Commit{{ID: id(1, 1), Parents: []hash.ObjectID{id(9, 9)}}}, ErrMissingParent},
		{"commits that are their own ancestors", hash.SHA1, loop, ErrCycle},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Encode(tt.format, tt.commits); !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestAnEmptyGraphIsStillAWellFormedFile(t *testing.T) {
	data, err := Encode(hash.SHA1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunkBody(t, data, chunkOIDFanout)) != fanoutEntries*wordSize || len(chunkBody(t, data, chunkOIDLookup)) != 0 {
		t.Fatalf("empty graph = % x", data)
	}
}

func TestWriteFileReplacesTheGraphInTheInfoDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "objects", "info")
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte("old"), 0o444); err != nil {
		t.Fatal(err)
	}

	if err := WriteFile(dir, hash.SHA1, linearHistory()); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatal(err)
	}
	want, _ := Encode(hash.SHA1, linearHistory())
	if !bytes.Equal(got, want) {
		t.Fatal("the written graph differs from the encoded one")
	}
	if _, err := os.Stat(filepath.Join(dir, FileName+lockSuffix)); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("lock left behind: %v", err)
	}
}

func TestWriteFileCreatesAMissingInfoDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "objects", "info")

	if err := WriteFile(dir, hash.SHA1, linearHistory()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, FileName)); err != nil {
		t.Fatal(err)
	}
}

func TestWriteFileWaitsForNoOneAndReportsAHeldLock(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, FileName+lockSuffix), nil, filePerm); err != nil {
		t.Fatal(err)
	}

	if err := WriteFile(dir, hash.SHA1, linearHistory()); !errors.Is(err, ErrLocked) {
		t.Fatalf("err = %v, want ErrLocked", err)
	}
}

func TestWriteFileReportsAnEncodingError(t *testing.T) {
	if err := WriteFile(t.TempDir(), hash.SHA256, nil); !errors.Is(err, ErrUnsupportedFormat) {
		t.Fatalf("err = %v", err)
	}
}

func swap[T any](t *testing.T, target *T, replacement T) {
	t.Helper()
	prev := *target
	*target = replacement
	t.Cleanup(func() { *target = prev })
}

func TestWriteFileReportsEveryFailingStepAndLeavesNoLock(t *testing.T) {
	boom := errors.New("boom")
	for _, tt := range []struct {
		name string
		fail func(t *testing.T)
	}{
		{"creating the directory", func(t *testing.T) {
			swap(t, &fsMkdirAll, func(string, fs.FileMode) error { return boom })
		}},
		{"opening the directory", func(t *testing.T) {
			swap(t, &fsOpenRoot, func(string) (*os.Root, error) { return nil, boom })
		}},
		{"creating the lock", func(t *testing.T) {
			swap(t, &fsCreate, func(*os.Root, string) (*os.File, error) { return nil, boom })
		}},
		{"writing the lock", func(t *testing.T) {
			swap(t, &fsWrite, func(*os.File, []byte) (int, error) { return 0, boom })
		}},
		{"closing the lock", func(t *testing.T) {
			swap(t, &fsClose, func(file *os.File) error { _ = file.Close(); return boom })
		}},
		{"renaming the lock", func(t *testing.T) {
			swap(t, &fsRename, func(*os.Root, string, string) error { return boom })
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			tt.fail(t)

			if err := WriteFile(dir, hash.SHA1, linearHistory()); !errors.Is(err, boom) {
				t.Fatalf("err = %v, want boom", err)
			}
			if _, err := os.Stat(filepath.Join(dir, FileName+lockSuffix)); !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("lock left behind: %v", err)
			}
		})
	}
}
