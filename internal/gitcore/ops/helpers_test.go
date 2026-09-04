package ops

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/index"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/repo"
)

func testSignature() object.Signature {
	return object.Signature{Name: "ann", Email: "ann@example.com", When: time.Unix(1700000000, 0)}
}

type testRepo struct {
	t          testing.TB
	dir        string
	repo       *repo.Repository
	clock      int64
	globalFile string
}

func isolatedGlobalFile(t testing.TB) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(path, nil, 0o666); err != nil {
		t.Fatalf("WriteFile returned error %v", err)
	}
	return path
}

func (r *testRepo) openOptions() repo.OpenOptions {
	return repo.OpenOptions{NoSystem: true, GlobalFile: r.globalFile}
}

func newTestRepoNoIdentity(t testing.TB) *testRepo {
	t.Helper()
	dir := t.TempDir()
	global := isolatedGlobalFile(t)
	r, err := repo.Init(dir, repo.InitOptions{InitialBranch: "main", NoSystem: true, GlobalFile: global})
	if err != nil {
		t.Fatalf("repo.Init returned error %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })
	return &testRepo{t: t, dir: dir, repo: r, clock: 1700000000, globalFile: global}
}

func newTestRepo(t testing.TB) *testRepo {
	t.Helper()
	tr := newTestRepoNoIdentity(t)
	tr.appendConfig("[user]\n\tname = ann\n\temail = ann@example.com\n")
	tr.repo = tr.reopen()
	return tr
}

func newBareTestRepo(t testing.TB) *testRepo {
	t.Helper()
	dir := t.TempDir()
	global := isolatedGlobalFile(t)
	r, err := repo.Init(dir, repo.InitOptions{InitialBranch: "main", Bare: true, NoSystem: true, GlobalFile: global})
	if err != nil {
		t.Fatalf("repo.Init returned error %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })
	return &testRepo{t: t, dir: dir, repo: r, clock: 1700000000, globalFile: global}
}

func (r *testRepo) path(rel string) string {
	return filepath.Join(r.dir, filepath.FromSlash(rel))
}

func (r *testRepo) writeFile(rel, content string) {
	r.t.Helper()
	full := r.path(rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o777); err != nil {
		r.t.Fatalf("MkdirAll returned error %v", err)
	}
	if err := os.WriteFile(full, []byte(content), 0o666); err != nil {
		r.t.Fatalf("WriteFile returned error %v", err)
	}
}

func (r *testRepo) remove(rel string) {
	r.t.Helper()
	if err := os.RemoveAll(r.path(rel)); err != nil {
		r.t.Fatalf("RemoveAll returned error %v", err)
	}
}

func (r *testRepo) symlink(target, rel string) bool {
	r.t.Helper()
	full := r.path(rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o777); err != nil {
		r.t.Fatalf("MkdirAll returned error %v", err)
	}
	if err := os.Symlink(target, full); err != nil {
		return false
	}
	return true
}

func (r *testRepo) chmodExecutable(rel string) {
	r.t.Helper()
	if err := os.Chmod(r.path(rel), 0o755); err != nil {
		r.t.Fatalf("Chmod returned error %v", err)
	}
}

func (r *testRepo) readFile(rel string) string {
	r.t.Helper()
	data, err := os.ReadFile(r.path(rel))
	if err != nil {
		r.t.Fatalf("ReadFile returned error %v", err)
	}
	return string(data)
}

func (r *testRepo) exists(rel string) bool {
	_, err := os.Lstat(r.path(rel))
	return err == nil
}

func (r *testRepo) db() *odb.DB {
	r.t.Helper()
	db, err := odb.Open(r.repo.ObjectsDir(), odb.Options{})
	if err != nil {
		r.t.Fatalf("odb.Open returned error %v", err)
	}
	r.t.Cleanup(func() { _ = db.Close() })
	return db
}

func (r *testRepo) refs() *refs.Store {
	r.t.Helper()
	db := r.db()
	store, err := refs.Open(refs.Options{
		GitDir:    r.repo.GitDir(),
		CommonDir: r.repo.CommonDir(),
		Peeler:    db,
		Committer: testSignature,
	})
	if err != nil {
		r.t.Fatalf("refs.Open returned error %v", err)
	}
	r.t.Cleanup(func() { _ = store.Close() })
	return store
}

func (r *testRepo) index() *index.Index {
	r.t.Helper()
	idx, err := readIndex(r.repo)
	if err != nil {
		r.t.Fatalf("readIndex returned error %v", err)
	}
	return idx
}

func (r *testRepo) saveIndex(idx *index.Index) {
	r.t.Helper()
	if err := idx.WriteFile(r.repo.IndexFile(), index.Version2); err != nil {
		r.t.Fatalf("WriteFile returned error %v", err)
	}
}

func (r *testRepo) commitAll(message string) hash.ObjectID {
	r.t.Helper()
	db := r.db()
	idx := r.index()
	treeID, err := idx.WriteTree(db)
	if err != nil {
		r.t.Fatalf("WriteTree returned error %v", err)
	}
	r.saveIndex(idx)
	r.clock += 60
	sig := object.Signature{Name: "ann", Email: "ann@example.com", When: time.Unix(r.clock, 0)}
	commit := &object.Commit{Tree: treeID, Author: sig, Committer: sig, Message: message + "\n"}
	store := r.refs()
	if parent := r.headCommit(store); !parent.IsZero() {
		commit.Parents = []hash.ObjectID{parent}
	}
	id, err := db.PutObject(commit)
	if err != nil {
		r.t.Fatalf("PutObject returned error %v", err)
	}
	target, err := currentBranchRef(store)
	if err != nil {
		r.t.Fatalf("currentBranchRef returned error %v", err)
	}
	if target == "" {
		target = refs.BranchName("main")
	}
	tx := store.Begin()
	if err := tx.Set(target, id); err != nil {
		r.t.Fatalf("Set returned error %v", err)
	}
	if err := tx.Commit(); err != nil {
		r.t.Fatalf("Commit returned error %v", err)
	}
	return id
}

func (r *testRepo) headCommit(store *refs.Store) hash.ObjectID {
	ref, err := store.Resolve(refs.HEAD)
	if err != nil {
		return hash.Zero
	}
	return ref.Target
}

func (r *testRepo) createBranch(name string, target hash.ObjectID) {
	r.t.Helper()
	store := r.refs()
	tx := store.Begin()
	if err := tx.Set(refs.BranchName(name), target); err != nil {
		r.t.Fatalf("Set returned error %v", err)
	}
	if err := tx.Commit(); err != nil {
		r.t.Fatalf("Commit returned error %v", err)
	}
}

func (r *testRepo) branchTarget(name string) hash.ObjectID {
	r.t.Helper()
	store := r.refs()
	ref, err := store.Lookup(refs.BranchName(name))
	if err != nil {
		r.t.Fatalf("Lookup returned error %v", err)
	}
	return ref.Target
}

func (r *testRepo) headSymbolicTarget() (refs.Name, bool) {
	r.t.Helper()
	store := r.refs()
	ref, err := store.Lookup(refs.HEAD)
	if err != nil {
		r.t.Fatalf("Lookup returned error %v", err)
	}
	return ref.SymbolicTarget, ref.IsSymbolic()
}

func (r *testRepo) appendConfig(text string) {
	r.t.Helper()
	data, err := r.repo.CommonRoot().ReadFile("config")
	if err != nil {
		r.t.Fatalf("ReadFile returned error %v", err)
	}
	data = append(data, []byte(text)...)
	if err := r.repo.CommonRoot().WriteFile("config", data, 0o666); err != nil {
		r.t.Fatalf("WriteFile returned error %v", err)
	}
}

func (r *testRepo) reopen() *repo.Repository {
	r.t.Helper()
	reopened, err := repo.Open(r.dir, r.openOptions())
	if err != nil {
		r.t.Fatalf("repo.Open returned error %v", err)
	}
	r.t.Cleanup(func() { _ = reopened.Close() })
	return reopened
}

func entryOf(t testing.TB, idx *index.Index, path string) (index.Entry, bool) {
	t.Helper()
	entry, ok := idx.Get(path, index.StageMerged)
	if !ok {
		return index.Entry{}, false
	}
	return *entry, true
}

func (r *testRepo) writeRawRef(rel, content string) {
	r.t.Helper()
	if err := r.repo.CommonRoot().WriteFile(filepath.FromSlash(rel), []byte(content), 0o666); err != nil {
		r.t.Fatalf("WriteFile returned error %v", err)
	}
}

func (r *testRepo) writeRawHead(content string) {
	r.t.Helper()
	if err := r.repo.Root().WriteFile("HEAD", []byte(content), 0o666); err != nil {
		r.t.Fatalf("WriteFile returned error %v", err)
	}
}

func (r *testRepo) corruptIndexFile() {
	r.t.Helper()
	if err := os.WriteFile(r.repo.IndexFile(), []byte("not an index file"), 0o666); err != nil {
		r.t.Fatalf("WriteFile returned error %v", err)
	}
}

func bogusObjectID(t testing.TB, format hash.Format) hash.ObjectID {
	t.Helper()
	id, err := hash.Sum(format, "commit", []byte("this object was never written to the odb"))
	if err != nil {
		t.Fatalf("hash.Sum returned error %v", err)
	}
	return id
}

type countingContext struct {
	context.Context
	calls  *int
	failAt int
}

func (c countingContext) Err() error {
	*c.calls++
	if *c.calls >= c.failAt {
		return context.Canceled
	}
	return nil
}

func newCountingContext(t testing.TB, failAt int) context.Context {
	t.Helper()
	calls := 0
	return countingContext{Context: t.Context(), calls: &calls, failAt: failAt}
}
