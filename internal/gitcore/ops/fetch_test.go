package ops

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/remote"
	"github.com/oops1/gogit/internal/gitcore/repo"
)

func (r *testRepo) branchTargetIn(name refs.Name) hash.ObjectID {
	r.t.Helper()
	store := r.refs()
	ref, err := store.Lookup(name)
	if err != nil {
		r.t.Fatalf("Lookup(%s) returned error %v", name, err)
	}
	return ref.Target
}

func newFetchServer(t testing.TB) *testRepo {
	t.Helper()
	src := newTestRepo(t)
	src.writeFile("a.txt", "hello\n")
	mustStage(t, src, "a.txt")
	src.commitAll("initial")
	return src
}

func cloneForFetch(t testing.TB, src *testRepo) *testRepo {
	t.Helper()
	dest := filepath.Join(t.TempDir(), "dest")
	r, err := Clone(t.Context(), src.dir, dest, CloneOptions{})
	if err != nil {
		t.Fatalf("Clone returned error %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })
	return &testRepo{t: t, dir: dest, repo: r, clock: 1700000000}
}

func TestFetchBringsInNewCommitsFromTheConfiguredRemote(t *testing.T) {
	src := newFetchServer(t)
	client := cloneForFetch(t, src)

	src.writeFile("b.txt", "world\n")
	mustStage(t, src, "b.txt")
	second := src.commitAll("second")

	result, err := Fetch(t.Context(), client.repo, "", remote.FetchOptions{})
	if err != nil {
		t.Fatalf("Fetch returned error %v", err)
	}
	if len(result.Changes) != 1 || result.Changes[0].New != second {
		t.Fatalf("Fetch returned changes %+v, want a single change to %s", result.Changes, second)
	}

	tracking := client.branchTargetIn(refs.RemoteBranchName("origin", "main"))
	if tracking != second {
		t.Fatalf("origin/main = %s, want %s", tracking, second)
	}
}

func TestFetchWithExplicitRemoteNameOverridesUpstream(t *testing.T) {
	src := newFetchServer(t)
	client := cloneForFetch(t, src)

	src.writeFile("b.txt", "world\n")
	mustStage(t, src, "b.txt")
	second := src.commitAll("second")

	if _, err := Fetch(t.Context(), client.repo, "origin", remote.FetchOptions{}); err != nil {
		t.Fatalf("Fetch returned error %v", err)
	}
	tracking := client.branchTargetIn(refs.RemoteBranchName("origin", "main"))
	if tracking != second {
		t.Fatalf("origin/main = %s, want %s", tracking, second)
	}
}

func TestFetchFailsWhenRemoteNameIsUnknown(t *testing.T) {
	src := newFetchServer(t)
	client := cloneForFetch(t, src)

	_, err := Fetch(t.Context(), client.repo, "does-not-exist", remote.FetchOptions{})
	if !errors.Is(err, remote.ErrNoRemote) {
		t.Fatalf("Fetch returned %v, want %v", err, remote.ErrNoRemote)
	}
}

func TestFetchFailsWhenCurrentBranchRefsCannotBeOpened(t *testing.T) {
	src := newFetchServer(t)
	client := cloneForFetch(t, src)
	swapRefsOpen(t, func(refs.Options) (*refs.Store, error) { return nil, errInjected })

	_, err := Fetch(t.Context(), client.repo, "", remote.FetchOptions{})
	if !errors.Is(err, errInjected) {
		t.Fatalf("Fetch returned %v, want %v", err, errInjected)
	}
}

func TestFetchFailsWhenHeadIsMalformed(t *testing.T) {
	src := newFetchServer(t)
	client := cloneForFetch(t, src)
	client.writeRawHead("not-a-valid-ref-or-hash\n")

	if _, err := Fetch(t.Context(), client.repo, "", remote.FetchOptions{}); err == nil {
		t.Fatal("Fetch returned nil error, want a malformed HEAD error")
	}
}

func TestFetchFailsWhenTheNetworkViewCannotBeOpened(t *testing.T) {
	src := newFetchServer(t)
	client := cloneForFetch(t, src)
	swapCloneRepoOpenLayout(t, func(repo.Layout, repo.OpenOptions) (*repo.Repository, error) { return nil, errInjected })

	_, err := Fetch(t.Context(), client.repo, "", remote.FetchOptions{})
	if !errors.Is(err, errInjected) {
		t.Fatalf("Fetch returned %v, want %v", err, errInjected)
	}
}

func TestFetchFailsWhenContextIsAlreadyCanceled(t *testing.T) {
	src := newFetchServer(t)
	client := cloneForFetch(t, src)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := Fetch(ctx, client.repo, "", remote.FetchOptions{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Fetch returned %v, want %v", err, context.Canceled)
	}
}
