package odb

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
)

func makeObjectsDir(t testing.TB, parent, name string) string {
	t.Helper()
	dir := filepath.Join(parent, name, "objects")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) returned error %v", dir, err)
	}
	return dir
}

func writeAlternates(t testing.TB, objects string, lines ...string) {
	t.Helper()
	writeFile(t, filepath.Join(objects, filepath.FromSlash(alternatesFile)), []byte(strings.Join(lines, "\n")+"\n"))
}

func storeBlob(t testing.TB, objects string, content []byte) hash.ObjectID {
	t.Helper()
	db := openDB(t, objects, Options{})
	id, err := db.Put(object.TypeBlob, content)
	if err != nil {
		t.Fatalf("Put returned error %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close returned error %v", err)
	}
	return id
}

func TestOpenFollowsTheAlternatesFile(t *testing.T) {
	root := t.TempDir()
	main := makeObjectsDir(t, root, "main")
	shared := makeObjectsDir(t, root, "shared")
	id := storeBlob(t, shared, []byte("borrowed content\n"))
	writeAlternates(t, main, "# comment", "", shared)
	db := openDB(t, main, Options{})
	if len(db.Alternates()) != 1 {
		t.Fatalf("Open linked %d alternates, want 1", len(db.Alternates()))
	}
	kind, data, err := db.Get(id)
	if err != nil || kind != object.TypeBlob || string(data) != "borrowed content\n" {
		t.Fatalf("Get gave (%s, %q, %v)", kind, data, err)
	}
	known, err := db.Has(id)
	if err != nil || !known {
		t.Fatalf("Has gave (%v, %v)", known, err)
	}
	size, err := db.Size(id)
	if err != nil || size != int64(len("borrowed content\n")) {
		t.Fatalf("Size gave (%d, %v)", size, err)
	}
}

func TestOpenResolvesRelativeAlternates(t *testing.T) {
	root := t.TempDir()
	main := makeObjectsDir(t, root, "main")
	shared := makeObjectsDir(t, root, "shared")
	id := storeBlob(t, shared, []byte("relative alternate\n"))
	writeAlternates(t, main, "../../shared/objects")
	db := openDB(t, main, Options{})
	if _, _, err := db.Get(id); err != nil {
		t.Fatalf("Get returned error %v", err)
	}
}

func TestOpenTakesAlternatesFromOptions(t *testing.T) {
	root := t.TempDir()
	main := makeObjectsDir(t, root, "main")
	shared := makeObjectsDir(t, root, "shared")
	id := storeBlob(t, shared, []byte("alternate from the environment\n"))
	db := openDB(t, main, Options{Alternates: []string{shared}})
	if _, _, err := db.Get(id); err != nil {
		t.Fatalf("Get returned error %v", err)
	}
}

func TestOpenSkipsMissingAlternateDirectories(t *testing.T) {
	root := t.TempDir()
	main := makeObjectsDir(t, root, "main")
	writeAlternates(t, main, filepath.Join(root, "gone", "objects"))
	db := openDB(t, main, Options{})
	if len(db.Alternates()) != 0 {
		t.Fatalf("Open linked %d alternates, want none", len(db.Alternates()))
	}
}

func TestOpenDetectsAlternateLoops(t *testing.T) {
	root := t.TempDir()
	first := makeObjectsDir(t, root, "first")
	second := makeObjectsDir(t, root, "second")
	writeAlternates(t, first, second)
	writeAlternates(t, second, first)
	if _, err := Open(first, Options{}); !errors.Is(err, ErrAlternatesLoop) {
		t.Fatalf("Open returned %v, want %v", err, ErrAlternatesLoop)
	}
}

func TestOpenStopsDeepAlternateChains(t *testing.T) {
	root := t.TempDir()
	dirs := make([]string, MaxAlternatesDepth+2)
	for i := range dirs {
		dirs[i] = makeObjectsDir(t, root, "level"+string(rune('a'+i)))
	}
	for i := range len(dirs) - 1 {
		writeAlternates(t, dirs[i], dirs[i+1])
	}
	if _, err := Open(dirs[0], Options{}); !errors.Is(err, ErrAlternatesLoop) {
		t.Fatalf("Open returned %v, want %v", err, ErrAlternatesLoop)
	}
}

func TestOpenLinksARepeatedAlternateOnce(t *testing.T) {
	root := t.TempDir()
	main := makeObjectsDir(t, root, "main")
	left := makeObjectsDir(t, root, "left")
	right := makeObjectsDir(t, root, "right")
	shared := makeObjectsDir(t, root, "shared")
	writeAlternates(t, main, left, right)
	writeAlternates(t, left, shared)
	writeAlternates(t, right, shared)
	db := openDB(t, main, Options{})
	if len(db.Alternates()) != 2 {
		t.Fatalf("Open linked %d alternates, want 2", len(db.Alternates()))
	}
	if len(db.Alternates()[0].Alternates()) != 1 || len(db.Alternates()[1].Alternates()) != 0 {
		t.Fatal("Open linked the shared directory more than once")
	}
}

func TestOpenReportsUnreadableAlternatesFiles(t *testing.T) {
	main := newObjectsDir(t)
	if err := os.MkdirAll(filepath.Join(main, filepath.FromSlash(alternatesFile)), 0o755); err != nil {
		t.Fatalf("MkdirAll returned error %v", err)
	}
	if _, err := Open(main, Options{}); err == nil {
		t.Fatal("Open accepted an unreadable alternates file")
	}
}

func TestOpenReportsFailuresInsideAlternates(t *testing.T) {
	root := t.TempDir()
	main := makeObjectsDir(t, root, "main")
	shared := makeObjectsDir(t, root, "shared")
	writeFile(t, filepath.Join(shared, packDirName, "pack-broken.pack"), []byte("not a packfile"))
	writeFile(t, filepath.Join(shared, packDirName, "pack-broken.idx"), []byte("not an index"))
	writeAlternates(t, main, shared)
	if _, err := Open(main, Options{}); err == nil {
		t.Fatal("Open accepted a damaged alternate")
	}
}

func TestParseAlternatesResolvesEveryForm(t *testing.T) {
	base := filepath.Join("root", "objects")
	absolute := filepath.Join(t.TempDir(), "shared", "objects")
	got := parseAlternates(base, []byte("# comment\n\nrelative/objects\r\n"+absolute+"\n"))
	want := []string{filepath.Join(base, "relative", "objects"), absolute}
	if !slices.Equal(got, want) {
		t.Fatalf("parseAlternates gave %v, want %v", got, want)
	}
}

func TestAlternatesReportReadFailures(t *testing.T) {
	root := t.TempDir()
	main := makeObjectsDir(t, root, "main")
	shared := makeObjectsDir(t, root, "shared")
	id := storeBlob(t, shared, []byte("failing alternate\n"))
	writeAlternates(t, main, shared)
	db := openDB(t, main, Options{})
	alternate := db.Alternates()[0]
	swapRootOpen(t, onlyIn(alternate), errInjected)
	if _, _, err := db.Get(id); !errors.Is(err, errInjected) {
		t.Fatalf("Get returned %v, want %v", err, errInjected)
	}
	if _, err := db.Type(id); !errors.Is(err, errInjected) {
		t.Fatalf("Type returned %v, want %v", err, errInjected)
	}
	swapRootStat(t, onlyIn(alternate), errInjected)
	if _, err := db.Has(id); !errors.Is(err, errInjected) {
		t.Fatalf("Has returned %v, want %v", err, errInjected)
	}
}

func TestReloadVisitsAlternates(t *testing.T) {
	root := t.TempDir()
	main := makeObjectsDir(t, root, "main")
	shared := makeObjectsDir(t, root, "shared")
	writeAlternates(t, main, shared)
	db := openDB(t, main, Options{})
	copyFixturePacks(t, shared)
	changed, err := db.Reload()
	if err != nil || !changed {
		t.Fatalf("Reload gave (%v, %v)", changed, err)
	}
	packed := packFixtureObjects(t)[0]
	if _, _, err := db.Get(packed.id); err != nil {
		t.Fatalf("Get returned error %v", err)
	}
}

func TestReloadReportsAlternateFailures(t *testing.T) {
	root := t.TempDir()
	main := makeObjectsDir(t, root, "main")
	shared := makeObjectsDir(t, root, "shared")
	writeAlternates(t, main, shared)
	db := openDB(t, main, Options{})
	writeFile(t, filepath.Join(shared, packDirName, "pack-broken.pack"), []byte("not a packfile"))
	writeFile(t, filepath.Join(shared, packDirName, "pack-broken.idx"), []byte("not an index"))
	if _, err := db.Reload(); err == nil {
		t.Fatal("Reload accepted a damaged alternate")
	}
}
