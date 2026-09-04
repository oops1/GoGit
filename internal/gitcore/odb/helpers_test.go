package odb

import (
	"bytes"
	"compress/zlib"
	"crypto/sha1"
	"encoding/binary"
	"hash/crc32"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
)

const (
	packFixtureDir   = "../pack/testdata/packs"
	packObjectsTable = "../pack/testdata/objects.tsv"
	objectFixtureDir = "../object/testdata/objects"
	objectManifest   = "../object/testdata/manifest.tsv"
)

type fixtureObject struct {
	id   hash.ObjectID
	kind object.Type
	size int64
}

func newObjectsDir(t testing.TB) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "objects")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) returned error %v", dir, err)
	}
	return dir
}

func openDB(t testing.TB, dir string, opts Options) *DB {
	t.Helper()
	db, err := Open(dir, opts)
	if err != nil {
		t.Fatalf("Open(%q) returned error %v", dir, err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func readFile(t testing.TB, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) returned error %v", path, err)
	}
	return data
}

func writeFile(t testing.TB, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) returned error %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("WriteFile(%q) returned error %v", path, err)
	}
}

func copyFixturePacks(t testing.TB, objects string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(packFixtureDir, "*"))
	if err != nil {
		t.Fatalf("Glob returned error %v", err)
	}
	if len(matches) == 0 {
		t.Fatalf("no packfile fixtures in %s", packFixtureDir)
	}
	for _, match := range matches {
		writeFile(t, filepath.Join(objects, packDirName, filepath.Base(match)), readFile(t, match))
	}
}

func copyFixtureLoose(t testing.TB, objects string) {
	t.Helper()
	entries, err := os.ReadDir(objectFixtureDir)
	if err != nil {
		t.Fatalf("ReadDir(%q) returned error %v", objectFixtureDir, err)
	}
	for _, entry := range entries {
		names, err := os.ReadDir(filepath.Join(objectFixtureDir, entry.Name()))
		if err != nil {
			t.Fatalf("ReadDir returned error %v", err)
		}
		for _, name := range names {
			source := filepath.Join(objectFixtureDir, entry.Name(), name.Name())
			writeFile(t, filepath.Join(objects, entry.Name(), name.Name()), readFile(t, source))
		}
	}
}

func packFixtureObjects(t testing.TB) []fixtureObject {
	t.Helper()
	var all []fixtureObject
	for _, line := range fixtureLines(t, packObjectsTable) {
		fields := strings.Split(line, "\t")
		if len(fields) != 3 {
			t.Fatalf("objects table line %q holds %d fields, want 3", line, len(fields))
		}
		kind, err := object.ParseType(fields[1])
		if err != nil {
			t.Fatalf("ParseType(%q) returned error %v", fields[1], err)
		}
		size, err := strconv.ParseInt(fields[2], 10, 64)
		if err != nil {
			t.Fatalf("ParseInt(%q) returned error %v", fields[2], err)
		}
		all = append(all, fixtureObject{id: parseID(t, fields[0]), kind: kind, size: size})
	}
	if len(all) == 0 {
		t.Fatal("the packed objects table is empty")
	}
	return all
}

func looseFixtureObjects(t testing.TB) []fixtureObject {
	t.Helper()
	var all []fixtureObject
	for _, line := range fixtureLines(t, objectManifest) {
		fields := strings.Split(line, "\t")
		if len(fields) != 3 {
			t.Fatalf("manifest line %q holds %d fields, want 3", line, len(fields))
		}
		kind, err := object.ParseType(fields[2])
		if err != nil {
			t.Fatalf("ParseType(%q) returned error %v", fields[2], err)
		}
		all = append(all, fixtureObject{id: parseID(t, fields[1]), kind: kind})
	}
	if len(all) == 0 {
		t.Fatal("the loose object manifest is empty")
	}
	return all
}

func namedLooseFixture(t testing.TB, name string) fixtureObject {
	t.Helper()
	for _, line := range fixtureLines(t, objectManifest) {
		fields := strings.Split(line, "\t")
		if fields[0] != name {
			continue
		}
		kind, err := object.ParseType(fields[2])
		if err != nil {
			t.Fatalf("ParseType(%q) returned error %v", fields[2], err)
		}
		return fixtureObject{id: parseID(t, fields[1]), kind: kind}
	}
	t.Fatalf("loose fixture %q is missing", name)
	return fixtureObject{}
}

func fixtureLines(t testing.TB, path string) []string {
	t.Helper()
	text := strings.ReplaceAll(string(readFile(t, path)), "\r\n", "\n")
	return strings.Split(strings.TrimSuffix(text, "\n"), "\n")
}

func parseID(t testing.TB, text string) hash.ObjectID {
	t.Helper()
	id, err := hash.Parse(text)
	if err != nil {
		t.Fatalf("Parse(%q) returned error %v", text, err)
	}
	return id
}

func writeLooseFile(t testing.TB, objects string, id hash.ObjectID, payload []byte) {
	t.Helper()
	writeFile(t, filepath.Join(objects, looseName(id)), payload)
}

func deflate(t testing.TB, data []byte) []byte {
	t.Helper()
	var out bytes.Buffer
	compressor := zlib.NewWriter(&out)
	if _, err := compressor.Write(data); err != nil {
		t.Fatalf("Write returned error %v", err)
	}
	if err := compressor.Close(); err != nil {
		t.Fatalf("Close returned error %v", err)
	}
	return out.Bytes()
}

func looseBytes(t testing.TB, kind object.Type, data []byte) []byte {
	t.Helper()
	return deflate(t, slices.Concat(hash.Header(kind.String(), int64(len(data))), data))
}

type thinPack struct {
	body    []byte
	offsets []int64
	sums    []uint32
}

func newThinPack() *thinPack {
	return &thinPack{body: slices.Concat([]byte("PACK"), binary.BigEndian.AppendUint32(nil, 2), make([]byte, 4))}
}

func (p *thinPack) addRefDelta(t testing.TB, base hash.ObjectID, delta []byte) {
	t.Helper()
	offset := int64(len(p.body))
	entry := slices.Concat(packObjectHeader(7, int64(len(delta))), base[:], deflate(t, delta))
	p.body = append(p.body, entry...)
	p.offsets = append(p.offsets, offset)
	p.sums = append(p.sums, crc32.ChecksumIEEE(entry))
}

func (p *thinPack) at(offsets ...int64) *thinPack {
	p.offsets = offsets
	return p
}

func (p *thinPack) pair(t testing.TB, ids []hash.ObjectID) ([]byte, []byte) {
	t.Helper()
	raw := slices.Clone(p.body)
	binary.BigEndian.PutUint32(raw[8:12], uint32(len(p.offsets)))
	sum := sha1.Sum(raw)
	raw = append(raw, sum[:]...)
	return raw, buildPackIndex(ids, p.offsets, p.sums, hash.ObjectID(sum))
}

func packObjectHeader(kind byte, size int64) []byte {
	current := kind<<4 | byte(size&0x0f)
	size >>= 4
	var out []byte
	for size > 0 {
		out = append(out, current|0x80)
		current = byte(size & 0x7f)
		size >>= 7
	}
	return append(out, current)
}

func buildPackIndex(ids []hash.ObjectID, offsets []int64, sums []uint32, packHash hash.ObjectID) []byte {
	order := make([]int, len(ids))
	for i := range order {
		order[i] = i
	}
	slices.SortFunc(order, func(a, b int) int { return ids[a].Compare(ids[b]) })
	out := slices.Concat([]byte{0xff, 0x74, 0x4f, 0x63}, binary.BigEndian.AppendUint32(nil, 2))
	var fanout [256]uint32
	for _, id := range ids {
		for bucket := int(id[0]); bucket < 256; bucket++ {
			fanout[bucket]++
		}
	}
	for _, value := range fanout {
		out = binary.BigEndian.AppendUint32(out, value)
	}
	for _, at := range order {
		out = append(out, ids[at][:]...)
	}
	for _, at := range order {
		out = binary.BigEndian.AppendUint32(out, sums[at])
	}
	for _, at := range order {
		out = binary.BigEndian.AppendUint32(out, uint32(offsets[at]))
	}
	out = append(out, packHash[:]...)
	sum := sha1.Sum(out)
	return append(out, sum[:]...)
}

func deltaVarint(size int64) []byte {
	var out []byte
	for {
		current := byte(size & 0x7f)
		size >>= 7
		if size == 0 {
			return append(out, current)
		}
		out = append(out, current|0x80)
	}
}

func copyWholeBase(size int64) []byte {
	out := []byte{0x80}
	for shift := range 3 {
		if current := byte(size >> (8 * shift)); current != 0 {
			out[0] |= 1 << (4 + shift)
			out = append(out, current)
		}
	}
	return out
}

func insertOp(data []byte) []byte {
	return append([]byte{byte(len(data))}, data...)
}

func swapFileWrite(t *testing.T, failure error) {
	t.Helper()
	previous := fileWrite
	fileWrite = func(file *os.File, data []byte) (int, error) { return 0, failure }
	t.Cleanup(func() { fileWrite = previous })
}

func swapRootCreate(t *testing.T, failure error) {
	t.Helper()
	previous := rootCreate
	rootCreate = func(*os.Root, string, fs.FileMode) (*os.File, error) { return nil, failure }
	t.Cleanup(func() { rootCreate = previous })
}

func swapRootRename(t *testing.T, failure error) {
	t.Helper()
	previous := rootRename
	rootRename = func(*os.Root, string, string) error { return failure }
	t.Cleanup(func() { rootRename = previous })
}

func swapRootStat(t *testing.T, fail func(*os.Root, string) bool, failure error) {
	t.Helper()
	previous := rootStat
	rootStat = func(root *os.Root, name string) (fs.FileInfo, error) {
		if fail(root, filepath.ToSlash(name)) {
			return nil, failure
		}
		return previous(root, name)
	}
	t.Cleanup(func() { rootStat = previous })
}

func swapRootOpen(t *testing.T, fail func(*os.Root, string) bool, failure error) {
	t.Helper()
	previous := rootOpen
	rootOpen = func(root *os.Root, name string) (*os.File, error) {
		if fail(root, filepath.ToSlash(name)) {
			return nil, failure
		}
		return previous(root, name)
	}
	t.Cleanup(func() { rootOpen = previous })
}

func always(*os.Root, string) bool { return true }

func onlyIn(target *DB) func(*os.Root, string) bool {
	return func(root *os.Root, _ string) bool { return root == target.root }
}

func swapRootMkdirAll(t *testing.T, failure error) {
	t.Helper()
	previous := rootMkdirAll
	rootMkdirAll = func(*os.Root, string, fs.FileMode) error { return failure }
	t.Cleanup(func() { rootMkdirAll = previous })
}
