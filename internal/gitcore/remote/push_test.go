package remote

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/transport"
)

func drainPack(t *testing.T, req transport.PushRequest) []byte {
	t.Helper()
	data, err := io.ReadAll(req.Pack)
	if err != nil {
		t.Fatalf("reading the push pack returned error %v", err)
	}
	return data
}

func containsID(ids []hash.ObjectID, want hash.ObjectID) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

func TestPushRequiresAURL(t *testing.T) {
	r := newTestRepo(t, "")
	if _, err := Push(t.Context(), r, Remote{}, PushOptions{}); !errors.Is(err, ErrNoURL) {
		t.Fatalf("Push returned %v, want %v", err, ErrNoURL)
	}
}

func TestPushWithoutRefspecsIsANoOp(t *testing.T) {
	r := newTestRepo(t, "")
	rem := Remote{Name: "origin", URLs: []string{"fake://x"}}
	result, err := Push(t.Context(), r, rem, PushOptions{})
	if err != nil {
		t.Fatalf("Push returned error %v", err)
	}
	if len(result.Changes) != 0 || len(result.Rejected) != 0 {
		t.Fatalf("Push returned %+v, want an empty result", result)
	}
}

func TestPushUsesTheRemoteConfiguredRefspecsByDefault(t *testing.T) {
	r := newTestRepo(t, "")
	db := openTestODB(t, r)
	store := openTestRefs(t, r, db)
	commit := putCommitWithTree(t, db, testWhen(), "root", putTree(t, db))
	setLocalBranch(t, store, refs.BranchName("main"), commit)

	fake := &fakeSession{}
	fake.pushFunc = func(_ context.Context, req transport.PushRequest) (*transport.PushResult, error) {
		drainPack(t, req)
		return &transport.PushResult{UnpackOK: true, Refs: []transport.RefStatus{{Name: "refs/heads/main", OK: true}}}, nil
	}
	withDial(t, fake, nil)

	rem := Remote{Name: "origin", URLs: []string{"fake://x"}, Push: mustParseSpecs(t, "refs/heads/main:refs/heads/main")}
	result, err := Push(t.Context(), r, rem, PushOptions{})
	if err != nil {
		t.Fatalf("Push returned error %v", err)
	}
	if len(result.Changes) != 1 || result.Changes[0].New != commit {
		t.Fatalf("Push returned changes %+v", result.Changes)
	}
}

func TestPushCreatesANewRemoteBranch(t *testing.T) {
	r := newTestRepo(t, "")
	db := openTestODB(t, r)
	store := openTestRefs(t, r, db)
	blob := putBlob(t, db, "hello\n")
	tree := putTree(t, db, object.TreeEntry{Mode: object.ModeBlob, Name: "a.txt", ID: blob})
	commit := putCommitWithTree(t, db, testWhen(), "initial", tree)
	setLocalBranch(t, store, refs.BranchName("main"), commit)

	var gotUpdates []transport.Update
	var packBytes []byte
	fake := &fakeSession{adv: transport.Advertisement{}}
	fake.pushFunc = func(_ context.Context, req transport.PushRequest) (*transport.PushResult, error) {
		gotUpdates = req.Updates
		packBytes = drainPack(t, req)
		return &transport.PushResult{UnpackOK: true, Refs: []transport.RefStatus{{Name: "refs/heads/main", OK: true}}}, nil
	}
	withDial(t, fake, nil)

	rem := Remote{Name: "origin", URLs: []string{"fake://x"}}
	specs := mustParseSpecs(t, "refs/heads/main:refs/heads/main")
	result, err := Push(t.Context(), r, rem, PushOptions{Refspecs: specs})
	if err != nil {
		t.Fatalf("Push returned error %v", err)
	}
	if len(gotUpdates) != 1 || gotUpdates[0].Name != "refs/heads/main" || !gotUpdates[0].Old.IsZero() || gotUpdates[0].New != commit {
		t.Fatalf("Push sent updates %+v", gotUpdates)
	}
	if len(packBytes) == 0 {
		t.Fatal("Push sent an empty pack for a brand new branch")
	}
	if len(result.Changes) != 1 {
		t.Fatalf("Push returned changes %+v, want one", result.Changes)
	}
	change := result.Changes[0]
	if change.Name != refs.RemoteBranchName("origin", "main") || change.New != commit || !change.Created {
		t.Fatalf("Push returned change %+v", change)
	}
	current, existed, err := lookupCurrent(store, refs.RemoteBranchName("origin", "main"))
	if err != nil || !existed || current != commit {
		t.Fatalf("lookupCurrent returned (%s, %v, %v), want (%s, true, nil)", current, existed, err, commit)
	}
}

func TestPushReportsAServerRejection(t *testing.T) {
	r := newTestRepo(t, "")
	db := openTestODB(t, r)
	store := openTestRefs(t, r, db)
	commit := putCommitWithTree(t, db, testWhen(), "initial", putTree(t, db))
	setLocalBranch(t, store, refs.BranchName("main"), commit)

	fake := &fakeSession{}
	fake.pushFunc = func(_ context.Context, req transport.PushRequest) (*transport.PushResult, error) {
		drainPack(t, req)
		return &transport.PushResult{UnpackOK: true, Refs: []transport.RefStatus{{Name: "refs/heads/main", OK: false, Message: "denied"}}}, nil
	}
	withDial(t, fake, nil)

	rem := Remote{Name: "origin", URLs: []string{"fake://x"}}
	result, err := Push(t.Context(), r, rem, PushOptions{Refspecs: mustParseSpecs(t, "refs/heads/main:refs/heads/main")})
	if !errors.Is(err, ErrRejected) {
		t.Fatalf("Push returned %v, want %v", err, ErrRejected)
	}
	if len(result.Rejected) != 1 || result.Rejected[0].Message != "denied" {
		t.Fatalf("Push returned rejected %+v", result.Rejected)
	}
	if len(result.Changes) != 0 {
		t.Fatalf("Push returned changes %+v, want none", result.Changes)
	}
}

func TestPushReportsAnUnpackError(t *testing.T) {
	r := newTestRepo(t, "")
	db := openTestODB(t, r)
	store := openTestRefs(t, r, db)
	commit := putCommitWithTree(t, db, testWhen(), "initial", putTree(t, db))
	setLocalBranch(t, store, refs.BranchName("main"), commit)

	fake := &fakeSession{}
	fake.pushFunc = func(_ context.Context, req transport.PushRequest) (*transport.PushResult, error) {
		drainPack(t, req)
		return &transport.PushResult{UnpackOK: false, UnpackError: "index-pack failed"}, nil
	}
	withDial(t, fake, nil)

	rem := Remote{Name: "origin", URLs: []string{"fake://x"}}
	result, err := Push(t.Context(), r, rem, PushOptions{Refspecs: mustParseSpecs(t, "refs/heads/main:refs/heads/main")})
	if !errors.Is(err, ErrRejected) {
		t.Fatalf("Push returned %v, want %v", err, ErrRejected)
	}
	if len(result.Changes) != 0 {
		t.Fatalf("Push returned changes %+v, want none", result.Changes)
	}
}

func TestPushRejectsANonFastForwardWithoutForce(t *testing.T) {
	r := newTestRepo(t, "")
	db := openTestODB(t, r)
	store := openTestRefs(t, r, db)
	oldCommit := putCommitWithTree(t, db, testWhen(), "old", putTree(t, db))
	newCommit := putCommitWithTree(t, db, testWhen(), "unrelated", putTree(t, db, object.TreeEntry{Mode: object.ModeBlob, Name: "x", ID: putBlob(t, db, "x")}))
	setLocalBranch(t, store, refs.BranchName("main"), newCommit)

	fake := &fakeSession{adv: transport.Advertisement{Refs: []transport.Ref{{Name: "refs/heads/main", ID: oldCommit}}}}
	fake.pushFunc = func(context.Context, transport.PushRequest) (*transport.PushResult, error) {
		t.Fatal("Push must not contact the server for a rejected non-fast-forward update")
		return nil, nil
	}
	withDial(t, fake, nil)

	rem := Remote{Name: "origin", URLs: []string{"fake://x"}}
	result, err := Push(t.Context(), r, rem, PushOptions{Refspecs: mustParseSpecs(t, "refs/heads/main:refs/heads/main")})
	if !errors.Is(err, ErrNonFastForward) {
		t.Fatalf("Push returned %v, want %v", err, ErrNonFastForward)
	}
	if len(result.Rejected) != 1 || result.Rejected[0].Name != "refs/heads/main" {
		t.Fatalf("Push returned rejected %+v", result.Rejected)
	}
}

func TestPushAllowsANonFastForwardWithForce(t *testing.T) {
	r := newTestRepo(t, "")
	db := openTestODB(t, r)
	store := openTestRefs(t, r, db)
	oldCommit := putCommitWithTree(t, db, testWhen(), "old", putTree(t, db))
	newCommit := putCommitWithTree(t, db, testWhen(), "unrelated", putTree(t, db, object.TreeEntry{Mode: object.ModeBlob, Name: "x", ID: putBlob(t, db, "x")}))
	setLocalBranch(t, store, refs.BranchName("main"), newCommit)

	fake := &fakeSession{adv: transport.Advertisement{Refs: []transport.Ref{{Name: "refs/heads/main", ID: oldCommit}}}}
	fake.pushFunc = func(_ context.Context, req transport.PushRequest) (*transport.PushResult, error) {
		drainPack(t, req)
		return &transport.PushResult{UnpackOK: true, Refs: []transport.RefStatus{{Name: "refs/heads/main", OK: true}}}, nil
	}
	withDial(t, fake, nil)

	rem := Remote{Name: "origin", URLs: []string{"fake://x"}}
	result, err := Push(t.Context(), r, rem, PushOptions{Refspecs: mustParseSpecs(t, "refs/heads/main:refs/heads/main"), Force: true})
	if err != nil {
		t.Fatalf("Push returned error %v", err)
	}
	if len(result.Changes) != 1 || !result.Changes[0].Forced {
		t.Fatalf("Push returned changes %+v, want a forced update", result.Changes)
	}
}

func TestPushAllowsForceWithLeaseWhenTheTrackingRefMatches(t *testing.T) {
	r := newTestRepo(t, "")
	db := openTestODB(t, r)
	store := openTestRefs(t, r, db)
	base := putCommitWithTree(t, db, testWhen(), "base", putTree(t, db))
	next := putCommitWithTree(t, db, testWhen(), "next", putTree(t, db), base)
	setLocalBranch(t, store, refs.BranchName("main"), next)
	setLocalBranch(t, store, refs.RemoteBranchName("origin", "main"), base)

	fake := &fakeSession{adv: transport.Advertisement{Refs: []transport.Ref{{Name: "refs/heads/main", ID: base}}}}
	fake.pushFunc = func(_ context.Context, req transport.PushRequest) (*transport.PushResult, error) {
		drainPack(t, req)
		return &transport.PushResult{UnpackOK: true, Refs: []transport.RefStatus{{Name: "refs/heads/main", OK: true}}}, nil
	}
	withDial(t, fake, nil)

	rem := Remote{Name: "origin", URLs: []string{"fake://x"}}
	opts := PushOptions{
		Refspecs:       mustParseSpecs(t, "refs/heads/main:refs/heads/main"),
		ForceWithLease: map[string]hash.ObjectID{"refs/heads/main": base},
	}
	result, err := Push(t.Context(), r, rem, opts)
	if err != nil {
		t.Fatalf("Push returned error %v", err)
	}
	if len(result.Changes) != 1 || result.Changes[0].New != next {
		t.Fatalf("Push returned changes %+v", result.Changes)
	}
}

func TestPushRejectsForceWithLeaseWhenTheTrackingRefDiffers(t *testing.T) {
	r := newTestRepo(t, "")
	db := openTestODB(t, r)
	store := openTestRefs(t, r, db)
	base := putCommitWithTree(t, db, testWhen(), "base", putTree(t, db))
	stale := putCommitWithTree(t, db, testWhen(), "stale", putTree(t, db))
	next := putCommitWithTree(t, db, testWhen(), "next", putTree(t, db), base)
	setLocalBranch(t, store, refs.BranchName("main"), next)
	setLocalBranch(t, store, refs.RemoteBranchName("origin", "main"), base)

	fake := &fakeSession{adv: transport.Advertisement{Refs: []transport.Ref{{Name: "refs/heads/main", ID: base}}}}
	fake.pushFunc = func(context.Context, transport.PushRequest) (*transport.PushResult, error) {
		t.Fatal("Push must not contact the server when force-with-lease fails")
		return nil, nil
	}
	withDial(t, fake, nil)

	rem := Remote{Name: "origin", URLs: []string{"fake://x"}}
	opts := PushOptions{
		Refspecs:       mustParseSpecs(t, "refs/heads/main:refs/heads/main"),
		ForceWithLease: map[string]hash.ObjectID{"refs/heads/main": stale},
	}
	result, err := Push(t.Context(), r, rem, opts)
	if !errors.Is(err, ErrRejected) {
		t.Fatalf("Push returned %v, want %v", err, ErrRejected)
	}
	if len(result.Rejected) != 1 || result.Rejected[0].Message != "stale info" {
		t.Fatalf("Push returned rejected %+v", result.Rejected)
	}
}

func TestPushDeletesARemoteBranch(t *testing.T) {
	r := newTestRepo(t, "")
	db := openTestODB(t, r)
	store := openTestRefs(t, r, db)
	remoteHead := putCommitWithTree(t, db, testWhen(), "gone", putTree(t, db))
	setLocalBranch(t, store, refs.RemoteBranchName("origin", "gone"), remoteHead)

	var gotUpdates []transport.Update
	fake := &fakeSession{adv: transport.Advertisement{Refs: []transport.Ref{{Name: "refs/heads/gone", ID: remoteHead}}}}
	fake.pushFunc = func(_ context.Context, req transport.PushRequest) (*transport.PushResult, error) {
		gotUpdates = req.Updates
		drainPack(t, req)
		return &transport.PushResult{UnpackOK: true, Refs: []transport.RefStatus{{Name: "refs/heads/gone", OK: true}}}, nil
	}
	withDial(t, fake, nil)

	rem := Remote{Name: "origin", URLs: []string{"fake://x"}}
	result, err := Push(t.Context(), r, rem, PushOptions{Refspecs: mustParseSpecs(t, ":refs/heads/gone")})
	if err != nil {
		t.Fatalf("Push returned error %v", err)
	}
	if len(gotUpdates) != 1 || !gotUpdates[0].New.IsZero() || gotUpdates[0].Old != remoteHead {
		t.Fatalf("Push sent updates %+v", gotUpdates)
	}
	if len(result.Changes) != 1 || !result.Changes[0].Deleted {
		t.Fatalf("Push returned changes %+v, want a deletion", result.Changes)
	}
	if _, existed, err := lookupCurrent(store, refs.RemoteBranchName("origin", "gone")); err != nil || existed {
		t.Fatalf("lookupCurrent returned existed=%v err=%v, want the tracking ref removed", existed, err)
	}
}

func TestPushSkipsDeletingARefTheServerDoesNotHave(t *testing.T) {
	r := newTestRepo(t, "")
	fake := &fakeSession{adv: transport.Advertisement{}}
	fake.pushFunc = func(context.Context, transport.PushRequest) (*transport.PushResult, error) {
		t.Fatal("Push must not contact the server for a delete of an absent ref")
		return nil, nil
	}
	withDial(t, fake, nil)

	rem := Remote{Name: "origin", URLs: []string{"fake://x"}}
	result, err := Push(t.Context(), r, rem, PushOptions{Refspecs: mustParseSpecs(t, ":refs/heads/gone")})
	if err != nil {
		t.Fatalf("Push returned error %v", err)
	}
	if len(result.Changes) != 0 {
		t.Fatalf("Push returned changes %+v, want none", result.Changes)
	}
}

func TestPushPassesAtomicThrough(t *testing.T) {
	r := newTestRepo(t, "")
	db := openTestODB(t, r)
	store := openTestRefs(t, r, db)
	commit := putCommitWithTree(t, db, testWhen(), "initial", putTree(t, db))
	setLocalBranch(t, store, refs.BranchName("main"), commit)

	var gotAtomic bool
	fake := &fakeSession{}
	fake.pushFunc = func(_ context.Context, req transport.PushRequest) (*transport.PushResult, error) {
		gotAtomic = req.Atomic
		drainPack(t, req)
		return &transport.PushResult{UnpackOK: true, Refs: []transport.RefStatus{{Name: "refs/heads/main", OK: true}}}, nil
	}
	withDial(t, fake, nil)

	rem := Remote{Name: "origin", URLs: []string{"fake://x"}}
	_, err := Push(t.Context(), r, rem, PushOptions{Refspecs: mustParseSpecs(t, "refs/heads/main:refs/heads/main"), Atomic: true})
	if err != nil {
		t.Fatalf("Push returned error %v", err)
	}
	if !gotAtomic {
		t.Fatal("Push did not forward Atomic to the transport request")
	}
}

func TestPushAtomicFailureLeavesNoChangesApplied(t *testing.T) {
	r := newTestRepo(t, "")
	db := openTestODB(t, r)
	store := openTestRefs(t, r, db)
	commitA := putCommitWithTree(t, db, testWhen(), "a", putTree(t, db))
	commitB := putCommitWithTree(t, db, testWhen(), "b", putTree(t, db))
	setLocalBranch(t, store, refs.BranchName("a"), commitA)
	setLocalBranch(t, store, refs.BranchName("b"), commitB)

	fake := &fakeSession{}
	fake.pushFunc = func(_ context.Context, req transport.PushRequest) (*transport.PushResult, error) {
		drainPack(t, req)
		refsOut := make([]transport.RefStatus, 0, len(req.Updates))
		for _, u := range req.Updates {
			refsOut = append(refsOut, transport.RefStatus{Name: u.Name, OK: false, Message: "atomic transaction failed"})
		}
		return &transport.PushResult{UnpackOK: true, Refs: refsOut}, nil
	}
	withDial(t, fake, nil)

	rem := Remote{Name: "origin", URLs: []string{"fake://x"}}
	specs := mustParseSpecs(t, "refs/heads/a:refs/heads/a", "refs/heads/b:refs/heads/b")
	result, err := Push(t.Context(), r, rem, PushOptions{Refspecs: specs, Atomic: true})
	if !errors.Is(err, ErrRejected) {
		t.Fatalf("Push returned %v, want %v", err, ErrRejected)
	}
	if len(result.Changes) != 0 {
		t.Fatalf("Push returned changes %+v, want none for an atomic failure", result.Changes)
	}
	if len(result.Rejected) != 2 {
		t.Fatalf("Push returned rejected %+v, want both refs rejected", result.Rejected)
	}
}

func TestPushStopsWhenTheContextIsAlreadyCanceled(t *testing.T) {
	r := newTestRepo(t, "")
	db := openTestODB(t, r)
	store := openTestRefs(t, r, db)
	commit := putCommitWithTree(t, db, testWhen(), "initial", putTree(t, db))
	setLocalBranch(t, store, refs.BranchName("main"), commit)

	fake := &fakeSession{}
	fake.pushFunc = func(context.Context, transport.PushRequest) (*transport.PushResult, error) {
		t.Fatal("Push must not reach the transport once the context is canceled")
		return nil, nil
	}
	withDial(t, fake, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	rem := Remote{Name: "origin", URLs: []string{"fake://x"}}
	_, err := Push(ctx, r, rem, PushOptions{Refspecs: mustParseSpecs(t, "refs/heads/main:refs/heads/main")})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Push returned %v, want %v", err, context.Canceled)
	}
}

func TestPushFailsWhenTheSourceRefspecDoesNotMatchALocalRef(t *testing.T) {
	r := newTestRepo(t, "")
	fake := &fakeSession{}
	withDial(t, fake, nil)

	rem := Remote{Name: "origin", URLs: []string{"fake://x"}}
	result, err := Push(t.Context(), r, rem, PushOptions{Refspecs: mustParseSpecs(t, "refs/heads/missing:refs/heads/missing")})
	if err == nil {
		t.Fatal("Push returned no error for a missing source ref")
	}
	if len(result.Rejected) != 1 {
		t.Fatalf("Push returned rejected %+v, want one entry", result.Rejected)
	}
}

func TestPushMatchesAWildcardRefspecAgainstLocalBranches(t *testing.T) {
	r := newTestRepo(t, "")
	db := openTestODB(t, r)
	store := openTestRefs(t, r, db)
	main := putCommitWithTree(t, db, testWhen(), "main", putTree(t, db))
	other := putCommitWithTree(t, db, testWhen(), "other", putTree(t, db))
	setLocalBranch(t, store, refs.BranchName("main"), main)
	setLocalBranch(t, store, refs.BranchName("other"), other)

	var names []string
	fake := &fakeSession{}
	fake.pushFunc = func(_ context.Context, req transport.PushRequest) (*transport.PushResult, error) {
		drainPack(t, req)
		out := make([]transport.RefStatus, 0, len(req.Updates))
		for _, u := range req.Updates {
			names = append(names, u.Name)
			out = append(out, transport.RefStatus{Name: u.Name, OK: true})
		}
		return &transport.PushResult{UnpackOK: true, Refs: out}, nil
	}
	withDial(t, fake, nil)

	rem := Remote{Name: "origin", URLs: []string{"fake://x"}}
	result, err := Push(t.Context(), r, rem, PushOptions{Refspecs: mustParseSpecs(t, "refs/heads/*:refs/heads/*")})
	if err != nil {
		t.Fatalf("Push returned error %v", err)
	}
	if len(names) != 2 {
		t.Fatalf("Push sent updates for %v, want both branches", names)
	}
	if len(result.Changes) != 2 {
		t.Fatalf("Push returned changes %+v, want two", result.Changes)
	}
}

func TestPushMatchesAWildcardRefspecWithTheStarNotAtTheEnd(t *testing.T) {
	r := newTestRepo(t, "")
	db := openTestODB(t, r)
	store := openTestRefs(t, r, db)
	stable := putCommitWithTree(t, db, testWhen(), "stable", putTree(t, db))
	other := putCommitWithTree(t, db, testWhen(), "other", putTree(t, db))
	setLocalBranch(t, store, refs.BranchName("release-stable"), stable)
	setLocalBranch(t, store, refs.BranchName("other"), other)

	var names []string
	fake := &fakeSession{}
	fake.pushFunc = func(_ context.Context, req transport.PushRequest) (*transport.PushResult, error) {
		drainPack(t, req)
		out := make([]transport.RefStatus, 0, len(req.Updates))
		for _, u := range req.Updates {
			names = append(names, u.Name)
			out = append(out, transport.RefStatus{Name: u.Name, OK: true})
		}
		return &transport.PushResult{UnpackOK: true, Refs: out}, nil
	}
	withDial(t, fake, nil)

	rem := Remote{Name: "origin", URLs: []string{"fake://x"}}
	result, err := Push(t.Context(), r, rem, PushOptions{Refspecs: mustParseSpecs(t, "refs/heads/*-stable:refs/heads/*-stable")})
	if err != nil {
		t.Fatalf("Push returned error %v", err)
	}
	if len(names) != 1 || names[0] != "refs/heads/release-stable" {
		t.Fatalf("Push sent updates for %v, want only refs/heads/release-stable", names)
	}
	if len(result.Changes) != 1 {
		t.Fatalf("Push returned changes %+v, want one", result.Changes)
	}
}

func TestCollectPushObjectsSendsATagItsCommitTreeAndBlob(t *testing.T) {
	r := newTestRepo(t, "")
	db := openTestODB(t, r)
	blob := putBlob(t, db, "content")
	tree := putTree(t, db, object.TreeEntry{Mode: object.ModeBlob, Name: "f", ID: blob})
	commit := putCommitWithTree(t, db, testWhen(), "tagged", tree)
	tag := putTag(t, db, "v1", commit, testWhen())

	ids, _, err := collectPushObjects(t.Context(), db, nil, []hash.ObjectID{tag}, nil)
	if err != nil {
		t.Fatalf("collectPushObjects returned error %v", err)
	}
	for _, want := range []hash.ObjectID{tag, commit, tree, blob} {
		if !containsID(ids, want) {
			t.Fatalf("collectPushObjects returned %v, missing %s", ids, want)
		}
	}
}

func TestCollectPushObjectsExcludesObjectsTheServerAlreadyHasAndMarksThemThin(t *testing.T) {
	r := newTestRepo(t, "")
	db := openTestODB(t, r)
	blob := putBlob(t, db, "shared")
	tree := putTree(t, db, object.TreeEntry{Mode: object.ModeBlob, Name: "f", ID: blob})
	base := putCommitWithTree(t, db, testWhen(), "base", tree)
	next := putCommitWithTree(t, db, testWhen(), "next", tree, base)

	ids, thin, err := collectPushObjects(t.Context(), db, []hash.ObjectID{base}, []hash.ObjectID{next}, nil)
	if err != nil {
		t.Fatalf("collectPushObjects returned error %v", err)
	}
	if !containsID(ids, next) {
		t.Fatalf("collectPushObjects returned %v, missing the new commit", ids)
	}
	if containsID(ids, base) || containsID(ids, tree) || containsID(ids, blob) {
		t.Fatalf("collectPushObjects returned %v, want the shared objects excluded", ids)
	}
	if _, ok := thin[tree]; !ok {
		t.Fatalf("collectPushObjects thin bases %v, want the shared tree included", thin)
	}
	if _, ok := thin[blob]; !ok {
		t.Fatalf("collectPushObjects thin bases %v, want the shared blob included", thin)
	}
}

func TestCollectPushObjectsIgnoresHaveIDsNotStoredLocally(t *testing.T) {
	r := newTestRepo(t, "")
	db := openTestODB(t, r)
	unknown := hash.SumSHA1("commit", []byte("unknown"))
	next := putCommitWithTree(t, db, testWhen(), "next", putTree(t, db))

	ids, _, err := collectPushObjects(t.Context(), db, []hash.ObjectID{unknown}, []hash.ObjectID{next}, nil)
	if err != nil {
		t.Fatalf("collectPushObjects returned error %v", err)
	}
	if !containsID(ids, next) {
		t.Fatalf("collectPushObjects returned %v, missing the new commit", ids)
	}
}

func TestPushForceWithLeaseAllowsANonFastForwardUpdate(t *testing.T) {
	r := newTestRepo(t, "")
	db := openTestODB(t, r)
	store := openTestRefs(t, r, db)
	base := putCommitWithTree(t, db, testWhen(), "base", putTree(t, db))
	rewritten := putCommitWithTree(t, db, testWhen(), "rewritten", putTree(t, db, object.TreeEntry{Mode: object.ModeBlob, Name: "x", ID: putBlob(t, db, "x")}))
	setLocalBranch(t, store, refs.BranchName("main"), rewritten)
	setLocalBranch(t, store, refs.RemoteBranchName("origin", "main"), base)

	fake := &fakeSession{adv: transport.Advertisement{Refs: []transport.Ref{{Name: "refs/heads/main", ID: base}}}}
	fake.pushFunc = func(_ context.Context, req transport.PushRequest) (*transport.PushResult, error) {
		drainPack(t, req)
		return &transport.PushResult{UnpackOK: true, Refs: []transport.RefStatus{{Name: "refs/heads/main", OK: true}}}, nil
	}
	withDial(t, fake, nil)

	rem := Remote{Name: "origin", URLs: []string{"fake://x"}}
	opts := PushOptions{
		Refspecs:       mustParseSpecs(t, "refs/heads/main:refs/heads/main"),
		ForceWithLease: map[string]hash.ObjectID{"refs/heads/main": base},
	}
	result, err := Push(t.Context(), r, rem, opts)
	if err != nil {
		t.Fatalf("Push returned error %v, want the lease-verified non-fast-forward update to succeed", err)
	}
	if len(result.Changes) != 1 || result.Changes[0].New != rewritten {
		t.Fatalf("Push returned changes %+v", result.Changes)
	}
}
