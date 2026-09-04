package pack

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
)

const (
	packsDir     = "testdata/packs"
	thinPackPath = "testdata/thin/thin.pack"
	verifyDir    = "testdata/verify"
	objectsTable = "testdata/objects.tsv"
	offsetPack   = "pack-"
)

type packRecord struct {
	id      hash.ObjectID
	kind    object.Type
	size    int64
	packed  int64
	offset  int64
	depth   int
	base    hash.ObjectID
	isDelta bool
}

type objectRecord struct {
	kind object.Type
	size int64
}

func fixtureNames(t testing.TB) []string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(packsDir, "*"+packSuffix))
	if err != nil {
		t.Fatalf("Glob returned error %v", err)
	}
	if len(matches) != 2 {
		t.Fatalf("testdata holds %d packfiles, want 2", len(matches))
	}
	names := make([]string, 0, len(matches))
	for _, match := range matches {
		names = append(names, strings.TrimSuffix(filepath.Base(match), packSuffix))
	}
	return names
}

func fixtureName(t testing.TB, prefix string) string {
	t.Helper()
	for _, name := range fixtureNames(t) {
		if strings.HasPrefix(name, prefix) {
			return name
		}
	}
	t.Fatalf("no packfile fixture starts with %q", prefix)
	return ""
}

func fixturePackPath(t testing.TB, name string) string {
	t.Helper()
	return filepath.Join(packsDir, name+packSuffix)
}

func fixtureIndexPath(t testing.TB, name string) string {
	t.Helper()
	return filepath.Join(packsDir, name+indexSuffix)
}

func readFixture(t testing.TB, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) returned error %v", path, err)
	}
	return data
}

func fixtureLines(t testing.TB, path string) []string {
	t.Helper()
	text := strings.ReplaceAll(string(readFixture(t, path)), "\r\n", "\n")
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

func parseInt(t testing.TB, text string) int64 {
	t.Helper()
	value, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		t.Fatalf("ParseInt(%q) returned error %v", text, err)
	}
	return value
}

func verifyRecords(t testing.TB, name string) []packRecord {
	t.Helper()
	records := parseVerifyLines(t, fixtureLines(t, filepath.Join(verifyDir, name+".txt")))
	if len(records) == 0 {
		t.Fatalf("verify-pack fixture for %q holds no records", name)
	}
	return records
}

func parseVerifyLines(t testing.TB, lines []string) []packRecord {
	t.Helper()
	var records []packRecord
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 5 || len(fields[0]) != hash.HexSize {
			continue
		}
		kind, err := object.ParseType(fields[1])
		if err != nil {
			t.Fatalf("ParseType(%q) returned error %v", fields[1], err)
		}
		record := packRecord{
			id:     parseID(t, fields[0]),
			kind:   kind,
			size:   parseInt(t, fields[2]),
			packed: parseInt(t, fields[3]),
			offset: parseInt(t, fields[4]),
		}
		if len(fields) == 7 {
			record.depth = int(parseInt(t, fields[5]))
			record.base = parseID(t, fields[6])
			record.isDelta = true
		}
		records = append(records, record)
	}
	return records
}

func showIndexEntries(t testing.TB, name string) map[hash.ObjectID]Entry {
	t.Helper()
	entries := make(map[hash.ObjectID]Entry)
	for _, line := range fixtureLines(t, filepath.Join(verifyDir, name+".index.txt")) {
		fields := strings.Fields(line)
		if len(fields) != 3 {
			t.Fatalf("show-index line %q holds %d fields, want 3", line, len(fields))
		}
		sum, err := strconv.ParseUint(strings.Trim(fields[2], "()"), 16, 32)
		if err != nil {
			t.Fatalf("ParseUint(%q) returned error %v", fields[2], err)
		}
		id := parseID(t, fields[1])
		entries[id] = Entry{ID: id, Offset: parseInt(t, fields[0]), CRC32: uint32(sum)}
	}
	return entries
}

func objectTypes(t testing.TB) map[hash.ObjectID]objectRecord {
	t.Helper()
	table := make(map[hash.ObjectID]objectRecord)
	for _, line := range fixtureLines(t, objectsTable) {
		fields := strings.Split(line, "\t")
		if len(fields) != 3 {
			t.Fatalf("objects table line %q holds %d fields, want 3", line, len(fields))
		}
		kind, err := object.ParseType(fields[1])
		if err != nil {
			t.Fatalf("ParseType(%q) returned error %v", fields[1], err)
		}
		table[parseID(t, fields[0])] = objectRecord{kind: kind, size: parseInt(t, fields[2])}
	}
	if len(table) == 0 {
		t.Fatal("objects table is empty")
	}
	return table
}

func openFixturePack(t testing.TB, name string, opts ...Option) *Pack {
	t.Helper()
	packfile, err := OpenPack(fixturePackPath(t, name), opts...)
	if err != nil {
		t.Fatalf("OpenPack(%q) returned error %v", name, err)
	}
	t.Cleanup(func() { _ = packfile.Close() })
	return packfile
}

func openFixtureIndex(t testing.TB, name string) *Index {
	t.Helper()
	index, err := OpenIndex(fixtureIndexPath(t, name))
	if err != nil {
		t.Fatalf("OpenIndex(%q) returned error %v", name, err)
	}
	t.Cleanup(func() { _ = index.Close() })
	return index
}

func copyFixtureDir(t testing.TB, names ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range names {
		for _, suffix := range []string{packSuffix, indexSuffix} {
			source := filepath.Join(packsDir, name+suffix)
			writeTemp(t, filepath.Join(dir, name+suffix), readFixture(t, source))
		}
	}
	return dir
}

func writeTemp(t testing.TB, path string, data []byte) string {
	t.Helper()
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("WriteFile(%q) returned error %v", path, err)
	}
	return path
}
