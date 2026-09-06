package remote

import (
	"context"
	"errors"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/transport"
)

func TestPushPropagatesOdbOpenErrors(t *testing.T) {
	r := newTestRepo(t, "")
	wantErr := errors.New("boom")
	withOdbOpenError(t, wantErr)
	rem := Remote{Name: "origin", URLs: []string{"fake://x"}}
	_, err := Push(t.Context(), r, rem, PushOptions{Refspecs: mustParseSpecs(t, "refs/heads/main:refs/heads/main")})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Push returned %v, want %v", err, wantErr)
	}
}

func TestPushPropagatesRefsOpenErrors(t *testing.T) {
	r := newTestRepo(t, "")
	wantErr := errors.New("boom")
	withRefsOpenError(t, wantErr)
	rem := Remote{Name: "origin", URLs: []string{"fake://x"}}
	_, err := Push(t.Context(), r, rem, PushOptions{Refspecs: mustParseSpecs(t, "refs/heads/main:refs/heads/main")})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Push returned %v, want %v", err, wantErr)
	}
}

func TestPushPropagatesDialErrors(t *testing.T) {
	r := newTestRepo(t, "")
	wantErr := errors.New("boom")
	withDial(t, nil, wantErr)
	rem := Remote{Name: "origin", URLs: []string{"fake://x"}}
	_, err := Push(t.Context(), r, rem, PushOptions{Refspecs: mustParseSpecs(t, "refs/heads/main:refs/heads/main")})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Push returned %v, want %v", err, wantErr)
	}
}

func TestPushPropagatesAdvertiseErrors(t *testing.T) {
	r := newTestRepo(t, "")
	wantErr := errors.New("boom")
	fake := &fakeSession{advErr: wantErr}
	withDial(t, fake, nil)
	rem := Remote{Name: "origin", URLs: []string{"fake://x"}}
	_, err := Push(t.Context(), r, rem, PushOptions{Refspecs: mustParseSpecs(t, "refs/heads/main:refs/heads/main")})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Push returned %v, want %v", err, wantErr)
	}
}

func TestPushPropagatesGatherHaveIDsErrors(t *testing.T) {
	r := newTestRepo(t, "")
	db := openTestODB(t, r)
	store := openTestRefs(t, r, db)
	commit := putCommitWithTree(t, db, testWhen(), "initial", putTree(t, db))
	setLocalBranch(t, store, refs.BranchName("main"), commit)
	mustWriteFile(t, r.CommonPath("refs/remotes/origin/broken"), "not-a-valid-ref\n")

	fake := &fakeSession{}
	withDial(t, fake, nil)
	rem := Remote{Name: "origin", URLs: []string{"fake://x"}}
	_, err := Push(t.Context(), r, rem, PushOptions{Refspecs: mustParseSpecs(t, "refs/heads/main:refs/heads/main")})
	if err == nil {
		t.Fatal("Push returned no error for a malformed remote-tracking ref")
	}
}

func TestPushPropagatesCollectObjectsErrors(t *testing.T) {
	r := newTestRepo(t, "")
	db := openTestODB(t, r)
	store := openTestRefs(t, r, db)
	missingTree := hash.SumSHA1("tree", []byte("missing"))
	commit := putCommitWithTree(t, db, testWhen(), "broken", missingTree)
	setLocalBranch(t, store, refs.BranchName("main"), commit)

	fake := &fakeSession{}
	withDial(t, fake, nil)
	rem := Remote{Name: "origin", URLs: []string{"fake://x"}}
	_, err := Push(t.Context(), r, rem, PushOptions{Refspecs: mustParseSpecs(t, "refs/heads/main:refs/heads/main")})
	if err == nil {
		t.Fatal("Push returned no error for a commit with a missing tree")
	}
}

func TestPushPropagatesTransportErrors(t *testing.T) {
	r := newTestRepo(t, "")
	db := openTestODB(t, r)
	store := openTestRefs(t, r, db)
	commit := putCommitWithTree(t, db, testWhen(), "initial", putTree(t, db))
	setLocalBranch(t, store, refs.BranchName("main"), commit)

	wantErr := errors.New("connection reset")
	fake := &fakeSession{}
	fake.pushFunc = func(_ context.Context, req transport.PushRequest) (*transport.PushResult, error) {
		drainPack(t, req)
		return nil, wantErr
	}
	withDial(t, fake, nil)
	rem := Remote{Name: "origin", URLs: []string{"fake://x"}}
	_, err := Push(t.Context(), r, rem, PushOptions{Refspecs: mustParseSpecs(t, "refs/heads/main:refs/heads/main")})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Push returned %v, want %v", err, wantErr)
	}
}

func TestPushPropagatesApplyReportStatusErrors(t *testing.T) {
	r := newTestRepo(t, "")
	db := openTestODB(t, r)
	store := openTestRefs(t, r, db)
	commit := putCommitWithTree(t, db, testWhen(), "initial", putTree(t, db))
	setLocalBranch(t, store, refs.BranchName("main"), commit)

	fake := &fakeSession{}
	fake.pushFunc = func(_ context.Context, req transport.PushRequest) (*transport.PushResult, error) {
		drainPack(t, req)
		mustWriteFile(t, r.CommonPath(string(refs.RemoteBranchName("origin", "main"))), "not-a-valid-ref\n")
		return &transport.PushResult{UnpackOK: true, Refs: []transport.RefStatus{{Name: "refs/heads/main", OK: true}}}, nil
	}
	withDial(t, fake, nil)
	rem := Remote{Name: "origin", URLs: []string{"fake://x"}}
	result, err := Push(t.Context(), r, rem, PushOptions{Refspecs: mustParseSpecs(t, "refs/heads/main:refs/heads/main")})
	if err == nil {
		t.Fatal("Push returned no error for a malformed tracking ref during apply")
	}
	if len(result.Changes) != 0 {
		t.Fatalf("Push returned changes %+v, want none once applyReportStatus fails", result.Changes)
	}
}

func TestPushPropagatesWildcardPrefixErrors(t *testing.T) {
	r := newTestRepo(t, "")
	mustWriteFile(t, r.CommonPath("refs/heads/broken"), "not-a-valid-ref\n")
	fake := &fakeSession{}
	withDial(t, fake, nil)
	rem := Remote{Name: "origin", URLs: []string{"fake://x"}}
	_, err := Push(t.Context(), r, rem, PushOptions{Refspecs: mustParseSpecs(t, "refs/heads/*:refs/heads/*")})
	if err == nil {
		t.Fatal("Push returned no error for a malformed local branch matched by a wildcard refspec")
	}
}

func TestPushPropagatesForceWithLeaseLookupErrors(t *testing.T) {
	r := newTestRepo(t, "")
	db := openTestODB(t, r)
	store := openTestRefs(t, r, db)
	commit := putCommitWithTree(t, db, testWhen(), "initial", putTree(t, db))
	setLocalBranch(t, store, refs.BranchName("main"), commit)
	mustWriteFile(t, r.CommonPath(string(refs.RemoteBranchName("origin", "main"))), "not-a-valid-ref\n")

	fake := &fakeSession{}
	withDial(t, fake, nil)
	rem := Remote{Name: "origin", URLs: []string{"fake://x"}}
	opts := PushOptions{
		Refspecs:       mustParseSpecs(t, "refs/heads/main:refs/heads/main"),
		ForceWithLease: map[string]hash.ObjectID{"refs/heads/main": commit},
	}
	_, err := Push(t.Context(), r, rem, opts)
	if err == nil {
		t.Fatal("Push returned no error for a malformed tracking ref during a force-with-lease check")
	}
}

func TestPushPropagatesForceWithLeaseLookupErrorsThroughAWildcardRefspec(t *testing.T) {
	r := newTestRepo(t, "")
	db := openTestODB(t, r)
	store := openTestRefs(t, r, db)
	commit := putCommitWithTree(t, db, testWhen(), "initial", putTree(t, db))
	setLocalBranch(t, store, refs.BranchName("main"), commit)
	mustWriteFile(t, r.CommonPath(string(refs.RemoteBranchName("origin", "main"))), "not-a-valid-ref\n")

	fake := &fakeSession{}
	withDial(t, fake, nil)
	rem := Remote{Name: "origin", URLs: []string{"fake://x"}}
	opts := PushOptions{
		Refspecs:       mustParseSpecs(t, "refs/heads/*:refs/heads/*"),
		ForceWithLease: map[string]hash.ObjectID{"refs/heads/main": commit},
	}
	_, err := Push(t.Context(), r, rem, opts)
	if err == nil {
		t.Fatal("Push returned no error for a malformed tracking ref during a wildcard force-with-lease check")
	}
}

func TestApplyReportStatusPropagatesUpdateConflicts(t *testing.T) {
	r := newTestRepo(t, "")
	db := openTestODB(t, r)
	store := openTestRefs(t, r, db)
	commitA := putCommitWithTree(t, db, testWhen(), "a", putTree(t, db))
	commitB := putCommitWithTree(t, db, testWhen(), "b", putTree(t, db))

	pending := []pendingUpdate{
		{name: "refs/heads/x", new: commitA},
		{name: "refs/tags/x", new: commitB},
	}
	resp := &transport.PushResult{Refs: []transport.RefStatus{
		{Name: "refs/heads/x", OK: true},
		{Name: "refs/tags/x", OK: true},
	}}
	if _, _, _, err := applyReportStatus(store, Remote{Name: "origin"}, pending, resp); err == nil {
		t.Fatal("applyReportStatus returned no error for two updates colliding on the same tracking ref")
	}
}

func TestApplyReportStatusPropagatesDeleteConflicts(t *testing.T) {
	r := newTestRepo(t, "")
	db := openTestODB(t, r)
	store := openTestRefs(t, r, db)
	head := putCommitWithTree(t, db, testWhen(), "head", putTree(t, db))
	setLocalBranch(t, store, refs.RemoteBranchName("origin", "x"), head)

	pending := []pendingUpdate{
		{name: "refs/heads/x", old: head, deleted: true},
		{name: "refs/tags/x", old: head, deleted: true},
	}
	resp := &transport.PushResult{Refs: []transport.RefStatus{
		{Name: "refs/heads/x", OK: true},
		{Name: "refs/tags/x", OK: true},
	}}
	if _, _, _, err := applyReportStatus(store, Remote{Name: "origin"}, pending, resp); err == nil {
		t.Fatal("applyReportStatus returned no error for two deletes colliding on the same tracking ref")
	}
}

func TestApplyReportStatusIgnoresAStatusNotMatchingAnyPendingUpdate(t *testing.T) {
	r := newTestRepo(t, "")
	db := openTestODB(t, r)
	store := openTestRefs(t, r, db)
	commit := putCommitWithTree(t, db, testWhen(), "initial", putTree(t, db))

	pending := []pendingUpdate{{name: "refs/heads/main", new: commit}}
	resp := &transport.PushResult{Refs: []transport.RefStatus{
		{Name: "refs/heads/unexpected", OK: true},
		{Name: "refs/heads/main", OK: true},
	}}
	changes, _, _, err := applyReportStatus(store, Remote{Name: "origin"}, pending, resp)
	if err != nil {
		t.Fatalf("applyReportStatus returned error %v", err)
	}
	if len(changes) != 1 || changes[0].Name != refs.RemoteBranchName("origin", "main") {
		t.Fatalf("applyReportStatus returned changes %+v, want only the tracked update", changes)
	}
}

func TestApplyReportStatusSkipsADeleteForARefWithNoTrackingEntry(t *testing.T) {
	r := newTestRepo(t, "")
	db := openTestODB(t, r)
	store := openTestRefs(t, r, db)

	pending := []pendingUpdate{{name: "refs/heads/gone", deleted: true}}
	resp := &transport.PushResult{Refs: []transport.RefStatus{{Name: "refs/heads/gone", OK: true}}}
	changes, _, _, err := applyReportStatus(store, Remote{Name: "origin"}, pending, resp)
	if err != nil {
		t.Fatalf("applyReportStatus returned error %v", err)
	}
	if len(changes) != 0 {
		t.Fatalf("applyReportStatus returned changes %+v, want none for a delete with no tracking ref", changes)
	}
}

func TestApplyReportStatusSkipsAnUpdateAlreadyAtTheReportedValue(t *testing.T) {
	r := newTestRepo(t, "")
	db := openTestODB(t, r)
	store := openTestRefs(t, r, db)
	commit := putCommitWithTree(t, db, testWhen(), "initial", putTree(t, db))
	setLocalBranch(t, store, refs.RemoteBranchName("origin", "main"), commit)

	pending := []pendingUpdate{{name: "refs/heads/main", new: commit}}
	resp := &transport.PushResult{Refs: []transport.RefStatus{{Name: "refs/heads/main", OK: true}}}
	changes, _, _, err := applyReportStatus(store, Remote{Name: "origin"}, pending, resp)
	if err != nil {
		t.Fatalf("applyReportStatus returned error %v", err)
	}
	if len(changes) != 0 {
		t.Fatalf("applyReportStatus returned changes %+v, want none when the tracking ref already matches", changes)
	}
}

func TestApplyReportStatusPropagatesTransactionCommitErrors(t *testing.T) {
	r := newTestRepo(t, "")
	db := openTestODB(t, r)
	store := openTestRefs(t, r, db)
	commit := putCommitWithTree(t, db, testWhen(), "initial", putTree(t, db))
	mustWriteFile(t, r.CommonPath("refs/remotes/origin"), commit.String()+"\n")

	pending := []pendingUpdate{{name: "refs/heads/main", new: commit}}
	resp := &transport.PushResult{Refs: []transport.RefStatus{{Name: "refs/heads/main", OK: true}}}
	if _, _, _, err := applyReportStatus(store, Remote{Name: "origin"}, pending, resp); err == nil {
		t.Fatal("applyReportStatus returned no error when refs/remotes/origin is already a loose ref file")
	}
}

func TestPushPropagatesFastForwardCheckErrorsWhenNotForced(t *testing.T) {
	r := newTestRepo(t, "")
	db := openTestODB(t, r)
	store := openTestRefs(t, r, db)
	unknownOld := hash.SumSHA1("commit", []byte("unknown-old"))
	newCommit := putCommitWithTree(t, db, testWhen(), "next", putTree(t, db))
	setLocalBranch(t, store, refs.BranchName("main"), newCommit)

	fake := &fakeSession{adv: transport.Advertisement{Refs: []transport.Ref{{Name: "refs/heads/main", ID: unknownOld}}}}
	withDial(t, fake, nil)
	rem := Remote{Name: "origin", URLs: []string{"fake://x"}}
	_, err := Push(t.Context(), r, rem, PushOptions{Refspecs: mustParseSpecs(t, "refs/heads/main:refs/heads/main")})
	if err == nil {
		t.Fatal("Push returned no error when the fast-forward check could not resolve the old value")
	}
}

func TestPushForcesAnUpdateWhenTheFastForwardCheckCannotResolveTheOldValue(t *testing.T) {
	r := newTestRepo(t, "")
	db := openTestODB(t, r)
	store := openTestRefs(t, r, db)
	unknownOld := hash.SumSHA1("commit", []byte("unknown-old"))
	newCommit := putCommitWithTree(t, db, testWhen(), "next", putTree(t, db))
	setLocalBranch(t, store, refs.BranchName("main"), newCommit)

	fake := &fakeSession{adv: transport.Advertisement{Refs: []transport.Ref{{Name: "refs/heads/main", ID: unknownOld}}}}
	fake.pushFunc = func(_ context.Context, req transport.PushRequest) (*transport.PushResult, error) {
		drainPack(t, req)
		return &transport.PushResult{UnpackOK: true, Refs: []transport.RefStatus{{Name: "refs/heads/main", OK: true}}}, nil
	}
	withDial(t, fake, nil)
	rem := Remote{Name: "origin", URLs: []string{"fake://x"}}
	result, err := Push(t.Context(), r, rem, PushOptions{Refspecs: mustParseSpecs(t, "refs/heads/main:refs/heads/main"), Force: true})
	if err != nil {
		t.Fatalf("Push returned error %v, want the forced update to proceed despite the unresolved old value", err)
	}
	if len(result.Changes) != 1 || !result.Changes[0].Forced || result.Changes[0].New != newCommit {
		t.Fatalf("Push returned changes %+v, want a forced update to %s", result.Changes, newCommit)
	}
}

func TestPushPropagatesDeleteForceWithLeaseLookupErrors(t *testing.T) {
	r := newTestRepo(t, "")
	mustWriteFile(t, r.CommonPath(string(refs.RemoteBranchName("origin", "gone"))), "not-a-valid-ref\n")
	gone := hash.SumSHA1("commit", []byte("gone"))

	fake := &fakeSession{adv: transport.Advertisement{Refs: []transport.Ref{{Name: "refs/heads/gone", ID: gone}}}}
	withDial(t, fake, nil)
	rem := Remote{Name: "origin", URLs: []string{"fake://x"}}
	opts := PushOptions{
		Refspecs:       mustParseSpecs(t, ":refs/heads/gone"),
		ForceWithLease: map[string]hash.ObjectID{"refs/heads/gone": gone},
	}
	if _, err := Push(t.Context(), r, rem, opts); err == nil {
		t.Fatal("Push returned no error for a malformed tracking ref during a delete force-with-lease check")
	}
}

func TestPushSkipsARefspecAlreadyPlannedForTheSameDestination(t *testing.T) {
	r := newTestRepo(t, "")
	db := openTestODB(t, r)
	store := openTestRefs(t, r, db)
	commit := putCommitWithTree(t, db, testWhen(), "initial", putTree(t, db))
	setLocalBranch(t, store, refs.BranchName("main"), commit)
	setLocalBranch(t, store, refs.BranchName("alias"), commit)

	var names []string
	fake := &fakeSession{}
	fake.pushFunc = func(_ context.Context, req transport.PushRequest) (*transport.PushResult, error) {
		drainPack(t, req)
		for _, u := range req.Updates {
			names = append(names, u.Name)
		}
		return &transport.PushResult{UnpackOK: true, Refs: []transport.RefStatus{{Name: "refs/heads/main", OK: true}}}, nil
	}
	withDial(t, fake, nil)
	rem := Remote{Name: "origin", URLs: []string{"fake://x"}}
	specs := mustParseSpecs(t, "refs/heads/main:refs/heads/main", "refs/heads/alias:refs/heads/main")
	if _, err := Push(t.Context(), r, rem, PushOptions{Refspecs: specs}); err != nil {
		t.Fatalf("Push returned error %v", err)
	}
	if len(names) != 1 {
		t.Fatalf("Push sent updates %v, want a single deduplicated destination", names)
	}
}

func TestPushSkipsADestinationAlreadyAtTheWantedValue(t *testing.T) {
	r := newTestRepo(t, "")
	db := openTestODB(t, r)
	store := openTestRefs(t, r, db)
	commit := putCommitWithTree(t, db, testWhen(), "initial", putTree(t, db))
	setLocalBranch(t, store, refs.BranchName("main"), commit)

	fake := &fakeSession{adv: transport.Advertisement{Refs: []transport.Ref{{Name: "refs/heads/main", ID: commit}}}}
	fake.pushFunc = func(context.Context, transport.PushRequest) (*transport.PushResult, error) {
		t.Fatal("Push must not contact the server when the destination already has the wanted value")
		return nil, nil
	}
	withDial(t, fake, nil)
	rem := Remote{Name: "origin", URLs: []string{"fake://x"}}
	result, err := Push(t.Context(), r, rem, PushOptions{Refspecs: mustParseSpecs(t, "refs/heads/main:refs/heads/main")})
	if err != nil {
		t.Fatalf("Push returned error %v", err)
	}
	if len(result.Changes) != 0 {
		t.Fatalf("Push returned changes %+v, want none", result.Changes)
	}
}
