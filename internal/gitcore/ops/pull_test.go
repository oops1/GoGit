package ops

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/odb"
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/remote"
)

func swapRefsOpenFailOnCall(t testing.TB, n int) {
	t.Helper()
	original := refsOpen
	count := 0
	refsOpen = func(opts refs.Options) (*refs.Store, error) {
		count++
		if count == n {
			return nil, errInjected
		}
		return original(opts)
	}
	t.Cleanup(func() { refsOpen = original })
}

func swapOdbOpenFailOnCall(t testing.TB, n int) {
	t.Helper()
	original := odbOpen
	count := 0
	odbOpen = func(dir string, opts odb.Options) (*odb.DB, error) {
		count++
		if count == n {
			return nil, errInjected
		}
		return original(dir, opts)
	}
	t.Cleanup(func() { odbOpen = original })
}

func TestPullFastForwardsBranchAndWorkingTree(t *testing.T) {
	src := newFetchServer(t)
	client := cloneForFetch(t, src)

	src.writeFile("a.txt", "hello v2\n")
	mustStage(t, src, "a.txt")
	second := src.commitAll("second")
	first := client.branchTargetIn(refs.BranchName("main"))

	result, err := Pull(t.Context(), client.repo, PullOptions{})
	if err != nil {
		t.Fatalf("Pull returned error %v", err)
	}
	if !result.Updated || result.UpToDate {
		t.Fatalf("Pull result = %+v, want Updated", result)
	}
	if result.Old != first || result.New != second {
		t.Fatalf("Pull result Old/New = %s/%s, want %s/%s", result.Old, result.New, first, second)
	}
	if got := client.branchTargetIn(refs.BranchName("main")); got != second {
		t.Fatalf("main = %s, want %s", got, second)
	}
	if got := client.readFile("a.txt"); got != "hello v2\n" {
		t.Fatalf("a.txt = %q, want %q", got, "hello v2\n")
	}
}

func TestPullReturnsUpToDateWhenAlreadyCurrent(t *testing.T) {
	src := newFetchServer(t)
	client := cloneForFetch(t, src)

	result, err := Pull(t.Context(), client.repo, PullOptions{})
	if err != nil {
		t.Fatalf("Pull returned error %v", err)
	}
	if !result.UpToDate || result.Updated {
		t.Fatalf("Pull result = %+v, want UpToDate", result)
	}
}

func TestPullFailsWhenHistoryHasDiverged(t *testing.T) {
	src := newFetchServer(t)
	client := cloneForFetch(t, src)

	client.writeFile("c.txt", "local\n")
	mustStage(t, client, "c.txt")
	client.commitAll("local divergent commit")

	src.writeFile("b.txt", "world\n")
	mustStage(t, src, "b.txt")
	src.commitAll("remote divergent commit")

	_, err := Pull(t.Context(), client.repo, PullOptions{})
	if !errors.Is(err, ErrNotFastForward) {
		t.Fatalf("Pull returned %v, want %v", err, ErrNotFastForward)
	}
}

func TestPullFailsWhenWorkingTreeIsDirty(t *testing.T) {
	src := newFetchServer(t)
	client := cloneForFetch(t, src)
	before := client.branchTargetIn(refs.BranchName("main"))

	src.writeFile("a.txt", "hello v2\n")
	mustStage(t, src, "a.txt")
	src.commitAll("second")

	client.writeFile("a.txt", "uncommitted local edit\n")

	_, err := Pull(t.Context(), client.repo, PullOptions{})
	if !errors.Is(err, ErrDirtyWorkTree) {
		t.Fatalf("Pull returned %v, want %v", err, ErrDirtyWorkTree)
	}
	if got := client.branchTargetIn(refs.BranchName("main")); got != before {
		t.Fatalf("main moved to %s after a failed pull, want %s", got, before)
	}
	if got := client.readFile("a.txt"); got != "uncommitted local edit\n" {
		t.Fatalf("a.txt = %q, want the local edit preserved", got)
	}
}

func TestPullFailsWhenBranchHasNoUpstream(t *testing.T) {
	r := newTestRepo(t)
	_, err := Pull(t.Context(), r.repo, PullOptions{})
	if !errors.Is(err, ErrNoUpstream) {
		t.Fatalf("Pull returned %v, want %v", err, ErrNoUpstream)
	}
}

func TestPullFailsWhenMergeRefDoesNotExistOnRemote(t *testing.T) {
	src := newFetchServer(t)
	client := newTestRepo(t)
	if err := AddRemote(client.repo, "origin", src.dir); err != nil {
		t.Fatalf("AddRemote returned error %v", err)
	}
	client.appendConfig("[branch \"main\"]\n\tremote = origin\n\tmerge = refs/heads/does-not-exist\n")
	client.repo = client.reopen()

	_, err := Pull(t.Context(), client.repo, PullOptions{})
	if !errors.Is(err, ErrNoUpstream) {
		t.Fatalf("Pull returned %v, want %v", err, ErrNoUpstream)
	}
}

func TestPullFailsWhenConfiguredRemoteDoesNotExist(t *testing.T) {
	client := newTestRepo(t)
	client.appendConfig("[branch \"main\"]\n\tremote = missing\n\tmerge = refs/heads/main\n")
	client.repo = client.reopen()

	_, err := Pull(t.Context(), client.repo, PullOptions{})
	if !errors.Is(err, remote.ErrNoRemote) {
		t.Fatalf("Pull returned %v, want %v", err, remote.ErrNoRemote)
	}
}

func TestPullFailsWhenHeadIsDetached(t *testing.T) {
	src := newFetchServer(t)
	client := cloneForFetch(t, src)
	commit := client.branchTargetIn(refs.BranchName("main"))
	client.writeRawHead(commit.String() + "\n")
	client.repo = client.reopen()

	_, err := Pull(t.Context(), client.repo, PullOptions{})
	if !errors.Is(err, ErrDetachedHead) {
		t.Fatalf("Pull returned %v, want %v", err, ErrDetachedHead)
	}
}

func TestPullOnUnbornBranchFastForwardsFromZero(t *testing.T) {
	src := newFetchServer(t)
	client := newTestRepo(t)
	if err := AddRemote(client.repo, "origin", src.dir); err != nil {
		t.Fatalf("AddRemote returned error %v", err)
	}
	client.appendConfig("[branch \"main\"]\n\tremote = origin\n\tmerge = refs/heads/main\n")
	client.repo = client.reopen()

	result, err := Pull(t.Context(), client.repo, PullOptions{})
	if err != nil {
		t.Fatalf("Pull returned error %v", err)
	}
	if !result.Updated || result.Old != hash.Zero {
		t.Fatalf("Pull result = %+v, want Updated from zero", result)
	}
	if got := client.readFile("a.txt"); got != "hello\n" {
		t.Fatalf("a.txt = %q, want %q", got, "hello\n")
	}
}

func TestPullWithExplicitRemoteOverridesConfiguredUpstream(t *testing.T) {
	src := newFetchServer(t)
	client := cloneForFetch(t, src)

	backup := newFetchServer(t)
	backup.writeFile("a.txt", "from backup\n")
	mustStage(t, backup, "a.txt")
	backupHead := backup.commitAll("backup ahead")

	if err := AddRemote(client.repo, "backup", backup.dir); err != nil {
		t.Fatalf("AddRemote returned error %v", err)
	}
	client.repo = client.reopen()

	result, err := Pull(t.Context(), client.repo, PullOptions{Remote: "backup"})
	if err != nil {
		t.Fatalf("Pull returned error %v", err)
	}
	if !result.Updated || result.New != backupHead {
		t.Fatalf("Pull result = %+v, want a fast-forward to %s", result, backupHead)
	}
}

func TestPullOnBareRepositorySkipsCheckout(t *testing.T) {
	src := newFetchServer(t)
	bareClient := newBareTestRepo(t)
	if err := AddRemote(bareClient.repo, "origin", src.dir); err != nil {
		t.Fatalf("AddRemote returned error %v", err)
	}
	bareClient.appendConfig("[user]\n\tname = ann\n\temail = ann@example.com\n[branch \"main\"]\n\tremote = origin\n\tmerge = refs/heads/main\n")
	bareClient.repo = bareClient.reopen()

	result, err := Pull(t.Context(), bareClient.repo, PullOptions{})
	if err != nil {
		t.Fatalf("Pull returned error %v", err)
	}
	if !result.Updated || result.Old != hash.Zero {
		t.Fatalf("Pull result = %+v, want a fast-forward from zero", result)
	}
	if got := bareClient.branchTargetIn(refs.BranchName("main")); got != result.New {
		t.Fatalf("main = %s, want %s", got, result.New)
	}
}

func TestPullFailsWhenContextIsAlreadyCanceled(t *testing.T) {
	src := newFetchServer(t)
	client := cloneForFetch(t, src)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := Pull(ctx, client.repo, PullOptions{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Pull returned %v, want %v", err, context.Canceled)
	}
}

func TestPullFailsWhenLocalBranchRefBecomesMalformedAfterFetch(t *testing.T) {
	src := newFetchServer(t)
	client := cloneForFetch(t, src)

	original := refsOpen
	count := 0
	refsOpen = func(opts refs.Options) (*refs.Store, error) {
		count++
		if count == 2 {
			client.writeRawRef("refs/heads/main", "garbage\n")
		}
		return original(opts)
	}
	t.Cleanup(func() { refsOpen = original })

	if _, err := Pull(t.Context(), client.repo, PullOptions{}); err == nil {
		t.Fatal("Pull returned nil error, want a malformed local ref error")
	}
}

func TestPullFailsWhenReadingLocalBranchRefFails(t *testing.T) {
	src := newFetchServer(t)
	client := cloneForFetch(t, src)
	swapRefsOpenFailOnCall(t, 2)

	_, err := Pull(t.Context(), client.repo, PullOptions{})
	if !errors.Is(err, errInjected) {
		t.Fatalf("Pull returned %v, want %v", err, errInjected)
	}
}

func TestPullFailsWhenReadingTrackingRefFails(t *testing.T) {
	src := newFetchServer(t)
	client := cloneForFetch(t, src)
	swapRefsOpenFailOnCall(t, 3)

	_, err := Pull(t.Context(), client.repo, PullOptions{})
	if !errors.Is(err, errInjected) {
		t.Fatalf("Pull returned %v, want %v", err, errInjected)
	}
}

func TestPullFailsWhenFetchCannotReadACorruptLocalBranchRef(t *testing.T) {
	src := newFetchServer(t)
	client := cloneForFetch(t, src)
	client.writeRawRef("refs/heads/main", "garbage\n")
	client.repo = client.reopen()

	if _, err := Pull(t.Context(), client.repo, PullOptions{}); err == nil {
		t.Fatal("Pull returned nil error, want a malformed ref error")
	}
}

func TestPullFailsWhenHeadIsMalformed(t *testing.T) {
	src := newFetchServer(t)
	client := cloneForFetch(t, src)
	client.writeRawHead("not-a-valid-ref-or-hash\n")

	if _, err := Pull(t.Context(), client.repo, PullOptions{}); err == nil {
		t.Fatal("Pull returned nil error, want a malformed HEAD error")
	}
}

func TestPullFailsWhenAncestorCheckFails(t *testing.T) {
	src := newFetchServer(t)
	client := cloneForFetch(t, src)
	src.writeFile("b.txt", "world\n")
	mustStage(t, src, "b.txt")
	src.commitAll("second")
	swapOdbOpenFailOnCall(t, 1)

	_, err := Pull(t.Context(), client.repo, PullOptions{})
	if !errors.Is(err, errInjected) {
		t.Fatalf("Pull returned %v, want %v", err, errInjected)
	}
}

func TestPullFailsWhenCheckoutFailsForANonOverwriteReason(t *testing.T) {
	src := newFetchServer(t)
	client := cloneForFetch(t, src)
	src.writeFile("b.txt", "world\n")
	mustStage(t, src, "b.txt")
	src.commitAll("second")
	swapOdbOpenFailOnCall(t, 2)

	_, err := Pull(t.Context(), client.repo, PullOptions{})
	if !errors.Is(err, errInjected) {
		t.Fatalf("Pull returned %v, want %v", err, errInjected)
	}
}

func TestPullFailsWhenOpeningTheFinalTransactionFails(t *testing.T) {
	src := newFetchServer(t)
	client := cloneForFetch(t, src)
	src.writeFile("b.txt", "world\n")
	mustStage(t, src, "b.txt")
	src.commitAll("second")
	swapRefsOpenFailOnCall(t, 4)

	_, err := Pull(t.Context(), client.repo, PullOptions{})
	if !errors.Is(err, errInjected) {
		t.Fatalf("Pull returned %v, want %v", err, errInjected)
	}
}

func TestPullFailsWhenTheBranchTransactionFails(t *testing.T) {
	src := newFetchServer(t)
	client := cloneForFetch(t, src)
	src.writeFile("b.txt", "world\n")
	mustStage(t, src, "b.txt")
	src.commitAll("second")
	swapTxUpdate(t, func(*refs.Transaction, refs.Name, hash.ObjectID, hash.ObjectID) error { return errInjected })

	_, err := Pull(t.Context(), client.repo, PullOptions{})
	if !errors.Is(err, errInjected) {
		t.Fatalf("Pull returned %v, want %v", err, errInjected)
	}
}

func TestPullFailsWhenIdentityIsMissing(t *testing.T) {
	src := newFetchServer(t)
	client := newTestRepoNoIdentity(t)
	if err := AddRemote(client.repo, "origin", src.dir); err != nil {
		t.Fatalf("AddRemote returned error %v", err)
	}
	client.appendConfig("[branch \"main\"]\n\tremote = origin\n\tmerge = refs/heads/main\n")
	client.repo = client.reopen()

	_, err := Pull(t.Context(), client.repo, PullOptions{})
	if !errors.Is(err, ErrMissingIdentity) {
		t.Fatalf("Pull returned %v, want %v", err, ErrMissingIdentity)
	}
	if client.exists("a.txt") {
		t.Fatalf("Pull wrote a.txt to the working tree despite failing before updating the branch")
	}
	if _, ok, err := pullTrackingRef(client.repo, refs.BranchName("main")); err != nil || ok {
		t.Fatalf("main = ok=%v err=%v, want the branch left unborn", ok, err)
	}
}

func TestPullPropagatesFetchFailures(t *testing.T) {
	client := newTestRepo(t)
	if err := AddRemote(client.repo, "origin", filepath.Join(t.TempDir(), "does-not-exist")); err != nil {
		t.Fatalf("AddRemote returned error %v", err)
	}
	client.appendConfig("[branch \"main\"]\n\tremote = origin\n\tmerge = refs/heads/main\n")
	client.repo = client.reopen()

	if _, err := Pull(t.Context(), client.repo, PullOptions{}); err == nil {
		t.Fatal("Pull returned nil error, want a dial failure")
	}
}
