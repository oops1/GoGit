package ops

import (
	"context"
	"errors"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/refs"
)

func TestCreateBranch(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "hello\n")
	mustStage(t, r, "a.txt")
	first := r.commitAll("initial")
	if err := CreateBranch(t.Context(), r.repo, "feature", first, CreateBranchOptions{}); err != nil {
		t.Fatalf("CreateBranch returned error %v", err)
	}
	if got := r.branchTarget("feature"); got != first {
		t.Fatalf("feature = %s, want %s", got, first)
	}
}

func TestCreateBranchAlreadyExists(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "hello\n")
	mustStage(t, r, "a.txt")
	first := r.commitAll("initial")
	if err := CreateBranch(t.Context(), r.repo, "feature", first, CreateBranchOptions{}); err != nil {
		t.Fatalf("CreateBranch returned error %v", err)
	}
	err := CreateBranch(t.Context(), r.repo, "feature", first, CreateBranchOptions{})
	if !errors.Is(err, ErrBranchExists) {
		t.Fatalf("err = %v, want ErrBranchExists", err)
	}
}

func TestCreateBranchForceOverwrites(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "hello\n")
	mustStage(t, r, "a.txt")
	first := r.commitAll("initial")
	r.writeFile("b.txt", "world\n")
	mustStage(t, r, "b.txt")
	second := r.commitAll("second")
	r.createBranch("feature", first)
	if err := CreateBranch(t.Context(), r.repo, "feature", second, CreateBranchOptions{Force: true}); err != nil {
		t.Fatalf("CreateBranch returned error %v", err)
	}
	if got := r.branchTarget("feature"); got != second {
		t.Fatalf("feature = %s, want %s", got, second)
	}
}

func TestCreateBranchInvalidName(t *testing.T) {
	r := newTestRepo(t)
	err := CreateBranch(t.Context(), r.repo, "bad..name", hash.Zero, CreateBranchOptions{})
	if !errors.Is(err, ErrInvalidBranchName) {
		t.Fatalf("err = %v, want ErrInvalidBranchName", err)
	}
}

func TestDeleteBranch(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "hello\n")
	mustStage(t, r, "a.txt")
	first := r.commitAll("initial")
	r.createBranch("feature", first)
	if err := DeleteBranch(t.Context(), r.repo, "feature", false); err != nil {
		t.Fatalf("DeleteBranch returned error %v", err)
	}
	store := r.refs()
	if _, err := store.Lookup(refs.BranchName("feature")); !errors.Is(err, refs.ErrNotFound) {
		t.Fatalf("feature still exists: %v", err)
	}
}

func TestDeleteBranchCurrentReturnsError(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "hello\n")
	mustStage(t, r, "a.txt")
	r.commitAll("initial")
	err := DeleteBranch(t.Context(), r.repo, "main", false)
	if !errors.Is(err, ErrBranchCheckedOut) {
		t.Fatalf("err = %v, want ErrBranchCheckedOut", err)
	}
}

func TestDeleteBranchNotMergedReturnsError(t *testing.T) {
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
	err := DeleteBranch(t.Context(), r.repo, "feature", false)
	if !errors.Is(err, ErrBranchNotMerged) {
		t.Fatalf("err = %v, want ErrBranchNotMerged", err)
	}
}

func TestDeleteBranchForceDeletesUnmerged(t *testing.T) {
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
	if err := DeleteBranch(t.Context(), r.repo, "feature", true); err != nil {
		t.Fatalf("DeleteBranch returned error %v", err)
	}
}

func TestDeleteBranchNotFound(t *testing.T) {
	r := newTestRepo(t)
	err := DeleteBranch(t.Context(), r.repo, "missing", true)
	if !errors.Is(err, ErrBranchNotFound) {
		t.Fatalf("err = %v, want ErrBranchNotFound", err)
	}
}

func TestRenameBranch(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "hello\n")
	mustStage(t, r, "a.txt")
	first := r.commitAll("initial")
	r.createBranch("feature", first)
	if err := RenameBranch(t.Context(), r.repo, "feature", "renamed", false); err != nil {
		t.Fatalf("RenameBranch returned error %v", err)
	}
	if got := r.branchTarget("renamed"); got != first {
		t.Fatalf("renamed = %s, want %s", got, first)
	}
	store := r.refs()
	if _, err := store.Lookup(refs.BranchName("feature")); !errors.Is(err, refs.ErrNotFound) {
		t.Fatalf("feature still exists: %v", err)
	}
}

func TestRenameBranchUpdatesHeadWhenCurrent(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "hello\n")
	mustStage(t, r, "a.txt")
	r.commitAll("initial")
	if err := RenameBranch(t.Context(), r.repo, "main", "trunk", false); err != nil {
		t.Fatalf("RenameBranch returned error %v", err)
	}
	target, symbolic := r.headSymbolicTarget()
	if !symbolic || target != refs.BranchName("trunk") {
		t.Fatalf("HEAD = %s symbolic=%v, want trunk", target, symbolic)
	}
}

func TestRenameBranchTargetExists(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "hello\n")
	mustStage(t, r, "a.txt")
	first := r.commitAll("initial")
	r.createBranch("feature", first)
	r.createBranch("other", first)
	err := RenameBranch(t.Context(), r.repo, "feature", "other", false)
	if !errors.Is(err, ErrBranchExists) {
		t.Fatalf("err = %v, want ErrBranchExists", err)
	}
}

func TestRenameBranchForceOverwritesTarget(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "hello\n")
	mustStage(t, r, "a.txt")
	first := r.commitAll("initial")
	r.createBranch("feature", first)
	r.createBranch("other", first)
	if err := RenameBranch(t.Context(), r.repo, "feature", "other", true); err != nil {
		t.Fatalf("RenameBranch returned error %v", err)
	}
}

func TestRenameBranchSourceNotFound(t *testing.T) {
	r := newTestRepo(t)
	err := RenameBranch(t.Context(), r.repo, "missing", "renamed", false)
	if !errors.Is(err, ErrBranchNotFound) {
		t.Fatalf("err = %v, want ErrBranchNotFound", err)
	}
}

func TestRenameBranchInvalidName(t *testing.T) {
	r := newTestRepo(t)
	err := RenameBranch(t.Context(), r.repo, "main", "bad..name", false)
	if !errors.Is(err, ErrInvalidBranchName) {
		t.Fatalf("err = %v, want ErrInvalidBranchName", err)
	}
}

func TestCreateBranchContextCanceledReturnsError(t *testing.T) {
	r := newTestRepo(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err := CreateBranch(ctx, r.repo, "feature", hash.Zero, CreateBranchOptions{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}
