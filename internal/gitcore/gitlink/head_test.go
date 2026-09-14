package gitlink

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/repo"
)

func nestedRepository(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "libs", "sub")
	global := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(global, nil, 0o666); err != nil {
		t.Fatal(err)
	}
	r, err := repo.Init(dir, repo.InitOptions{InitialBranch: "main", NoSystem: true, GlobalFile: global})
	if err != nil {
		t.Fatalf("Init returned error %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestHeadOfReadsTheCommitTheNestedRepositoryHasCheckedOut(t *testing.T) {
	dir := nestedRepository(t)
	want := hash.SumSHA1("commit", []byte("nested"))
	branch := filepath.Join(dir, ".git", "refs", "heads", "main")
	if err := os.WriteFile(branch, []byte(want.String()+"\n"), 0o666); err != nil {
		t.Fatal(err)
	}

	id, found, err := HeadOf(dir)

	if err != nil || !found || id != want {
		t.Fatalf("HeadOf = %s, %v, %v; want %s", id, found, err, want)
	}
}

func TestHeadOfReportsAnUnbornNestedRepository(t *testing.T) {
	dir := nestedRepository(t)

	_, found, err := HeadOf(dir)

	if !found || !errors.Is(err, ErrNoCommit) {
		t.Fatalf("HeadOf = %v, %v; want ErrNoCommit", found, err)
	}
}

func TestHeadOfFindsNothingInAPlainDirectory(t *testing.T) {
	_, found, err := HeadOf(t.TempDir())

	if found || err != nil {
		t.Fatalf("HeadOf = %v, %v", found, err)
	}
}

func TestHeadFailsWhenTheReferencesCannotBeOpened(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "absent")

	_, err := Head(repo.Layout{GitDir: missing, CommonDir: missing})

	if !errors.Is(err, ErrNoCommit) {
		t.Fatalf("Head = %v, want ErrNoCommit", err)
	}
}
