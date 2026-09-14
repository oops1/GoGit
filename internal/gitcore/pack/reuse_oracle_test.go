//go:build oracle

package pack

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
)

type storeSource struct {
	store *Store
}

func (s storeSource) Get(id hash.ObjectID) (object.Type, []byte, error) {
	kind, data, ok, err := s.store.Get(id)
	if err == nil && !ok {
		err = fmt.Errorf("%w: %s", ErrBaseNotFound, id)
	}
	return kind, data, err
}

func (s storeSource) Info(id hash.ObjectID) (object.Type, int64, error) {
	for file := range s.store.Acquire() {
		offset, ok, err := file.Index.Lookup(id)
		if err != nil {
			return 0, 0, err
		}
		if ok {
			return file.Pack.InfoAt(offset)
		}
	}
	return 0, 0, fmt.Errorf("%w: %s", ErrBaseNotFound, id)
}

func (s storeSource) PackReuse() *Reuse {
	return NewReuse(s.store)
}

func TestOracleWritePackReusesAGitPackIntoOneGitIndexesIdentically(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git is not available: %v", err)
	}
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatalf("MkdirAll returned error %v", err)
	}
	env := writerOracleEnv(home)
	repo := filepath.Join(root, "repo")
	runWriterOracleGit(t, root, env, "init", "-q", "-b", "main", repo)
	var text strings.Builder
	for line := range 400 {
		fmt.Fprintf(&text, "line %03d of a file git deltifies\n", line)
	}
	for step := range 40 {
		fmt.Fprintf(&text, "change %d\n", step)
		if err := os.WriteFile(filepath.Join(repo, fmt.Sprintf("file%d.txt", step%4)), []byte(text.String()), 0o644); err != nil {
			t.Fatalf("WriteFile returned error %v", err)
		}
		runWriterOracleGit(t, repo, env, "add", ".")
		runWriterOracleGit(t, repo, env, "-c", "user.name=oracle", "-c", "user.email=oracle@example.com", "commit", "-q", "-m", fmt.Sprintf("change %d", step))
	}
	runWriterOracleGit(t, repo, env, "repack", "-a", "-d", "-f", "-q", "--window=10", "--depth=50")
	packDir := filepath.Join(repo, ".git", "objects", "pack")
	store, err := Open(packDir)
	if err != nil {
		t.Fatalf("Open returned error %v", err)
	}
	defer func() { _ = store.Close() }()
	gitPack := store.Files()[0].Pack
	ids := slices.Collect(store.Objects())

	started := time.Now()
	result, err := WritePackFile(t.Context(), t.TempDir(), storeSource{store: store}, ids, WriteOptions{Window: 10, Depth: 50})
	elapsed := time.Since(started)
	if err != nil {
		t.Fatalf("WritePackFile returned error %v", err)
	}

	runWriterOracleGit(t, repo, env, "verify-pack", result.IndexPath)
	theirs := filepath.Join(t.TempDir(), "theirs.idx")
	runWriterOracleGit(t, repo, env, "index-pack", "-o", theirs, result.PackPath)
	if ours, git := readOracleFile(t, result.IndexPath), readOracleFile(t, theirs); !bytes.Equal(ours, git) {
		t.Fatal("git index-pack writes another index for our pack")
	}
	written, err := Open(filepath.Dir(result.PackPath))
	if err != nil {
		t.Fatalf("Open returned error %v", err)
	}
	defer func() { _ = written.Close() }()
	for _, id := range ids {
		wantKind, want, _, _ := store.Get(id)
		kind, data, ok, err := written.Get(id)
		if err != nil || !ok || kind != wantKind || !bytes.Equal(data, want) {
			t.Fatalf("%s read back as (%v, %d bytes, %v, %v)", id, kind, len(data), ok, err)
		}
	}
	if result.Bytes > gitPack.Size()+gitPack.Size()/10 {
		t.Fatalf("our pack takes %d bytes, git's %d", result.Bytes, gitPack.Size())
	}
	t.Logf("git pack %d bytes, ours %d bytes written in %v", gitPack.Size(), result.Bytes, elapsed)
}

func readOracleFile(t testing.TB, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile returned error %v", err)
	}
	return data
}
