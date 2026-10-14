package app

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/diff"
	"github.com/oops1/gogit/internal/gitcore/hash"
)

func cutHistoryBelow(t *testing.T, target string, boundary, parent hash.ObjectID) {
	t.Helper()
	gitDir := filepath.Join(target, ".git")
	if err := os.WriteFile(filepath.Join(gitDir, "shallow"), []byte(boundary.String()+"\n"), 0o666); err != nil {
		t.Fatal(err)
	}
	hex := parent.String()
	if err := os.Remove(filepath.Join(gitDir, "objects", hex[:2], hex[2:])); err != nil {
		t.Fatal(err)
	}
}

func TestTheFilesOfAShallowBoundaryCommitAreShownAsAdded(t *testing.T) {
	isolateGitConfig(t)
	target := filepath.Join(t.TempDir(), "repo")
	initTestRepoWithBranch(t, target, "main")
	parent := commitTestFiles(t, target, map[string]string{"old.txt": "old\n"}, nil)
	boundary := commitTestFiles(t, target, map[string]string{"new.txt": "new\n"}, nil)
	cutHistoryBelow(t, target, boundary, parent)

	a := newTestApp(t)
	o := openTestRepository(t, target)
	a.setOpened(o)
	files, err := a.loadDiffFiles(t.Context(), o.db, boundary)
	if err != nil {
		t.Fatalf("the files of the commit at the shallow boundary failed to load: %v", err)
	}
	var added []string
	for _, f := range files {
		if f.Status == diff.StatusAdded {
			added = append(added, f.NewPath)
		}
	}
	slices.Sort(added)
	if !slices.Equal(added, []string{"new.txt", "old.txt"}) {
		t.Fatalf("added = %v, want the whole tree of a commit with no history below it", added)
	}
}

func TestNoCommitIsAShallowBoundaryWhileNoRepositoryIsOpen(t *testing.T) {
	a := newTestApp(t)
	if a.shallowBoundary(hash.Zero) {
		t.Fatal("an app with no repository called a commit a shallow boundary")
	}
}
