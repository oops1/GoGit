package ops

import (
	"context"
	"errors"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/refs"
)

func TestSwitchBetweenBranches(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "hello\n")
	mustStage(t, r, "a.txt")
	first := r.commitAll("initial")
	r.createBranch("feature", first)
	if err := Switch(t.Context(), r.repo, "feature", SwitchOptions{}); err != nil {
		t.Fatalf("Switch returned error %v", err)
	}
	target, symbolic := r.headSymbolicTarget()
	if !symbolic || target != refs.BranchName("feature") {
		t.Fatalf("HEAD = %s symbolic=%v, want feature", target, symbolic)
	}
}

func TestSwitchWritesFilesFromTargetTree(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "hello\n")
	mustStage(t, r, "a.txt")
	first := r.commitAll("initial")
	r.createBranch("feature", first)
	if err := Switch(t.Context(), r.repo, "feature", SwitchOptions{}); err != nil {
		t.Fatalf("Switch returned error %v", err)
	}
	r.writeFile("b.txt", "world\n")
	mustStage(t, r, "b.txt")
	r.commitAll("on feature")

	if err := Switch(t.Context(), r.repo, "main", SwitchOptions{}); err != nil {
		t.Fatalf("Switch returned error %v", err)
	}
	if r.exists("b.txt") {
		t.Fatalf("b.txt should not exist on main")
	}
	if err := Switch(t.Context(), r.repo, "feature", SwitchOptions{}); err != nil {
		t.Fatalf("Switch returned error %v", err)
	}
	if got := r.readFile("b.txt"); got != "world\n" {
		t.Fatalf("b.txt = %q", got)
	}
}

func TestSwitchToDetachedCommit(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "hello\n")
	mustStage(t, r, "a.txt")
	first := r.commitAll("initial")
	if err := Switch(t.Context(), r.repo, first.String(), SwitchOptions{}); err != nil {
		t.Fatalf("Switch returned error %v", err)
	}
	store := r.refs()
	ref, err := store.Lookup(refs.HEAD)
	if err != nil {
		t.Fatalf("Lookup returned error %v", err)
	}
	if ref.IsSymbolic() {
		t.Fatalf("HEAD unexpectedly symbolic")
	}
	if ref.Target != first {
		t.Fatalf("HEAD = %s, want %s", ref.Target, first)
	}
}

func TestSwitchRefusesOnConflictingChanges(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "hello\n")
	mustStage(t, r, "a.txt")
	first := r.commitAll("initial")
	r.createBranch("feature", first)
	if err := Switch(t.Context(), r.repo, "feature", SwitchOptions{}); err != nil {
		t.Fatalf("Switch returned error %v", err)
	}
	r.writeFile("a.txt", "feature change\n")
	mustStage(t, r, "a.txt")
	r.commitAll("on feature")
	if err := Switch(t.Context(), r.repo, "main", SwitchOptions{}); err != nil {
		t.Fatalf("Switch returned error %v", err)
	}
	r.writeFile("a.txt", "dirty uncommitted change\n")

	err := Switch(t.Context(), r.repo, "feature", SwitchOptions{})
	var overwrite *OverwriteError
	if !errors.As(err, &overwrite) {
		t.Fatalf("err = %v, want *OverwriteError", err)
	}
	if len(overwrite.Paths) != 1 || overwrite.Paths[0] != "a.txt" {
		t.Fatalf("paths = %v", overwrite.Paths)
	}
	if !errors.Is(err, ErrWouldOverwrite) {
		t.Fatalf("errors.Is(err, ErrWouldOverwrite) = false")
	}
	if got := r.readFile("a.txt"); got != "dirty uncommitted change\n" {
		t.Fatalf("a.txt was modified: %q", got)
	}
}

func TestSwitchForceOverwritesChanges(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "hello\n")
	mustStage(t, r, "a.txt")
	first := r.commitAll("initial")
	r.createBranch("feature", first)
	if err := Switch(t.Context(), r.repo, "feature", SwitchOptions{}); err != nil {
		t.Fatalf("Switch returned error %v", err)
	}
	r.writeFile("a.txt", "feature change\n")
	mustStage(t, r, "a.txt")
	r.commitAll("on feature")
	if err := Switch(t.Context(), r.repo, "main", SwitchOptions{}); err != nil {
		t.Fatalf("Switch returned error %v", err)
	}
	r.writeFile("a.txt", "dirty uncommitted change\n")

	if err := Switch(t.Context(), r.repo, "feature", SwitchOptions{Force: true}); err != nil {
		t.Fatalf("Switch returned error %v", err)
	}
	if got := r.readFile("a.txt"); got != "feature change\n" {
		t.Fatalf("a.txt = %q, want feature change", got)
	}
}

func TestSwitchWritesReflog(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "hello\n")
	mustStage(t, r, "a.txt")
	first := r.commitAll("initial")
	r.createBranch("feature", first)
	if err := Switch(t.Context(), r.repo, "feature", SwitchOptions{}); err != nil {
		t.Fatalf("Switch returned error %v", err)
	}
	store := r.refs()
	last, err := store.ReflogLast(refs.HEAD)
	if err != nil {
		t.Fatalf("ReflogLast returned error %v", err)
	}
	want := "checkout: moving from main to feature"
	if last.Message != want {
		t.Fatalf("reflog message = %q, want %q", last.Message, want)
	}
}

func TestSwitchTargetNotFound(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "hello\n")
	mustStage(t, r, "a.txt")
	r.commitAll("initial")
	err := Switch(t.Context(), r.repo, "does-not-exist", SwitchOptions{})
	if !errors.Is(err, ErrTargetNotFound) {
		t.Fatalf("err = %v, want ErrTargetNotFound", err)
	}
}

func TestSwitchRemovesFilesNotInTarget(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "hello\n")
	mustStage(t, r, "a.txt")
	first := r.commitAll("initial")
	r.createBranch("feature", first)
	r.writeFile("b.txt", "world\n")
	mustStage(t, r, "b.txt")
	r.commitAll("on main")

	if err := Switch(t.Context(), r.repo, "feature", SwitchOptions{}); err != nil {
		t.Fatalf("Switch returned error %v", err)
	}
	if r.exists("b.txt") {
		t.Fatalf("b.txt should have been removed")
	}
	idx := r.index()
	if _, ok := entryOf(t, idx, "b.txt"); ok {
		t.Fatalf("b.txt should not be tracked on feature")
	}
}

func TestSwitchOnBareRepositoryReturnsError(t *testing.T) {
	r := newBareTestRepo(t)
	err := Switch(t.Context(), r.repo, "main", SwitchOptions{})
	if !errors.Is(err, ErrBareRepository) {
		t.Fatalf("err = %v, want ErrBareRepository", err)
	}
}

func TestSwitchContextCanceledReturnsError(t *testing.T) {
	r := newTestRepo(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err := Switch(ctx, r.repo, "main", SwitchOptions{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestSwitchLeavesUntrackedFilesAlone(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "hello\n")
	mustStage(t, r, "a.txt")
	first := r.commitAll("initial")
	r.createBranch("feature", first)
	r.writeFile("untracked.txt", "keep\n")
	if err := Switch(t.Context(), r.repo, "feature", SwitchOptions{}); err != nil {
		t.Fatalf("Switch returned error %v", err)
	}
	if !r.exists("untracked.txt") {
		t.Fatalf("untracked.txt should have been preserved")
	}
}
