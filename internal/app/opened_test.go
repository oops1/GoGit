package app

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/refs"
	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
)

func TestOpenRepositoryAtReadsTheShallowFile(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	initTestRepoWithBranch(t, target, "main")
	db, store := withJournalRepo(t, target)
	tree := putJournalTree(t, db)
	missingParent := hash.SumSHA1("commit", []byte("gone"))
	tip := putJournalCommit(t, db, tree, time.Now(), "tip\n", missingParent)
	setRef(t, store, refs.BranchName("main"), tip)
	writeShallowFile(t, target, tip)

	o := openTestRepository(t, target)

	if len(o.shallow) != 1 {
		t.Fatalf("shallow set has %d entries, want 1", len(o.shallow))
	}
	if _, ok := o.shallow[tip]; !ok {
		t.Fatalf("shallow set = %v, want it to contain %s", o.shallow, tip)
	}
}

func TestOpenRepositoryAtHasAnEmptyShallowSetForARegularRepository(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	initTestRepoWithBranch(t, target, "main")

	o := openTestRepository(t, target)

	if len(o.shallow) != 0 {
		t.Fatalf("shallow set = %v, want it empty", o.shallow)
	}
}

func TestOpenRepositoryAtFailsAndCleansUpWhenTheShallowReadFails(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main")
	initTestRepoWithBranch(t, target, "main")
	prev := readRepositoryShallow
	readRepositoryShallow = func(*gitrepo.Repository) (map[hash.ObjectID]struct{}, error) {
		return nil, errors.New("boom")
	}
	t.Cleanup(func() { readRepositoryShallow = prev })

	if _, _, err := openRepositoryAt("r1", target); err == nil || err.Error() != "boom" {
		t.Fatalf("openRepositoryAt returned error %v, want boom", err)
	}
}
