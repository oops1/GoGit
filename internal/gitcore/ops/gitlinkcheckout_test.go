package ops

import (
	"errors"
	"os"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/index"
	"github.com/oops1/gogit/internal/gitcore/object"
)

func requireDirectory(t *testing.T, r *testRepo, rel string) {
	t.Helper()
	info, err := os.Lstat(r.path(rel))
	if err != nil || !info.IsDir() {
		t.Fatalf("%s is not a directory: %v, %v", rel, info, err)
	}
}

func gitlinkCommit(t *testing.T, r *testRepo, parent, pointer hash.ObjectID) hash.ObjectID {
	t.Helper()
	libs := putTree(t, r, object.TreeEntry{Mode: object.ModeSubmodule, Name: "sub", ID: pointer})
	root := putTree(t, r,
		object.TreeEntry{Mode: object.ModeBlob, Name: "a.txt", ID: storedBlob(t, r, "hello\n")},
		object.TreeEntry{Mode: object.ModeTree, Name: "libs", ID: libs},
	)
	return putCommit(t, r, root, parent)
}

func TestSwitchCreatesTheDirectoryOfASubmodule(t *testing.T) {
	r := newTestRepo(t)
	main := r.initialCommit()
	r.createBranch("withsub", gitlinkCommit(t, r, main, main))

	if err := Switch(t.Context(), r.repo, "withsub", SwitchOptions{}); err != nil {
		t.Fatalf("Switch returned error %v", err)
	}

	requireDirectory(t, r, "libs/sub")
	if entry, ok := entryOf(t, r.index(), "libs/sub"); !ok || entry.Mode != object.ModeSubmodule || entry.ID != main {
		t.Fatalf("libs/sub = %+v, %v", entry, ok)
	}
}

func TestSwitchMovesASubmoduleWhoseDirectoryIsMissing(t *testing.T) {
	r := newTestRepo(t)
	main := r.initialCommit()
	moved := hash.SumSHA1("commit", []byte("moved"))
	r.createBranch("one", gitlinkCommit(t, r, main, main))
	r.createBranch("two", gitlinkCommit(t, r, main, moved))
	r.switchTo("one")
	if err := os.Remove(r.path("libs/sub")); err != nil {
		t.Fatal(err)
	}

	if err := Switch(t.Context(), r.repo, "two", SwitchOptions{}); err != nil {
		t.Fatalf("Switch returned error %v", err)
	}

	requireDirectory(t, r, "libs/sub")
	if entry, ok := entryOf(t, r.index(), "libs/sub"); !ok || entry.ID != moved {
		t.Fatalf("libs/sub = %+v, %v", entry, ok)
	}
}

func TestAGitlinkDirectoryReplacesAFileInItsWay(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("sub", "in the way\n")
	wt, err := openWorkingTree(r.repo)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = wt.close() })

	if err := writeGitlinkDirectory(wt, "sub"); err != nil {
		t.Fatalf("writeGitlinkDirectory returned error %v", err)
	}

	requireDirectory(t, r, "sub")
}

func TestAGitlinkDirectoryIsNeverWrittenInsideTheGitDirectory(t *testing.T) {
	r := newTestRepo(t)
	wt, err := openWorkingTree(r.repo)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = wt.close() })

	if err := writeGitlinkDirectory(wt, ".git/sub"); !errors.Is(err, index.ErrUnsafePath) {
		t.Fatalf("writeGitlinkDirectory = %v, want ErrUnsafePath", err)
	}
}

func TestDiscardRecreatesAMissingSubmoduleDirectory(t *testing.T) {
	r := newTestRepo(t)
	main := r.initialCommit()
	idx := r.index()
	idx.Add(index.Entry{Path: "libs/sub", Mode: object.ModeSubmodule, ID: main, Stage: index.StageMerged})
	r.saveIndex(idx)

	if err := Discard(t.Context(), r.repo, []string{"libs"}, DiscardOptions{}); err != nil {
		t.Fatalf("Discard returned error %v", err)
	}

	requireDirectory(t, r, "libs/sub")
}
