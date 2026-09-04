package index

import (
	"bytes"
	"crypto/sha1"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
)

const (
	basicV2     = "basic-v2.index"
	flagsV3     = "flags-v3.index"
	prefixV4    = "prefix-v4.index"
	conflictV2  = "conflict-v2.index"
	reucV2      = "reuc-v2.index"
	untrackedV2 = "untracked-v2.index"
	splitV2     = "split-v2.index"
	offsetsV2   = "offsets-v2.index"
	offsetsV4   = "offsets-v4.index"
	longNameV2  = "longname-v2.index"
	longNameV4  = "longname-v4.index"
)

func fixturePath(name string) string {
	return filepath.Join("testdata", name)
}

func readFixture(tb testing.TB, name string) []byte {
	tb.Helper()
	data, err := os.ReadFile(fixturePath(name))
	if err != nil {
		tb.Fatalf("ReadFile(%q) returned error %v", name, err)
	}
	return data
}

func loadFixture(tb testing.TB, name string) *Index {
	tb.Helper()
	idx, err := Read(bytes.NewReader(readFixture(tb, name)))
	if err != nil {
		tb.Fatalf("Read(%q) returned error %v", name, err)
	}
	return idx
}

func fixtureNames() []string {
	return []string{basicV2, flagsV3, prefixV4, conflictV2, reucV2, untrackedV2, offsetsV2, offsetsV4, longNameV2, longNameV4}
}

func mustParseID(tb testing.TB, text string) hash.ObjectID {
	tb.Helper()
	id, err := hash.Parse(text)
	if err != nil {
		tb.Fatalf("Parse(%q) returned error %v", text, err)
	}
	return id
}

func paths(idx *Index) []string {
	var out []string
	for entry := range idx.Entries() {
		out = append(out, entry.Path)
	}
	return out
}

func blobEntry(path string, stage Stage) Entry {
	return Entry{Path: path, Mode: object.ModeBlob, Stage: stage, ID: idOfByte(byte(len(path)))}
}

func idOfByte(value byte) hash.ObjectID {
	var id hash.ObjectID
	for at := range id {
		id[at] = value
	}
	return id
}

type memoryObjects struct {
	stored map[hash.ObjectID][]byte
	fail   error
}

func newMemoryObjects() *memoryObjects {
	return &memoryObjects{stored: make(map[hash.ObjectID][]byte)}
}

func (m *memoryObjects) Put(kind object.Type, data []byte) (hash.ObjectID, error) {
	if m.fail != nil {
		return hash.Zero, m.fail
	}
	id := hash.SumSHA1(kind.String(), data)
	m.stored[id] = bytes.Clone(data)
	return id, nil
}

type failingReader struct {
	err error
}

func (f failingReader) Read([]byte) (int, error) {
	return 0, f.err
}

type failingWriter struct {
	err error
}

func (f failingWriter) Write([]byte) (int, error) {
	return 0, f.err
}

var errInjected = errors.New("injected failure")

func swapFileWrite(t *testing.T, replacement func(*os.File, []byte) (int, error)) {
	t.Helper()
	original := fsWrite
	fsWrite = replacement
	t.Cleanup(func() { fsWrite = original })
}

func swapFileClose(t *testing.T, replacement func(*os.File) error) {
	t.Helper()
	original := fsClose
	fsClose = replacement
	t.Cleanup(func() { fsClose = original })
}

func swapRename(t *testing.T, replacement func(*os.Root, string, string) error) {
	t.Helper()
	original := fsRename
	fsRename = replacement
	t.Cleanup(func() { fsRename = original })
}

func swapCreate(t *testing.T, replacement func(*os.Root, string, int) (*os.File, error)) {
	t.Helper()
	original := fsCreate
	fsCreate = replacement
	t.Cleanup(func() { fsCreate = original })
}

func swapStat(t *testing.T, replacement func(*os.File) (fs.FileInfo, error)) {
	t.Helper()
	original := fsStat
	fsStat = replacement
	t.Cleanup(func() { fsStat = original })
}

type fakeInfo struct {
	size     int64
	mode     fs.FileMode
	modified time.Time
}

func (f fakeInfo) Name() string       { return "fake" }
func (f fakeInfo) Size() int64        { return f.size }
func (f fakeInfo) Mode() fs.FileMode  { return f.mode }
func (f fakeInfo) ModTime() time.Time { return f.modified }
func (f fakeInfo) IsDir() bool        { return f.mode.IsDir() }
func (f fakeInfo) Sys() any           { return nil }

func encodeIndex(tb testing.TB, idx *Index, version int) []byte {
	tb.Helper()
	var buf bytes.Buffer
	if err := idx.Write(&buf, version); err != nil {
		tb.Fatalf("Write returned error %v", err)
	}
	return buf.Bytes()
}

func reread(tb testing.TB, data []byte) *Index {
	tb.Helper()
	idx, err := Read(bytes.NewReader(data))
	if err != nil {
		tb.Fatalf("Read returned error %v", err)
	}
	return idx
}

func buildIndex(tb testing.TB, version int, entries ...Entry) []byte {
	tb.Helper()
	idx := New(version)
	for _, entry := range entries {
		idx.Add(entry)
	}
	return encodeIndex(tb, idx, version)
}

func rewriteChecksum(data []byte) []byte {
	body := bytes.Clone(data[:len(data)-hash.Size])
	sum := sha1.Sum(body)
	return append(body, sum[:]...)
}
