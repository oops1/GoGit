package remote

import (
	"context"
	"testing"
	"time"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/transport"
)

func TestFetchPruneKeepsTheSymbolicRemoteHead(t *testing.T) {
	r := newTestRepo(t, defaultRemoteConfig("git://example.com/repo.git"))
	rem := loadTestRemote(t, r, "origin")
	db := openTestODB(t, r)
	store := openTestRefs(t, r, db)

	server := newFakeObjectStore()
	head := server.putCommit(time.Unix(1_700_000_000, 0).UTC(), "root")
	old := putLocalCommit(t, db, time.Unix(1_600_000_000, 0).UTC(), "old")
	setLocalBranch(t, store, refs.RemoteBranchName("origin", "master"), old)
	remoteHead := refs.Name("refs/remotes/origin/HEAD")
	tx := store.Begin()
	if err := tx.SetSymbolic(remoteHead, refs.RemoteBranchName("origin", "master")); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	adv := transport.Advertisement{Refs: []transport.Ref{{Name: "refs/heads/master", ID: head}}}
	fake := withDial(t, &fakeSession{adv: adv}, nil)
	fake.fetchFunc = func(context.Context, transport.FetchRequest, transport.Negotiator) (*transport.FetchResponse, error) {
		return packResponse(t, server, []hash.ObjectID{head}), nil
	}

	result, err := Fetch(t.Context(), r, rem, FetchOptions{Prune: true})
	if err != nil {
		t.Fatalf("Fetch returned error %v", err)
	}
	for _, change := range result.Changes {
		if change.Deleted {
			t.Fatalf("Fetch pruned %s", change.Name)
		}
	}
	ref, err := store.Lookup(remoteHead)
	if err != nil || !ref.IsSymbolic() {
		t.Fatalf("origin/HEAD = %+v, %v", ref, err)
	}
}
