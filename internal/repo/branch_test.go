package repo

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/refs"
	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
)

func branchTestCommitter() object.Signature {
	return object.Signature{
		Name:  "Go Git",
		Email: "gogit@example.com",
		When:  time.Unix(1700000000, 0).UTC(),
	}
}

func initBranchTestRepo(t *testing.T, path, branch string) {
	t.Helper()
	r, err := gitrepo.Init(path, gitrepo.InitOptions{InitialBranch: branch})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
}

func branchTestObjectID(t *testing.T, seed string) hash.ObjectID {
	t.Helper()
	id, err := hash.Parse(seed + strings.Repeat("0", hash.HexSize-len(seed)))
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestCurrentBranchReturnsTheShortBranchNameForARepositoryOnABranch(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	initBranchTestRepo(t, target, "develop")

	if got := CurrentBranch(target); got != "develop" {
		t.Fatalf("CurrentBranch = %q, want %q", got, "develop")
	}
}

func TestCurrentBranchReturnsTheShortHashForADetachedHead(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	initBranchTestRepo(t, target, "main")
	target1 := branchTestObjectID(t, "abc123")

	store, err := refs.Open(refs.Options{GitDir: filepath.Join(target, ".git"), Committer: branchTestCommitter})
	if err != nil {
		t.Fatal(err)
	}
	tx := store.Begin()
	if err := tx.Detach(refs.HEAD, target1); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	want := target1.String()[:branchShortHashLength]
	if got := CurrentBranch(target); got != want {
		t.Fatalf("CurrentBranch = %q, want %q", got, want)
	}
}

func TestCurrentBranchReturnsEmptyForAMissingRepository(t *testing.T) {
	dir := t.TempDir()
	if got := CurrentBranch(filepath.Join(dir, "not-a-repo")); got != "" {
		t.Fatalf("CurrentBranch = %q, want empty", got)
	}
}

func TestCurrentBranchReturnsEmptyWhenTheRefsStoreFailsToOpen(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	initBranchTestRepo(t, target, "main")
	prev := openBranchRefsStore
	openBranchRefsStore = func(refs.Options) (*refs.Store, error) { return nil, errors.New("boom") }
	t.Cleanup(func() { openBranchRefsStore = prev })

	if got := CurrentBranch(target); got != "" {
		t.Fatalf("CurrentBranch = %q, want empty", got)
	}
}

func TestCurrentBranchReturnsEmptyWhenTheRepositoryFailsToOpen(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	initBranchTestRepo(t, target, "main")
	prev := openBranchRepository
	openBranchRepository = func(string, gitrepo.OpenOptions) (*gitrepo.Repository, error) {
		return nil, errors.New("boom")
	}
	t.Cleanup(func() { openBranchRepository = prev })

	if got := CurrentBranch(target); got != "" {
		t.Fatalf("CurrentBranch = %q, want empty", got)
	}
}

func TestCurrentBranchReturnsEmptyWhenHeadIsMalformed(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	initBranchTestRepo(t, target, "main")
	headFile := filepath.Join(target, ".git", "HEAD")
	if err := os.WriteFile(headFile, []byte("ref: refs/heads/.bad\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if got := CurrentBranch(target); got != "" {
		t.Fatalf("CurrentBranch = %q, want empty", got)
	}
}
