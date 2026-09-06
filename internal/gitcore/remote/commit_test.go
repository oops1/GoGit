package remote

import (
	"errors"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/transport"
)

func TestCommitRefUpdatesPropagatesHasErrors(t *testing.T) {
	r := newTestRepo(t, "")
	db := openTestODB(t, r)
	store := openTestRefs(t, r, db)
	head := hash.SumSHA1("commit", []byte("head"))
	fake := newFakeStore()
	wantErr := errors.New("boom")
	fake.hasErr = wantErr

	matched := []matchedRef{{ref: transport.Ref{Name: "refs/heads/master", ID: head}, dst: refs.RemoteBranchName("origin", "master")}}
	if _, _, err := commitRefUpdates(store, fake, nil, matched, nil, FetchOptions{}); !errors.Is(err, wantErr) {
		t.Fatalf("commitRefUpdates returned %v, want %v", err, wantErr)
	}
}

func TestCommitRefUpdatesPropagatesFastForwardCheckErrors(t *testing.T) {
	r := newTestRepo(t, "")
	db := openTestODB(t, r)
	store := openTestRefs(t, r, db)
	old := hash.SumSHA1("commit", []byte("old"))
	next := hash.SumSHA1("commit", []byte("next"))
	setLocalBranch(t, store, refs.RemoteBranchName("origin", "master"), old)

	fake := newFakeStore()
	fake.has[next] = true
	wantErr := errors.New("boom")
	fake.peelErr = wantErr

	matched := []matchedRef{{ref: transport.Ref{Name: "refs/heads/master", ID: next}, dst: refs.RemoteBranchName("origin", "master")}}
	if _, _, err := commitRefUpdates(store, fake, nil, matched, nil, FetchOptions{}); !errors.Is(err, wantErr) {
		t.Fatalf("commitRefUpdates returned %v, want %v", err, wantErr)
	}
}

func TestCommitRefUpdatesPropagatesUpdateConflicts(t *testing.T) {
	r := newTestRepo(t, "")
	db := openTestODB(t, r)
	store := openTestRefs(t, r, db)
	first := hash.SumSHA1("commit", []byte("first"))
	second := hash.SumSHA1("commit", []byte("second"))
	fake := newFakeStore()
	fake.has[first] = true
	fake.has[second] = true

	dst := refs.RemoteBranchName("origin", "master")
	matched := []matchedRef{
		{ref: transport.Ref{Name: "refs/heads/master", ID: first}, dst: dst},
		{ref: transport.Ref{Name: "refs/heads/other", ID: second}, dst: dst},
	}
	if _, _, err := commitRefUpdates(store, fake, nil, matched, nil, FetchOptions{}); err == nil {
		t.Fatal("commitRefUpdates returned no error for two updates targeting the same ref")
	}
}

func TestCommitRefUpdatesPropagatesDeleteConflicts(t *testing.T) {
	r := newTestRepo(t, "")
	db := openTestODB(t, r)
	store := openTestRefs(t, r, db)
	head := hash.SumSHA1("commit", []byte("head"))
	fake := newFakeStore()
	fake.has[head] = true

	dst := refs.RemoteBranchName("origin", "master")
	matched := []matchedRef{{ref: transport.Ref{Name: "refs/heads/master", ID: head}, dst: dst}}
	stale := []refs.Ref{{Name: dst, Target: head}}
	if _, _, err := commitRefUpdates(store, fake, nil, matched, stale, FetchOptions{}); err == nil {
		t.Fatal("commitRefUpdates returned no error for an update and a delete on the same ref")
	}
}

func TestCommitRefUpdatesPropagatesLookupCurrentSymbolicResolutionErrors(t *testing.T) {
	r := newTestRepo(t, "")
	db := openTestODB(t, r)
	store := openTestRefs(t, r, db)
	dst := refs.RemoteBranchName("origin", "master")
	tx := store.Begin()
	if err := tx.SetSymbolic(dst, refs.BranchName("a")); err != nil {
		t.Fatalf("SetSymbolic returned error %v", err)
	}
	if err := tx.SetSymbolic(refs.BranchName("a"), refs.BranchName("b")); err != nil {
		t.Fatalf("SetSymbolic returned error %v", err)
	}
	if err := tx.SetSymbolic(refs.BranchName("b"), refs.BranchName("a")); err != nil {
		t.Fatalf("SetSymbolic returned error %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit returned error %v", err)
	}

	head := hash.SumSHA1("commit", []byte("head"))
	fake := newFakeStore()
	fake.has[head] = true
	matched := []matchedRef{{ref: transport.Ref{Name: "refs/heads/master", ID: head}, dst: dst}}
	if _, _, err := commitRefUpdates(store, fake, nil, matched, nil, FetchOptions{}); err == nil {
		t.Fatal("commitRefUpdates returned no error for a circular symbolic destination ref")
	}
}

func TestIsFastForwardPropagatesTheNewSidePeelError(t *testing.T) {
	fake := newFakeStore()
	old := hash.SumSHA1("commit", []byte("old"))
	newID := hash.SumSHA1("commit", []byte("new"))
	wantErr := errors.New("boom")
	fake.peelErrFor[newID] = wantErr
	if _, err := isFastForward(fake, nil, old, newID); !errors.Is(err, wantErr) {
		t.Fatalf("isFastForward returned %v, want %v", err, wantErr)
	}
}

func TestCommitRefUpdatesSkipsAnAlreadyUpToDateRef(t *testing.T) {
	r := newTestRepo(t, "")
	db := openTestODB(t, r)
	store := openTestRefs(t, r, db)
	head := hash.SumSHA1("commit", []byte("head"))
	setLocalBranch(t, store, refs.RemoteBranchName("origin", "master"), head)

	fake := newFakeStore()
	fake.has[head] = true
	matched := []matchedRef{{ref: transport.Ref{Name: "refs/heads/master", ID: head}, dst: refs.RemoteBranchName("origin", "master")}}
	applied, changes, err := commitRefUpdates(store, fake, nil, matched, nil, FetchOptions{})
	if err != nil {
		t.Fatalf("commitRefUpdates returned error %v", err)
	}
	if len(changes) != 0 {
		t.Fatalf("commitRefUpdates returned changes %+v, want none for an up-to-date ref", changes)
	}
	if len(applied) != 1 {
		t.Fatalf("commitRefUpdates returned applied %+v, want the up-to-date ref still applied", applied)
	}
}

func TestCommitRefUpdatesResolvesASymbolicDestination(t *testing.T) {
	r := newTestRepo(t, "")
	db := openTestODB(t, r)
	store := openTestRefs(t, r, db)
	old := hash.SumSHA1("commit", []byte("old"))
	next := hash.SumSHA1("commit", []byte("next"))
	dst := refs.RemoteBranchName("origin", "master")
	target := refs.BranchName("real")
	setLocalBranch(t, store, target, old)
	tx := store.Begin()
	if err := tx.SetSymbolic(dst, target); err != nil {
		t.Fatalf("SetSymbolic returned error %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit returned error %v", err)
	}

	fake := newFakeStore()
	fake.has[next] = true
	matched := []matchedRef{{ref: transport.Ref{Name: "refs/heads/master", ID: next}, dst: dst}}
	_, changes, err := commitRefUpdates(store, fake, nil, matched, nil, FetchOptions{Force: true})
	if err != nil {
		t.Fatalf("commitRefUpdates returned error %v", err)
	}
	if len(changes) != 1 || changes[0].Old != old {
		t.Fatalf("commitRefUpdates returned changes %+v, want the old value resolved through the symref", changes)
	}
}

func TestCommitRefUpdatesPropagatesLookupCurrentReadErrors(t *testing.T) {
	r := newTestRepo(t, "")
	db := openTestODB(t, r)
	store := openTestRefs(t, r, db)
	dst := refs.RemoteBranchName("origin", "master")
	mustWriteFile(t, r.CommonPath(string(dst)), "not-a-valid-ref\n")

	head := hash.SumSHA1("commit", []byte("head"))
	fake := newFakeStore()
	fake.has[head] = true
	matched := []matchedRef{{ref: transport.Ref{Name: "refs/heads/master", ID: head}, dst: dst}}
	if _, _, err := commitRefUpdates(store, fake, nil, matched, nil, FetchOptions{}); err == nil {
		t.Fatal("commitRefUpdates returned no error for a malformed destination ref")
	}
}

func TestCommitRefUpdatesPropagatesTransactionCommitErrors(t *testing.T) {
	r := newTestRepo(t, "")
	db := openTestODB(t, r)
	store := openTestRefs(t, r, db)
	mustWriteFile(t, r.CommonPath("refs/remotes/origin"), hash.SumSHA1("commit", []byte("blocker")).String()+"\n")

	head := hash.SumSHA1("commit", []byte("head"))
	fake := newFakeStore()
	fake.has[head] = true
	matched := []matchedRef{{ref: transport.Ref{Name: "refs/heads/master", ID: head}, dst: refs.RemoteBranchName("origin", "master")}}
	if _, _, err := commitRefUpdates(store, fake, nil, matched, nil, FetchOptions{}); err == nil {
		t.Fatal("commitRefUpdates returned no error when refs/remotes/origin is already a loose ref file")
	}
}

func TestIsFastForwardTreatsANonCommitTargetAsNotFastForwardable(t *testing.T) {
	fake := newFakeStore()
	old := hash.SumSHA1("commit", []byte("old"))
	newID := hash.SumSHA1("commit", []byte("new"))
	fake.types[newID] = 3
	ff, err := isFastForward(fake, nil, old, newID)
	if err != nil {
		t.Fatalf("isFastForward returned error %v", err)
	}
	if ff {
		t.Fatal("isFastForward returned true for a non-commit target")
	}
}
