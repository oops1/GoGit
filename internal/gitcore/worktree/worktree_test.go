package worktree

import (
	"errors"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/repo"
)

func TestOpenFailsWithoutObjectDatabase(t *testing.T) {
	tr := newTestRepo(t)
	tr.saveIndex()
	if _, err := Open(tr.repo, Options{}); !errors.Is(err, ErrNoObjectDatabase) {
		t.Fatalf("Open returned error %v, want ErrNoObjectDatabase", err)
	}
}

func TestOpenFailsForBareRepository(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir, repo.InitOptions{Bare: true})
	if err != nil {
		t.Fatalf("repo.Init returned error %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })
	db, err := odb.Open(r.ObjectsDir(), odb.Options{})
	if err != nil {
		t.Fatalf("odb.Open returned error %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := Open(r, Options{DB: db}); !errors.Is(err, ErrBareRepository) {
		t.Fatalf("Open returned error %v, want ErrBareRepository", err)
	}
}

func TestOpenSucceedsWithoutAnIndexFile(t *testing.T) {
	tr := newTestRepo(t)
	w, err := Open(tr.repo, Options{DB: tr.db, Refs: tr.refs})
	if err != nil {
		t.Fatalf("Open returned error %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })
	if w.Index().Len() != 0 {
		t.Fatalf("Index().Len() = %d, want 0", w.Index().Len())
	}
}

func TestOpenOpensItsOwnRefsStoreWhenNoneIsProvided(t *testing.T) {
	tr := newTestRepo(t)
	tr.stage("a.txt", "hello\n")
	tr.commit("initial")
	w, err := Open(tr.repo, Options{DB: tr.db})
	if err != nil {
		t.Fatalf("Open returned error %v", err)
	}
	defer func() {
		if err := w.Close(); err != nil {
			t.Fatalf("Close returned error %v", err)
		}
	}()
	status, err := w.Status(t.Context())
	if err != nil {
		t.Fatalf("Status returned error %v", err)
	}
	if status.HeadBranch != "main" {
		t.Fatalf("HeadBranch = %q, want %q", status.HeadBranch, "main")
	}
}

func TestRepositoryAndIndexAccessors(t *testing.T) {
	tr := newTestRepo(t)
	w := tr.open()
	if w.Repository() != tr.repo {
		t.Fatalf("Repository() returned an unexpected repository")
	}
	if w.Index() == nil {
		t.Fatalf("Index() returned nil")
	}
}
