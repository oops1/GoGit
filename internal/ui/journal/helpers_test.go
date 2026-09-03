package journal

import (
	"iter"
	"path/filepath"
	"testing"
	"time"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/gitcore/revision"
)

func noEnv(string) string { return "" }

func testSignature() object.Signature {
	return object.Signature{Name: "Go Git", Email: "gogit@example.com", When: time.Unix(1700000000, 0).UTC()}
}

func initTestRepo(t *testing.T, branch string) *repo.Repository {
	t.Helper()
	dir := t.TempDir()
	r, err := repo.Init(filepath.Join(dir, "work"), repo.InitOptions{
		Env:           noEnv,
		NoSystem:      true,
		GlobalFile:    filepath.Join(dir, "absent-global"),
		InitialBranch: branch,
	})
	if err != nil {
		t.Fatalf("Init returned error %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })
	return r
}

func openTestDB(t *testing.T, r *repo.Repository) *odb.DB {
	t.Helper()
	db, err := odb.Open(r.ObjectsDir(), odb.Options{})
	if err != nil {
		t.Fatalf("odb.Open returned error %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func openTestStore(t *testing.T, r *repo.Repository, peeler refs.ObjectPeeler) *refs.Store {
	t.Helper()
	store, err := refs.Open(refs.Options{
		GitDir:    r.GitDir(),
		CommonDir: r.CommonDir(),
		Committer: testSignature,
		Peeler:    peeler,
	})
	if err != nil {
		t.Fatalf("refs.Open returned error %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func setRef(t *testing.T, store *refs.Store, name refs.Name, target hash.ObjectID) {
	t.Helper()
	tx := store.Begin()
	if err := tx.Set(name, target); err != nil {
		t.Fatalf("Set(%s) returned error %v", name, err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit(%s) returned error %v", name, err)
	}
}

func putBlob(t *testing.T, db *odb.DB, content string) hash.ObjectID {
	t.Helper()
	id, err := db.PutObject(&object.Blob{Data: []byte(content)})
	if err != nil {
		t.Fatalf("PutObject(blob) returned error %v", err)
	}
	return id
}

func putTree(t *testing.T, db *odb.DB, entries ...object.TreeEntry) hash.ObjectID {
	t.Helper()
	tree := &object.Tree{Entries: entries}
	tree.Sort()
	id, err := db.PutObject(tree)
	if err != nil {
		t.Fatalf("PutObject(tree) returned error %v", err)
	}
	return id
}

func putCommit(t *testing.T, db *odb.DB, tree hash.ObjectID, when time.Time, author, message string, parents ...hash.ObjectID) hash.ObjectID {
	t.Helper()
	signature := object.Signature{Name: author, Email: author + "@example.com", When: when}
	commit := &object.Commit{
		Tree:      tree,
		Author:    signature,
		Committer: signature,
		Message:   message,
		Parents:   parents,
	}
	id, err := db.PutObject(commit)
	if err != nil {
		t.Fatalf("PutObject(commit) returned error %v", err)
	}
	return id
}

func putAnnotatedTag(t *testing.T, db *odb.DB, target hash.ObjectID, name string, when time.Time) hash.ObjectID {
	t.Helper()
	tagger := object.Signature{Name: "tagger", Email: "tagger@example.com", When: when}
	tag := &object.Tag{
		Object:     target,
		ObjectType: object.TypeCommit,
		Name:       name,
		Tagger:     &tagger,
		Message:    "tag " + name + "\n",
	}
	id, err := db.PutObject(tag)
	if err != nil {
		t.Fatalf("PutObject(tag) returned error %v", err)
	}
	return id
}

type countingObjects struct {
	inner revision.Objects
	gets  int
}

func (c *countingObjects) Get(id hash.ObjectID) (object.Type, []byte, error) {
	c.gets++
	return c.inner.Get(id)
}

type failingObjects struct {
	inner revision.Objects
	fail  hash.ObjectID
	err   error
}

func (f failingObjects) Get(id hash.ObjectID) (object.Type, []byte, error) {
	if id == f.fail {
		return 0, nil, f.err
	}
	return f.inner.Get(id)
}

type headErrorRefs struct {
	inner *refs.Store
	err   error
}

func (f headErrorRefs) Resolve(name refs.Name) (refs.Ref, error) {
	if name == refs.HEAD {
		return refs.Ref{}, f.err
	}
	return f.inner.Resolve(name)
}

func (f headErrorRefs) ResolveName(name refs.Name) (refs.Name, error) {
	return f.inner.ResolveName(name)
}

func (f headErrorRefs) Prefix(prefix string) iter.Seq2[refs.Ref, error] {
	return f.inner.Prefix(prefix)
}

func (f headErrorRefs) Reflog(name refs.Name) iter.Seq2[refs.ReflogEntry, error] {
	return f.inner.Reflog(name)
}
