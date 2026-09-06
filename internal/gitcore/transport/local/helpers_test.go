package local

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/repo"
)

type testRepo struct {
	t     testing.TB
	dir   string
	repo  *repo.Repository
	db    *odb.DB
	refs  *refs.Store
	clock int64
}

func isolatedGlobalFile(t testing.TB) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(path, nil, 0o666); err != nil {
		t.Fatalf("WriteFile returned error %v", err)
	}
	return path
}

func newTestRepo(t testing.TB, bare bool) *testRepo {
	t.Helper()
	dir := t.TempDir()
	r, err := repo.Init(dir, repo.InitOptions{
		InitialBranch: "main",
		Bare:          bare,
		NoSystem:      true,
		GlobalFile:    isolatedGlobalFile(t),
	})
	if err != nil {
		t.Fatalf("repo.Init returned error %v", err)
	}
	db, err := odb.Open(r.ObjectsDir(), odb.Options{Format: r.ObjectFormat})
	if err != nil {
		t.Fatalf("odb.Open returned error %v", err)
	}
	sig := object.Signature{Name: "ann", Email: "ann@example.com"}
	store, err := refs.Open(refs.Options{
		GitDir:    r.GitDir(),
		CommonDir: r.CommonDir(),
		Bare:      bare,
		Peeler:    db,
		Committer: func() object.Signature { return sig },
	})
	if err != nil {
		t.Fatalf("refs.Open returned error %v", err)
	}
	tr := &testRepo{t: t, dir: dir, repo: r, db: db, refs: store, clock: 1700000000}
	t.Cleanup(func() {
		_ = store.Close()
		_ = db.Close()
		_ = r.Close()
	})
	return tr
}

func (r *testRepo) blob(content string) hash.ObjectID {
	r.t.Helper()
	id, err := r.db.Put(object.TypeBlob, []byte(content))
	if err != nil {
		r.t.Fatalf("Put returned error %v", err)
	}
	return id
}

func (r *testRepo) tree(files map[string]string) hash.ObjectID {
	r.t.Helper()
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	tr := &object.Tree{}
	for _, name := range names {
		tr.Entries = append(tr.Entries, object.TreeEntry{Mode: object.ModeBlob, Name: name, ID: r.blob(files[name])})
	}
	tr.Sort()
	id, err := r.db.PutObject(tr)
	if err != nil {
		r.t.Fatalf("PutObject returned error %v", err)
	}
	return id
}

func (r *testRepo) signature() object.Signature {
	r.clock += 60
	return object.Signature{Name: "ann", Email: "ann@example.com", When: time.Unix(r.clock, 0)}
}

func (r *testRepo) commitWithTree(treeID hash.ObjectID, parents ...hash.ObjectID) hash.ObjectID {
	r.t.Helper()
	sig := r.signature()
	commit := &object.Commit{Tree: treeID, Parents: parents, Author: sig, Committer: sig, Message: "commit\n"}
	id, err := r.db.PutObject(commit)
	if err != nil {
		r.t.Fatalf("PutObject returned error %v", err)
	}
	return id
}

func (r *testRepo) commit(branch string, files map[string]string, parents ...hash.ObjectID) hash.ObjectID {
	r.t.Helper()
	id := r.commitWithTree(r.tree(files), parents...)
	r.setBranch(branch, id)
	return id
}

func (r *testRepo) treeWithEntries(entries []object.TreeEntry) hash.ObjectID {
	r.t.Helper()
	tr := &object.Tree{Entries: entries}
	tr.Sort()
	id, err := r.db.PutObject(tr)
	if err != nil {
		r.t.Fatalf("PutObject returned error %v", err)
	}
	return id
}

func (r *testRepo) missingObjectID() hash.ObjectID {
	return hash.SumSHA1("blob", []byte("this object was never written to any odb"))
}

func (r *testRepo) corruptLooseObject() hash.ObjectID {
	r.t.Helper()
	id := r.missingObjectID()
	dir := filepath.Join(r.repo.ObjectsDir(), id.String()[:2])
	if err := os.MkdirAll(dir, 0o755); err != nil {
		r.t.Fatalf("MkdirAll returned error %v", err)
	}
	path := filepath.Join(dir, id.String()[2:])
	if err := os.WriteFile(path, []byte("not zlib data at all"), 0o644); err != nil {
		r.t.Fatalf("WriteFile returned error %v", err)
	}
	return id
}

func (r *testRepo) rawObject(kind object.Type, data []byte) hash.ObjectID {
	r.t.Helper()
	id, err := r.db.Put(kind, data)
	if err != nil {
		r.t.Fatalf("Put returned error %v", err)
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

func (r *testRepo) setBranch(branch string, target hash.ObjectID) {
	r.t.Helper()
	tx := r.refs.Begin()
	if err := tx.Set(refs.BranchName(branch), target); err != nil {
		r.t.Fatalf("Set returned error %v", err)
	}
	if err := tx.Commit(); err != nil {
		r.t.Fatalf("Commit returned error %v", err)
	}
}

func (r *testRepo) lightweightTag(name string, target hash.ObjectID) {
	r.t.Helper()
	tx := r.refs.Begin()
	if err := tx.Set(refs.TagName(name), target); err != nil {
		r.t.Fatalf("Set returned error %v", err)
	}
	if err := tx.Commit(); err != nil {
		r.t.Fatalf("Commit returned error %v", err)
	}
}

func (r *testRepo) annotatedTag(name string, target hash.ObjectID, targetType object.Type) hash.ObjectID {
	r.t.Helper()
	sig := r.signature()
	tag := &object.Tag{Object: target, ObjectType: targetType, Name: name, Tagger: &sig, Message: "tag\n"}
	id, err := r.db.PutObject(tag)
	if err != nil {
		r.t.Fatalf("PutObject returned error %v", err)
	}
	r.lightweightTag(name, id)
	return id
}

func (r *testRepo) branchTarget(branch string) hash.ObjectID {
	r.t.Helper()
	ref, err := r.refs.Lookup(refs.BranchName(branch))
	if err != nil {
		r.t.Fatalf("Lookup returned error %v", err)
	}
	return ref.Target
}

func (r *testRepo) hasObject(id hash.ObjectID) bool {
	r.t.Helper()
	if _, err := r.db.Reload(); err != nil {
		r.t.Fatalf("Reload returned error %v", err)
	}
	known, err := r.db.Has(id)
	if err != nil {
		r.t.Fatalf("Has returned error %v", err)
	}
	return known
}

func (r *testRepo) writeRawHead(content string) {
	r.t.Helper()
	if err := r.repo.Root().WriteFile("HEAD", []byte(content), 0o666); err != nil {
		r.t.Fatalf("WriteFile returned error %v", err)
	}
}

func (r *testRepo) removeHead() {
	r.t.Helper()
	if err := r.repo.Root().Remove("HEAD"); err != nil {
		r.t.Fatalf("Remove returned error %v", err)
	}
}

func (r *testRepo) writeRawRef(rel, content string) {
	r.t.Helper()
	if err := r.repo.CommonRoot().WriteFile(filepath.FromSlash(rel), []byte(content), 0o666); err != nil {
		r.t.Fatalf("WriteFile returned error %v", err)
	}
}

func (r *testRepo) allObjects() map[hash.ObjectID]struct{} {
	r.t.Helper()
	if _, err := r.db.Reload(); err != nil {
		r.t.Fatalf("Reload returned error %v", err)
	}
	out := make(map[hash.ObjectID]struct{})
	for id, err := range r.db.All() {
		if err != nil {
			r.t.Fatalf("All returned error %v", err)
		}
		out[id] = struct{}{}
	}
	return out
}
