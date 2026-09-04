package ops

import (
	"errors"
	"testing"
)

func TestDiscardModifiedFileRestoresIndexVersion(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "hello\n")
	mustStage(t, r, "a.txt")
	r.commitAll("initial")
	r.writeFile("a.txt", "changed\n")
	if err := Discard(t.Context(), r.repo, []string{"a.txt"}, DiscardOptions{}); err != nil {
		t.Fatalf("Discard returned error %v", err)
	}
	if got := r.readFile("a.txt"); got != "hello\n" {
		t.Fatalf("a.txt = %q, want %q", got, "hello\n")
	}
}

func TestDiscardDeletedFileRestoresFile(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "hello\n")
	mustStage(t, r, "a.txt")
	r.commitAll("initial")
	r.remove("a.txt")
	if err := Discard(t.Context(), r.repo, []string{"a.txt"}, DiscardOptions{}); err != nil {
		t.Fatalf("Discard returned error %v", err)
	}
	if got := r.readFile("a.txt"); got != "hello\n" {
		t.Fatalf("a.txt = %q, want %q", got, "hello\n")
	}
}

func TestDiscardDirectory(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("dir/a.txt", "a\n")
	r.writeFile("dir/b.txt", "b\n")
	mustStage(t, r, "dir")
	r.commitAll("initial")
	r.writeFile("dir/a.txt", "changed\n")
	r.remove("dir/b.txt")
	if err := Discard(t.Context(), r.repo, []string{"dir"}, DiscardOptions{}); err != nil {
		t.Fatalf("Discard returned error %v", err)
	}
	if got := r.readFile("dir/a.txt"); got != "a\n" {
		t.Fatalf("dir/a.txt = %q, want %q", got, "a\n")
	}
	if got := r.readFile("dir/b.txt"); got != "b\n" {
		t.Fatalf("dir/b.txt = %q, want %q", got, "b\n")
	}
}

func TestDiscardLeavesUntrackedFilesByDefault(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "hello\n")
	mustStage(t, r, "a.txt")
	r.commitAll("initial")
	r.writeFile("untracked.txt", "keep me\n")
	if err := Discard(t.Context(), r.repo, []string{"untracked.txt"}, DiscardOptions{}); err != nil {
		t.Fatalf("Discard returned error %v", err)
	}
	if !r.exists("untracked.txt") {
		t.Fatalf("untracked.txt was removed")
	}
}

func TestDiscardRemovesUntrackedWhenRequested(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "hello\n")
	mustStage(t, r, "a.txt")
	r.commitAll("initial")
	r.writeFile("dir/untracked.txt", "junk\n")
	if err := Discard(t.Context(), r.repo, []string{"dir"}, DiscardOptions{RemoveUntracked: true}); err != nil {
		t.Fatalf("Discard returned error %v", err)
	}
	if r.exists("dir/untracked.txt") {
		t.Fatalf("dir/untracked.txt should have been removed")
	}
	if r.exists("dir") {
		t.Fatalf("dir should have been pruned once empty")
	}
}

func TestDiscardRemoveUntrackedPreservesIgnoredFiles(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile(".gitignore", "*.log\n")
	mustStage(t, r, ".gitignore")
	r.commitAll("initial")
	r.writeFile("dir/keep.log", "log\n")
	if err := Discard(t.Context(), r.repo, []string{"dir"}, DiscardOptions{RemoveUntracked: true}); err != nil {
		t.Fatalf("Discard returned error %v", err)
	}
	if !r.exists("dir/keep.log") {
		t.Fatalf("ignored file dir/keep.log should have been preserved")
	}
}

func TestDiscardEOLConvertOnCheckout(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile(".gitattributes", "*.txt text=auto eol=crlf\n")
	mustStage(t, r, ".gitattributes")
	r.writeFile("a.txt", "line1\r\nline2\r\n")
	mustStage(t, r, "a.txt")
	r.commitAll("initial")
	r.remove("a.txt")
	if err := Discard(t.Context(), r.repo, []string{"a.txt"}, DiscardOptions{}); err != nil {
		t.Fatalf("Discard returned error %v", err)
	}
	if got := r.readFile("a.txt"); got != "line1\r\nline2\r\n" {
		t.Fatalf("a.txt = %q, want CRLF content", got)
	}
}

func TestDiscardOnBareRepositoryReturnsError(t *testing.T) {
	r := newBareTestRepo(t)
	err := Discard(t.Context(), r.repo, []string{"a.txt"}, DiscardOptions{})
	if !errors.Is(err, ErrBareRepository) {
		t.Fatalf("err = %v, want ErrBareRepository", err)
	}
}

func TestDiscardInvalidPathReturnsError(t *testing.T) {
	r := newTestRepo(t)
	err := Discard(t.Context(), r.repo, []string{""}, DiscardOptions{})
	if !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("err = %v, want ErrInvalidPath", err)
	}
}
