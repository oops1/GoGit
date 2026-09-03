package refs

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/oops1/gogit/internal/gitcore/config"
	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
)

func testSignature() object.Signature {
	return object.Signature{
		Name:  "Go Git",
		Email: "gogit@example.com",
		When:  time.Unix(1700000000, 0).In(time.FixedZone("+0300", 3*3600)),
	}
}

func testCommitter() func() object.Signature {
	return testSignature
}

func oidFrom(t *testing.T, seed string) hash.ObjectID {
	t.Helper()
	if len(seed) > hash.HexSize {
		t.Fatalf("seed %q is longer than %d", seed, hash.HexSize)
	}
	id, err := hash.Parse(seed + strings.Repeat("0", hash.HexSize-len(seed)))
	if err != nil {
		t.Fatalf("Parse(%q) returned error %v", seed, err)
	}
	return id
}

func loadConfig(t *testing.T, value string) *config.Config {
	t.Helper()
	dir := t.TempDir()
	content := ""
	if value != "" {
		content = "[core]\n\tlogAllRefUpdates = " + value + "\n"
	}
	writeAt(t, dir, "config", content)
	cfg, err := config.Load(config.Options{
		GitDir:     dir,
		NoSystem:   true,
		GlobalFile: filepath.Join(dir, "absent"),
	})
	if err != nil {
		t.Fatalf("config.Load returned error %v", err)
	}
	return cfg
}

func newGitDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, sub := range []string{"refs/heads", "refs/tags"} {
		if err := os.MkdirAll(filepath.Join(dir, filepath.FromSlash(sub)), 0o777); err != nil {
			t.Fatalf("MkdirAll returned error %v", err)
		}
	}
	writeAt(t, dir, "HEAD", "ref: refs/heads/main\n")
	return dir
}

func writeBenchmarkRepository(dir string) error {
	if err := os.WriteFile(filepath.Join(dir, "HEAD"), []byte("ref: refs/heads/main\n"), 0o666); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(dir, "refs", "heads"), 0o777); err != nil {
		return err
	}
	for index := range 100 {
		name := fmt.Sprintf("branch-%04d", index)
		content := fmt.Sprintf("%040x\n", index+1)
		if err := os.WriteFile(filepath.Join(dir, "refs", "heads", name), []byte(content), 0o666); err != nil {
			return err
		}
	}
	return os.WriteFile(filepath.Join(dir, packedRefsFile), benchmarkPackedRefs(1000), 0o666)
}

func writeAt(t *testing.T, dir, rel, content string) {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o777); err != nil {
		t.Fatalf("MkdirAll returned error %v", err)
	}
	if err := os.WriteFile(full, []byte(content), 0o666); err != nil {
		t.Fatalf("WriteFile(%q) returned error %v", rel, err)
	}
}

func mkdirAt(t *testing.T, dir, rel string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, filepath.FromSlash(rel)), 0o777); err != nil {
		t.Fatalf("MkdirAll(%q) returned error %v", rel, err)
	}
}

func readAt(t *testing.T, dir, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("ReadFile(%q) returned error %v", rel, err)
	}
	return string(data)
}

func existsAt(dir, rel string) bool {
	_, err := os.Lstat(filepath.Join(dir, filepath.FromSlash(rel)))
	return err == nil
}

func openStore(t *testing.T, dir string) *Store {
	t.Helper()
	return openStoreWith(t, Options{GitDir: dir, Committer: testCommitter()})
}

func openStoreWith(t *testing.T, opts Options) *Store {
	t.Helper()
	store, err := Open(opts)
	if err != nil {
		t.Fatalf("Open returned error %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("Close returned error %v", err)
		}
	})
	return store
}

func collect(t *testing.T, store *Store, prefix string) []Ref {
	t.Helper()
	var refs []Ref
	for ref, err := range store.Prefix(prefix) {
		if err != nil {
			t.Fatalf("Prefix(%q) returned error %v", prefix, err)
		}
		refs = append(refs, ref)
	}
	return refs
}

func names(refs []Ref) []string {
	out := make([]string, 0, len(refs))
	for _, ref := range refs {
		out = append(out, string(ref.Name))
	}
	return out
}

func iterationError(t *testing.T, store *Store, prefix string) error {
	t.Helper()
	for _, err := range store.Prefix(prefix) {
		if err != nil {
			return err
		}
	}
	return nil
}

type fakePeeler struct {
	tags map[hash.ObjectID]hash.ObjectID
	err  error
}

func (p fakePeeler) PeelTag(id hash.ObjectID) (hash.ObjectID, bool, error) {
	if p.err != nil {
		return hash.Zero, false, p.err
	}
	target, ok := p.tags[id]
	return target, ok, nil
}

func swapLstat(t *testing.T, fail func(name string) bool) {
	t.Helper()
	previous := fsLstat
	fsLstat = func(root *os.Root, name string) (fs.FileInfo, error) {
		if fail(filepath.ToSlash(name)) {
			return nil, fs.ErrNotExist
		}
		return previous(root, name)
	}
	t.Cleanup(func() { fsLstat = previous })
}

func swapReadFile(t *testing.T, fail func(name string) bool, err error) {
	t.Helper()
	previous := fsReadFile
	fsReadFile = func(root *os.Root, name string) ([]byte, error) {
		if fail(filepath.ToSlash(name)) {
			return nil, err
		}
		return previous(root, name)
	}
	t.Cleanup(func() { fsReadFile = previous })
}

func swapRename(t *testing.T, fail func(from string) bool, err error) {
	t.Helper()
	previous := fsRename
	fsRename = func(root *os.Root, from, to string) error {
		if fail(filepath.ToSlash(from)) {
			return err
		}
		return previous(root, from, to)
	}
	t.Cleanup(func() { fsRename = previous })
}

func swapRemove(t *testing.T, fail func(name string) bool, err error) {
	t.Helper()
	previous := fsRemove
	fsRemove = func(root *os.Root, name string) error {
		if fail(filepath.ToSlash(name)) {
			return err
		}
		return previous(root, name)
	}
	t.Cleanup(func() { fsRemove = previous })
}

func swapWrite(t *testing.T, after int, err error) {
	t.Helper()
	previous := fsWrite
	calls := 0
	fsWrite = func(file *os.File, data []byte) (int, error) {
		calls++
		if calls > after {
			return 0, err
		}
		return previous(file, data)
	}
	t.Cleanup(func() { fsWrite = previous })
}
