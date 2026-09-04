package worktree

import (
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

type testRepo struct {
	t     testing.TB
	dir   string
	repo  *repo.Repository
	db    *odb.DB
	refs  *refs.Store
	idx   *index.Index
	clock int64
}

func newTestRepo(t testing.TB) *testRepo {
	t.Helper()
	dir := t.TempDir()
	r, err := repo.Init(dir, repo.InitOptions{InitialBranch: "main"})
	if err != nil {
		t.Fatalf("repo.Init returned error %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })
	db, err := odb.Open(r.ObjectsDir(), odb.Options{})
	if err != nil {
		t.Fatalf("odb.Open returned error %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store, err := refs.Open(refs.Options{
		GitDir:    r.GitDir(),
		CommonDir: r.CommonDir(),
		Committer: func() object.Signature {
			return object.Signature{Name: "ann", Email: "ann@example.com", When: time.Unix(1700000000, 0)}
		},
	})
	if err != nil {
		t.Fatalf("refs.Open returned error %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return &testRepo{t: t, dir: dir, repo: r, db: db, refs: store, idx: index.New(index.Version2), clock: 1700000000}
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

func (r *testRepo) mkdir(rel string) {
	r.t.Helper()
	if err := os.MkdirAll(r.path(rel), 0o777); err != nil {
		r.t.Fatalf("MkdirAll returned error %v", err)
	}
}

func (r *testRepo) remove(rel string) {
	r.t.Helper()
	if err := os.Remove(r.path(rel)); err != nil {
		r.t.Fatalf("Remove returned error %v", err)
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

func (r *testRepo) stage(rel, content string) {
	r.t.Helper()
	r.writeFile(rel, content)
	r.stageExisting(rel, object.ModeBlob)
}

func (r *testRepo) stageExisting(rel string, mode object.Mode) {
	r.t.Helper()
	full := r.path(rel)
	fi, err := os.Lstat(full)
	if err != nil {
		r.t.Fatalf("Lstat returned error %v", err)
	}
	var id hash.ObjectID
	if mode.IsSymlink() {
		target, err := os.Readlink(full)
		if err != nil {
			r.t.Fatalf("Readlink returned error %v", err)
		}
		id, err = r.db.Put(object.TypeBlob, []byte(target))
		if err != nil {
			r.t.Fatalf("Put returned error %v", err)
		}
	} else {
		data, err := os.ReadFile(full)
		if err != nil {
			r.t.Fatalf("ReadFile returned error %v", err)
		}
		id, err = r.db.Put(object.TypeBlob, data)
		if err != nil {
			r.t.Fatalf("Put returned error %v", err)
		}
	}
	r.idx.Add(index.Entry{
		Path: rel,
		Mode: mode,
		ID:   id,
		Stat: index.Stat{MTime: fi.ModTime(), CTime: fi.ModTime(), Size: uint32(fi.Size())},
	})
}

func (r *testRepo) stageBlob(rel, content string, stage index.Stage) hash.ObjectID {
	r.t.Helper()
	id, err := r.db.Put(object.TypeBlob, []byte(content))
	if err != nil {
		r.t.Fatalf("Put returned error %v", err)
	}
	r.idx.Add(index.Entry{Path: rel, Mode: object.ModeBlob, ID: id, Stage: stage})
	return id
}

func (r *testRepo) unstage(rel string) {
	r.t.Helper()
	r.idx.Remove(rel)
}

func (r *testRepo) saveIndex() {
	r.t.Helper()
	if err := r.idx.WriteFile(r.repo.IndexFile(), index.Version2); err != nil {
		r.t.Fatalf("WriteFile returned error %v", err)
	}
}

func (r *testRepo) headCommit() hash.ObjectID {
	ref, err := r.refs.Resolve(refs.HEAD)
	if err != nil {
		return hash.Zero
	}
	return ref.Target
}

func (r *testRepo) commit(message string) hash.ObjectID {
	r.t.Helper()
	treeID, err := r.idx.WriteTree(r.db)
	if err != nil {
		r.t.Fatalf("WriteTree returned error %v", err)
	}
	r.clock += 60
	sig := object.Signature{Name: "ann", Email: "ann@example.com", When: time.Unix(r.clock, 0)}
	commit := &object.Commit{Tree: treeID, Author: sig, Committer: sig, Message: message + "\n"}
	if parent := r.headCommit(); !parent.IsZero() {
		commit.Parents = []hash.ObjectID{parent}
	}
	id, err := r.db.PutObject(commit)
	if err != nil {
		r.t.Fatalf("PutObject returned error %v", err)
	}
	tx := r.refs.Begin()
	if err := tx.Set(refs.BranchName("main"), id); err != nil {
		r.t.Fatalf("Set returned error %v", err)
	}
	if err := tx.Commit(); err != nil {
		r.t.Fatalf("Commit returned error %v", err)
	}
	return id
}

func (r *testRepo) commitWithTree(treeID hash.ObjectID, message string) hash.ObjectID {
	r.t.Helper()
	r.clock += 60
	sig := object.Signature{Name: "ann", Email: "ann@example.com", When: time.Unix(r.clock, 0)}
	commit := &object.Commit{Tree: treeID, Author: sig, Committer: sig, Message: message + "\n"}
	id, err := r.db.PutObject(commit)
	if err != nil {
		r.t.Fatalf("PutObject returned error %v", err)
	}
	tx := r.refs.Begin()
	if err := tx.Set(refs.BranchName("main"), id); err != nil {
		r.t.Fatalf("Set returned error %v", err)
	}
	if err := tx.Commit(); err != nil {
		r.t.Fatalf("Commit returned error %v", err)
	}
	return id
}

func (r *testRepo) setBranchTarget(id hash.ObjectID) {
	r.t.Helper()
	tx := r.refs.Begin()
	if err := tx.Set(refs.BranchName("main"), id); err != nil {
		r.t.Fatalf("Set returned error %v", err)
	}
	if err := tx.Commit(); err != nil {
		r.t.Fatalf("Commit returned error %v", err)
	}
}

func (r *testRepo) open() *Worktree {
	r.t.Helper()
	r.saveIndex()
	w, err := Open(r.repo, Options{DB: r.db, Refs: r.refs})
	if err != nil {
		r.t.Fatalf("Open returned error %v", err)
	}
	r.t.Cleanup(func() { _ = w.Close() })
	return w
}

func (r *testRepo) openWith(opts Options) *Worktree {
	r.t.Helper()
	r.saveIndex()
	opts.DB = r.db
	if opts.Refs == nil {
		opts.Refs = r.refs
	}
	w, err := Open(r.repo, opts)
	if err != nil {
		r.t.Fatalf("Open returned error %v", err)
	}
	r.t.Cleanup(func() { _ = w.Close() })
	return w
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
	reopened, err := repo.Open(r.dir, repo.OpenOptions{})
	if err != nil {
		r.t.Fatalf("repo.Open returned error %v", err)
	}
	r.t.Cleanup(func() { _ = reopened.Close() })
	return reopened
}

func entryMap(entries []Entry) map[string]Entry {
	out := make(map[string]Entry, len(entries))
	for _, entry := range entries {
		out[entry.Path] = entry
	}
	return out
}
