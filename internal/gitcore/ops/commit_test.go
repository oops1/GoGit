package ops

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/refs"
)

func TestCommitFirstCommitOnUnbornHead(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "hello\n")
	mustStage(t, r, "a.txt")
	id, err := Commit(t.Context(), r.repo, CommitOptions{Message: "initial"})
	if err != nil {
		t.Fatalf("Commit returned error %v", err)
	}
	db := r.db()
	commit, err := db.Commit(id)
	if err != nil {
		t.Fatalf("Commit lookup returned error %v", err)
	}
	if len(commit.Parents) != 0 {
		t.Fatalf("parents = %v, want none", commit.Parents)
	}
	if commit.Author.Name != "ann" || commit.Author.Email != "ann@example.com" {
		t.Fatalf("author = %+v", commit.Author)
	}
	if commit.Message != "initial\n" {
		t.Fatalf("message = %q", commit.Message)
	}
	store := r.refs()
	ref, err := store.Resolve(refs.HEAD)
	if err != nil {
		t.Fatalf("Resolve returned error %v", err)
	}
	if ref.Target != id {
		t.Fatalf("branch head = %s, want %s", ref.Target, id)
	}
}

func TestCommitNormalCommitHasParent(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "hello\n")
	mustStage(t, r, "a.txt")
	first := r.commitAll("initial")
	r.writeFile("b.txt", "world\n")
	mustStage(t, r, "b.txt")
	id, err := Commit(t.Context(), r.repo, CommitOptions{Message: "second"})
	if err != nil {
		t.Fatalf("Commit returned error %v", err)
	}
	db := r.db()
	commit, err := db.Commit(id)
	if err != nil {
		t.Fatalf("Commit lookup returned error %v", err)
	}
	if len(commit.Parents) != 1 || commit.Parents[0] != first {
		t.Fatalf("parents = %v, want [%s]", commit.Parents, first)
	}
}

func TestCommitAmendReplacesHeadKeepingGrandparent(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "hello\n")
	mustStage(t, r, "a.txt")
	grandparent := r.commitAll("initial")
	r.writeFile("b.txt", "world\n")
	mustStage(t, r, "b.txt")
	original := r.commitAll("second")
	r.remove("b.txt")
	mustStage(t, r, "b.txt")
	r.writeFile("c.txt", "amended\n")
	mustStage(t, r, "c.txt")
	id, err := Commit(t.Context(), r.repo, CommitOptions{Message: "second amended", Amend: true})
	if err != nil {
		t.Fatalf("Commit returned error %v", err)
	}
	if id == original {
		t.Fatalf("amended commit has the same id as the original")
	}
	db := r.db()
	commit, err := db.Commit(id)
	if err != nil {
		t.Fatalf("Commit lookup returned error %v", err)
	}
	if len(commit.Parents) != 1 || commit.Parents[0] != grandparent {
		t.Fatalf("parents = %v, want [%s]", commit.Parents, grandparent)
	}
	tree, err := db.Tree(commit.Tree)
	if err != nil {
		t.Fatalf("Tree returned error %v", err)
	}
	names := map[string]bool{}
	for _, entry := range tree.Entries {
		names[entry.Name] = true
	}
	if !names["a.txt"] || !names["c.txt"] || names["b.txt"] {
		t.Fatalf("tree entries = %v", names)
	}
}

func TestCommitAmendWithoutHeadReturnsError(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "hello\n")
	mustStage(t, r, "a.txt")
	_, err := Commit(t.Context(), r.repo, CommitOptions{Message: "amend", Amend: true})
	if !errors.Is(err, ErrUnbornHead) {
		t.Fatalf("err = %v, want ErrUnbornHead", err)
	}
}

func TestCommitEmptyReturnsErrNothingToCommit(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "hello\n")
	mustStage(t, r, "a.txt")
	r.commitAll("initial")
	_, err := Commit(t.Context(), r.repo, CommitOptions{Message: "empty"})
	if !errors.Is(err, ErrNothingToCommit) {
		t.Fatalf("err = %v, want ErrNothingToCommit", err)
	}
}

func TestCommitAllowEmptySucceeds(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "hello\n")
	mustStage(t, r, "a.txt")
	r.commitAll("initial")
	id, err := Commit(t.Context(), r.repo, CommitOptions{Message: "empty", AllowEmpty: true})
	if err != nil {
		t.Fatalf("Commit returned error %v", err)
	}
	if id.IsZero() {
		t.Fatalf("id is zero")
	}
}

func TestCommitEmptyOnUnbornHeadWithNoStagedFilesAllowsEmpty(t *testing.T) {
	r := newTestRepo(t)
	id, err := Commit(t.Context(), r.repo, CommitOptions{Message: "empty root", AllowEmpty: true})
	if err != nil {
		t.Fatalf("Commit returned error %v", err)
	}
	if id.IsZero() {
		t.Fatalf("id is zero")
	}
}

func TestCommitEmptyMessageReturnsError(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "hello\n")
	mustStage(t, r, "a.txt")
	_, err := Commit(t.Context(), r.repo, CommitOptions{Message: "   \n# only a comment\n"})
	if !errors.Is(err, ErrEmptyMessage) {
		t.Fatalf("err = %v, want ErrEmptyMessage", err)
	}
}

func TestCommitMissingIdentityReturnsError(t *testing.T) {
	r := newTestRepoNoIdentity(t)
	r.writeFile("a.txt", "hello\n")
	mustStage(t, r, "a.txt")
	_, err := Commit(t.Context(), r.repo, CommitOptions{Message: "initial"})
	if !errors.Is(err, ErrMissingIdentity) {
		t.Fatalf("err = %v, want ErrMissingIdentity", err)
	}
}

func TestCommitUsesConfiguredAuthor(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "hello\n")
	mustStage(t, r, "a.txt")
	when := time.Unix(1710000000, 0)
	id, err := Commit(t.Context(), r.repo, CommitOptions{Message: "initial", When: when})
	if err != nil {
		t.Fatalf("Commit returned error %v", err)
	}
	db := r.db()
	commit, err := db.Commit(id)
	if err != nil {
		t.Fatalf("Commit lookup returned error %v", err)
	}
	if !commit.Author.When.Equal(when) {
		t.Fatalf("author.When = %v, want %v", commit.Author.When, when)
	}
}

func TestCommitWithExplicitAuthor(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "hello\n")
	mustStage(t, r, "a.txt")
	author := &object.Signature{Name: "bob", Email: "bob@example.com", When: time.Unix(1710000001, 0)}
	id, err := Commit(t.Context(), r.repo, CommitOptions{Message: "initial", Author: author})
	if err != nil {
		t.Fatalf("Commit returned error %v", err)
	}
	db := r.db()
	commit, err := db.Commit(id)
	if err != nil {
		t.Fatalf("Commit lookup returned error %v", err)
	}
	if commit.Author.Name != "bob" {
		t.Fatalf("author = %+v", commit.Author)
	}
	if commit.Committer.Name != "ann" {
		t.Fatalf("committer = %+v", commit.Committer)
	}
}

func TestCommitWritesReflog(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "hello\n")
	mustStage(t, r, "a.txt")
	if _, err := Commit(t.Context(), r.repo, CommitOptions{Message: "initial commit\n\nbody"}); err != nil {
		t.Fatalf("Commit returned error %v", err)
	}
	store := r.refs()
	last, err := store.ReflogLast(refs.BranchName("main"))
	if err != nil {
		t.Fatalf("ReflogLast returned error %v", err)
	}
	if last.Message != "commit (initial): initial commit" {
		t.Fatalf("reflog message = %q", last.Message)
	}
	headLast, err := store.ReflogLast(refs.HEAD)
	if err != nil {
		t.Fatalf("ReflogLast(HEAD) returned error %v", err)
	}
	if headLast.Message != "commit (initial): initial commit" {
		t.Fatalf("HEAD reflog message = %q", headLast.Message)
	}
}

func TestCommitMessageNormalizationStripsCommentsAndBlankLines(t *testing.T) {
	got := normalizeMessage("subject\n\n\n# comment\nbody line\n\n\n")
	want := "subject\n\nbody line\n"
	if got != want {
		t.Fatalf("normalizeMessage = %q, want %q", got, want)
	}
}

func TestCommitContextCanceledReturnsError(t *testing.T) {
	r := newTestRepo(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := Commit(ctx, r.repo, CommitOptions{Message: "x"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestCommitDetachedHeadMovesHeadDirectly(t *testing.T) {
	r := newTestRepo(t)
	r.writeFile("a.txt", "hello\n")
	mustStage(t, r, "a.txt")
	first := r.commitAll("initial")
	if err := Switch(t.Context(), r.repo, first.String(), SwitchOptions{}); err != nil {
		t.Fatalf("Switch returned error %v", err)
	}
	r.writeFile("b.txt", "world\n")
	mustStage(t, r, "b.txt")
	id, err := Commit(t.Context(), r.repo, CommitOptions{Message: "detached"})
	if err != nil {
		t.Fatalf("Commit returned error %v", err)
	}
	store := r.refs()
	ref, err := store.Lookup(refs.HEAD)
	if err != nil {
		t.Fatalf("Lookup returned error %v", err)
	}
	if ref.IsSymbolic() {
		t.Fatalf("HEAD unexpectedly became symbolic")
	}
	if ref.Target != id {
		t.Fatalf("HEAD = %s, want %s", ref.Target, id)
	}
	branchRef, err := store.Lookup(refs.BranchName("main"))
	if err != nil {
		t.Fatalf("Lookup returned error %v", err)
	}
	if branchRef.Target != first {
		t.Fatalf("main should be unchanged, got %s want %s", branchRef.Target, first)
	}
}
