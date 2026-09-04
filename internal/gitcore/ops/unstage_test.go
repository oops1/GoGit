package ops

import (
	"errors"
	"testing"
)

func TestUnstageAddedFileRemovesFromIndex(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "hello\n")
	mustStage(t, r, "a.txt")
	r.commitAll("initial")
	r.writeFile("new.txt", "new\n")
	mustStage(t, r, "new.txt")
	if err := Unstage(t.Context(), r.repo, []string{"new.txt"}); err != nil {
		t.Fatalf("Unstage returned error %v", err)
	}
	idx := r.index()
	if _, ok := entryOf(t, idx, "new.txt"); ok {
		t.Fatalf("new.txt is still staged")
	}
}

func TestUnstageModifiedFileRestoresHeadVersion(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "hello\n")
	mustStage(t, r, "a.txt")
	headID := r.commitAll("initial")
	r.writeFile("a.txt", "goodbye\n")
	mustStage(t, r, "a.txt")
	if err := Unstage(t.Context(), r.repo, []string{"a.txt"}); err != nil {
		t.Fatalf("Unstage returned error %v", err)
	}
	idx := r.index()
	entry, ok := entryOf(t, idx, "a.txt")
	if !ok {
		t.Fatalf("a.txt was removed from index")
	}
	db := r.db()
	commit, err := db.Commit(headID)
	if err != nil {
		t.Fatalf("Commit returned error %v", err)
	}
	tree, err := db.Tree(commit.Tree)
	if err != nil {
		t.Fatalf("Tree returned error %v", err)
	}
	want, _ := tree.Find("a.txt")
	if entry.ID != want.ID {
		t.Fatalf("entry.ID = %s, want %s", entry.ID, want.ID)
	}
}

func TestUnstageDirectory(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("dir/a.txt", "a\n")
	r.writeFile("dir/b.txt", "b\n")
	mustStage(t, r, "dir")
	r.commitAll("initial")
	r.writeFile("dir/a.txt", "changed\n")
	mustStage(t, r, "dir")
	r.writeFile("dir/c.txt", "new\n")
	mustStage(t, r, "dir")
	if err := Unstage(t.Context(), r.repo, []string{"dir"}); err != nil {
		t.Fatalf("Unstage returned error %v", err)
	}
	idx := r.index()
	if _, ok := entryOf(t, idx, "dir/c.txt"); ok {
		t.Fatalf("dir/c.txt should have been unstaged")
	}
	entry, _ := entryOf(t, idx, "dir/a.txt")
	db := r.db()
	_, data, err := db.Get(entry.ID)
	if err != nil {
		t.Fatalf("Get returned error %v", err)
	}
	if string(data) != "a\n" {
		t.Fatalf("dir/a.txt content = %q, want original", data)
	}
}

func TestUnstageOnUnbornHeadRemovesFromIndex(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "hello\n")
	mustStage(t, r, "a.txt")
	if err := Unstage(t.Context(), r.repo, []string{"a.txt"}); err != nil {
		t.Fatalf("Unstage returned error %v", err)
	}
	idx := r.index()
	if idx.Len() != 0 {
		t.Fatalf("index still has %d entries", idx.Len())
	}
}

func TestUnstageNoopWhenNothingTracked(t *testing.T) {
	r := newTestRepo(t)
	if err := Unstage(t.Context(), r.repo, []string{"missing.txt"}); err != nil {
		t.Fatalf("Unstage returned error %v", err)
	}
}

func TestUnstageInvalidPathReturnsError(t *testing.T) {
	r := newTestRepo(t)
	err := Unstage(t.Context(), r.repo, []string{".."})
	if !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("err = %v, want ErrInvalidPath", err)
	}
}

func TestUnstageWhileIndexLockedReturnsError(t *testing.T) {
	r := newTestRepo(t)
	lock, err := lockIndex(r.repo)
	if err != nil {
		t.Fatalf("lockIndex returned error %v", err)
	}
	defer lock.abort()
	err = Unstage(t.Context(), r.repo, []string{"a.txt"})
	if !errors.Is(err, ErrIndexLocked) {
		t.Fatalf("err = %v, want ErrIndexLocked", err)
	}
}
