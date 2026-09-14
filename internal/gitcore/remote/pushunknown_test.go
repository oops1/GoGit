package remote

import (
	"context"
	"errors"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/transport"
)

func TestPushRejectsAnUpdateOverAnObjectWeHaveNotFetched(t *testing.T) {
	r := newTestRepo(t, "")
	db := openTestODB(t, r)
	store := openTestRefs(t, r, db)
	commit := putCommitWithTree(t, db, testWhen(), "local", putTree(t, db))
	setLocalBranch(t, store, refs.BranchName("main"), commit)
	setLocalBranch(t, store, refs.BranchName("other"), commit)

	fake := &fakeSession{adv: transport.Advertisement{Refs: []transport.Ref{{Name: "refs/heads/main", ID: bogusID(t)}}}}
	fake.pushFunc = func(_ context.Context, req transport.PushRequest) (*transport.PushResult, error) {
		drainPack(t, req)
		return &transport.PushResult{UnpackOK: true, Refs: []transport.RefStatus{{Name: "refs/heads/other", OK: true}}}, nil
	}
	withDial(t, fake, nil)

	result, err := Push(t.Context(), r, tagRemote, PushOptions{Refspecs: mustParseSpecs(t, "refs/heads/main:refs/heads/main", "refs/heads/other:refs/heads/other")})

	if !errors.Is(err, ErrNonFastForward) {
		t.Fatalf("Push returned %v, want a non-fast-forward rejection", err)
	}
	if len(result.Rejected) != 1 || result.Rejected[0].Name != "refs/heads/main" || len(result.Changes) != 1 {
		t.Fatalf("Push = %+v; want main rejected and other sent", result)
	}
}

func TestPushReportsACorruptObjectBehindTheServerTip(t *testing.T) {
	r := newTestRepo(t, "")
	db := openTestODB(t, r)
	store := openTestRefs(t, r, db)
	commit := putCommitWithTree(t, db, testWhen(), "local", putTree(t, db))
	setLocalBranch(t, store, refs.BranchName("main"), commit)
	junk, err := db.Put(object.TypeCommit, []byte("junk"))
	if err != nil {
		t.Fatal(err)
	}

	withDial(t, &fakeSession{adv: transport.Advertisement{Refs: []transport.Ref{{Name: "refs/heads/main", ID: junk}}}}, nil)

	if _, err := Push(t.Context(), r, tagRemote, PushOptions{Refspecs: mustParseSpecs(t, "refs/heads/main:refs/heads/main")}); err == nil || errors.Is(err, ErrNonFastForward) {
		t.Fatalf("Push returned %v, want the corrupt object reported", err)
	}
}
