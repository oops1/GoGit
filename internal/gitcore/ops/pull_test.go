package ops

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
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

func divergedPull(t *testing.T, config string) (*testRepo, *testRepo) {
	t.Helper()
	src := newFetchServer(t)
	client := cloneForFetch(t, src)
	client.appendConfig("[user]\n\tname = ann\n\temail = ann@example.com\n" + config)
	client.repo = client.reopen()

	client.writeFile("c.txt", "local\n")
	mustStage(t, client, "c.txt")
	client.commitAll("local divergent commit")

	src.writeFile("b.txt", "world\n")
	mustStage(t, src, "b.txt")
	src.commitAll("remote divergent commit")
	return src, client
}

func TestPullMergesDivergedHistory(t *testing.T) {
	src, client := divergedPull(t, "")
	local := client.branchTarget("main")

	result, err := Pull(t.Context(), client.repo, PullOptions{})

	if err != nil || !result.Updated || !result.Merge.Committed {
		t.Fatalf("result = %+v, %v", result, err)
	}
	commit, err := client.db().Commit(result.New)
	if err != nil {
		t.Fatal(err)
	}
	want := "Merge branch 'main' of " + src.dir + "\n"
	if commit.Message != want || len(commit.Parents) != 2 || commit.Parents[0] != local {
		t.Fatalf("commit = %+v, want message %q", commit, want)
	}
	if !client.exists("b.txt") || !client.exists("c.txt") {
		t.Fatal("the working tree lacks one side")
	}
}

func TestPullWithFastForwardOnlyRefusesDivergedHistory(t *testing.T) {
	_, client := divergedPull(t, "[pull]\n\tff = only\n")

	if _, err := Pull(t.Context(), client.repo, PullOptions{}); !errors.Is(err, ErrNotFastForward) {
		t.Fatalf("Pull returned %v, want %v", err, ErrNotFastForward)
	}
}

func TestPullWithRebaseReplaysLocalCommitsOnTheUpstream(t *testing.T) {
	src, client := divergedPull(t, "[pull]\n\trebase = merges\n")
	upstream := src.branchTarget("main")

	result, err := Pull(t.Context(), client.repo, PullOptions{})

	if err != nil || !result.Updated || result.Rebase.Applied != 1 {
		t.Fatalf("result = %+v, %v", result, err)
	}
	commit, err := client.db().Commit(client.branchTarget("main"))
	if err != nil || len(commit.Parents) != 1 || commit.Parents[0] != upstream || commit.Message != "local divergent commit\n" {
		t.Fatalf("commit = %+v, %v", commit, err)
	}
	if head, ok := client.headSymbolicTarget(); !ok || head != refs.BranchName("main") {
		t.Fatalf("HEAD = %s, attached %v", head, ok)
	}
}

func TestPullWithRebaseStopsOnAConflict(t *testing.T) {
	src := newFetchServer(t)
	client := cloneForFetch(t, src)
	client.appendConfig("[user]\n\tname = ann\n\temail = ann@example.com\n[pull]\n\trebase = true\n")
	client.repo = client.reopen()
	client.writeFile("a.txt", "local\n")
	mustStage(t, client, "a.txt")
	client.commitAll("local")
	src.writeFile("a.txt", "remote\n")
	mustStage(t, src, "a.txt")
	src.commitAll("remote")

	result, err := Pull(t.Context(), client.repo, PullOptions{})

	if err != nil || result.Updated || len(result.Rebase.Conflicts) != 1 {
		t.Fatalf("result = %+v, %v", result, err)
	}
	if state, err := ReadRebaseState(client.repo); err != nil || !state.InProgress() {
		t.Fatalf("state = %+v, %v", state, err)
	}
}

func TestABranchRebaseSettingOverridesThePullDefault(t *testing.T) {
	_, client := divergedPull(t, "[pull]\n\trebase = true\n[branch \"main\"]\n\trebase = false\n")

	if result, err := Pull(t.Context(), client.repo, PullOptions{}); err != nil || !result.Merge.Committed {
		t.Fatalf("result = %+v, %v", result, err)
	}
}

func TestPullWithoutFastForwardCommitsEvenWhenBehind(t *testing.T) {
	src := newFetchServer(t)
	client := cloneForFetch(t, src)
	client.appendConfig("[user]\n\tname = ann\n\temail = ann@example.com\n[pull]\n\tff = false\n")
	client.repo = client.reopen()
	src.writeFile("b.txt", "world\n")
	mustStage(t, src, "b.txt")
	src.commitAll("second")

	result, err := Pull(t.Context(), client.repo, PullOptions{})

	if err != nil || !result.Merge.Committed || result.Merge.FastForward {
		t.Fatalf("result = %+v, %v", result, err)
	}
}

func TestPullStopsOnAConflictWithTheMergeInProgress(t *testing.T) {
	src := newFetchServer(t)
	client := cloneForFetch(t, src)
	client.appendConfig("[user]\n\tname = ann\n\temail = ann@example.com\n")
	client.repo = client.reopen()
	client.writeFile("a.txt", "local\n")
	mustStage(t, client, "a.txt")
	client.commitAll("local")
	src.writeFile("a.txt", "remote\n")
	mustStage(t, src, "a.txt")
	src.commitAll("remote")

	result, err := Pull(t.Context(), client.repo, PullOptions{})

	if err != nil || result.Updated || len(result.Merge.Conflicts) != 1 {
		t.Fatalf("result = %+v, %v", result, err)
	}
	state, err := ReadMergeState(client.repo)
	if err != nil || !state.InProgress() {
		t.Fatalf("state = %+v, %v", state, err)
	}
	if _, err := Pull(t.Context(), client.repo, PullOptions{}); !errors.Is(err, ErrMergeInProgress) {
		t.Fatalf("a second pull returned %v, want %v", err, ErrMergeInProgress)
	}
}

func TestPullNamesTheRemoteWithoutCredentialsOrSuffix(t *testing.T) {
	userinfo := strings.Join([]string{"someone", "anything"}, ":")
	for raw, want := range map[string]string{
		"https://" + userinfo + "@github.com/o/r.git/": "https://github.com/o/r",
		"https://github.com/o/r":                       "https://github.com/o/r",
		"ssh://git@host/a@b/r.git":                     "ssh://host/a@b/r",
		"git@github.com:o/r.git":                       "github.com:o/r",
		"C:/repos/up.git":                              "C:/repos/up",
		"/srv/r@x.git":                                 "/srv/r@x",
	} {
		if got := fetchHeadURL(raw); got != want {
			t.Errorf("%s: %q, want %q", raw, got, want)
		}
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

func pulledBareClient(t *testing.T) (*testRepo, *testRepo) {
	t.Helper()
	src := newFetchServer(t)
	bare := newBareTestRepo(t)
	if err := AddRemote(bare.repo, "origin", src.dir); err != nil {
		t.Fatal(err)
	}
	bare.appendConfig("[branch \"main\"]\n\tremote = origin\n\tmerge = refs/heads/main\n")
	bare.repo = bare.reopen()
	if _, err := Pull(t.Context(), bare.repo, PullOptions{}); err != nil {
		t.Fatal(err)
	}
	src.writeFile("b.txt", "world\n")
	mustStage(t, src, "b.txt")
	src.commitAll("second")
	return src, bare
}

func TestPullFastForwardsABareRepository(t *testing.T) {
	_, bare := pulledBareClient(t)
	before := bare.branchTargetIn(refs.BranchName("main"))

	result, err := Pull(t.Context(), bare.repo, PullOptions{})

	if err != nil || !result.Updated || result.Old != before || bare.branchTargetIn(refs.BranchName("main")) != result.New {
		t.Fatalf("result = %+v, %v", result, err)
	}
}

func TestPullRefusesDivergedHistoryInABareRepository(t *testing.T) {
	_, bare := pulledBareClient(t)
	db := bare.db()
	tree, err := db.PutObject(&object.Tree{})
	if err != nil {
		t.Fatal(err)
	}
	sig := testSignature()
	parent := bare.branchTargetIn(refs.BranchName("main"))
	local, err := db.PutObject(&object.Commit{Tree: tree, Parents: []hash.ObjectID{parent}, Author: sig, Committer: sig, Message: "local\n"})
	if err != nil {
		t.Fatal(err)
	}
	store := bare.refs()
	tx := store.Begin()
	if err := tx.Set(refs.BranchName("main"), local); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	if _, err := Pull(t.Context(), bare.repo, PullOptions{}); !errors.Is(err, ErrNotFastForward) {
		t.Fatalf("Pull returned %v, want %v", err, ErrNotFastForward)
	}
}

func TestPullIntoABareRepositoryStopsOnFailures(t *testing.T) {
	for name, fail := range map[string]func(t *testing.T){
		"ancestor check": func(t *testing.T) { swapOdbOpenFailOnCall(t, 1) },
		"branch update": func(t *testing.T) {
			swapTxUpdate(t, func(*refs.Transaction, refs.Name, hash.ObjectID, hash.ObjectID) error { return errInjected })
		},
		"refs": func(t *testing.T) { swapRefsOpenFailOnCall(t, 4) },
	} {
		t.Run(name, func(t *testing.T) {
			_, bare := pulledBareClient(t)
			fail(t)

			if _, err := Pull(t.Context(), bare.repo, PullOptions{}); !errors.Is(err, errInjected) {
				t.Fatalf("Pull returned %v, want %v", err, errInjected)
			}
		})
	}
}

func TestPullNamesTheBranchItMergesInto(t *testing.T) {
	src, client := divergedPull(t, "[merge]\n\tsuppressDest = release\n")

	result, err := Pull(t.Context(), client.repo, PullOptions{})
	if err != nil {
		t.Fatal(err)
	}
	commit, err := client.db().Commit(result.New)
	if err != nil {
		t.Fatal(err)
	}
	if want := "Merge branch 'main' of " + src.dir + " into main\n"; commit.Message != want {
		t.Fatalf("message = %q, want %q", commit.Message, want)
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

func TestPullFailsWhenTheMergeCannotOpenTheObjectDatabase(t *testing.T) {
	src := newFetchServer(t)
	probe := cloneForFetch(t, src)
	client := cloneForFetch(t, src)
	src.writeFile("b.txt", "world\n")
	mustStage(t, src, "b.txt")
	src.commitAll("second")
	calls := 0
	original := odbOpen
	odbOpen = func(dir string, opts odb.Options) (*odb.DB, error) {
		calls++
		return original(dir, opts)
	}
	if _, err := Pull(t.Context(), probe.repo, PullOptions{}); err != nil {
		t.Fatal(err)
	}
	odbOpen = original
	swapOdbOpenFailOnCall(t, calls)

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

func TestPullSucceedsWithoutAConfiguredIdentity(t *testing.T) {
	src := newFetchServer(t)
	client := newTestRepoNoIdentity(t)
	if err := AddRemote(client.repo, "origin", src.dir); err != nil {
		t.Fatalf("AddRemote returned error %v", err)
	}
	client.appendConfig("[branch \"main\"]\n\tremote = origin\n\tmerge = refs/heads/main\n")
	client.repo = client.reopen()

	result, err := Pull(t.Context(), client.repo, PullOptions{})
	if err != nil {
		t.Fatalf("Pull returned error %v", err)
	}
	if !result.Updated {
		t.Fatal("Pull reported no update")
	}
	if !client.exists("a.txt") {
		t.Fatal("Pull did not write a.txt to the working tree")
	}
	id, ok, err := pullTrackingRef(client.repo, refs.BranchName("main"))
	if err != nil || !ok {
		t.Fatalf("main = ok=%v err=%v, want the branch created", ok, err)
	}
	if id != result.New {
		t.Fatalf("main = %s, want %s", id, result.New)
	}
}

func TestFallbackIdentityDescribesTheAccountAndHost(t *testing.T) {
	previous := accountIdentity
	accountIdentity = func() (string, string) { return "Test Account", "test.account@host" }
	t.Cleanup(func() { accountIdentity = previous })
	when := time.Unix(1700000000, 0)
	got := fallbackIdentity(when)
	want := object.Signature{Name: "Test Account", Email: "test.account@host", When: when}
	if got != want {
		t.Fatalf("fallbackIdentity = %+v, want %+v", got, want)
	}
}

func TestSystemAccountIdentityFillsBothFields(t *testing.T) {
	name, email := systemAccountIdentity()
	if name == "" {
		t.Fatal("systemAccountIdentity returned an empty name")
	}
	if !strings.Contains(email, "@") {
		t.Fatalf("systemAccountIdentity returned email %q without a host", email)
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
