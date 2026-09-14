package remote

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/transport"
)

func TestFetchWantsADetachedHeadNoBranchContains(t *testing.T) {
	r := newTestRepo(t, defaultRemoteConfig("git://example.com/repo.git"))
	rem := loadTestRemote(t, r, "origin")
	server := newFakeObjectStore()
	main := server.putCommit(time.Unix(1_700_000_000, 0).UTC(), "main")
	detached := server.putCommit(time.Unix(1_700_000_100, 0).UTC(), "detached", main)
	adv := transport.Advertisement{Refs: []transport.Ref{
		{Name: "HEAD", ID: detached},
		{Name: "refs/heads/main", ID: main},
	}}
	fake := withDial(t, &fakeSession{adv: adv}, nil)
	fake.fetchFunc = func(context.Context, transport.FetchRequest, transport.Negotiator) (*transport.FetchResponse, error) {
		return packResponse(t, server, []hash.ObjectID{main, detached}), nil
	}

	result, err := Fetch(t.Context(), r, rem, FetchOptions{WantHead: true})
	if err != nil {
		t.Fatalf("Fetch returned error %v", err)
	}
	if !slices.Equal(fake.lastReq.Wants, []hash.ObjectID{main, detached}) {
		t.Fatalf("wants = %v, want main and the detached head", fake.lastReq.Wants)
	}
	if len(result.Changes) != 1 || result.Changes[0].Name != refs.RemoteBranchName("origin", "main") {
		t.Fatalf("changes = %+v, want only origin/main", result.Changes)
	}
	if has, err := openTestODB(t, r).Has(detached); err != nil || !has {
		t.Fatalf("the detached head was not fetched: %v, %v", has, err)
	}
}

func TestFetchWantsTheHeadEvenWhenNoRefspecMatches(t *testing.T) {
	r := newTestRepo(t, defaultRemoteConfig("git://example.com/repo.git"))
	rem := loadTestRemote(t, r, "origin")
	server := newFakeObjectStore()
	detached := server.putCommit(time.Unix(1_700_000_000, 0).UTC(), "detached")
	fake := withDial(t, &fakeSession{adv: transport.Advertisement{Refs: []transport.Ref{{Name: "HEAD", ID: detached}}}}, nil)
	fake.fetchFunc = func(context.Context, transport.FetchRequest, transport.Negotiator) (*transport.FetchResponse, error) {
		return packResponse(t, server, []hash.ObjectID{detached}), nil
	}

	if _, err := Fetch(t.Context(), r, rem, FetchOptions{WantHead: true}); err != nil {
		t.Fatalf("Fetch returned error %v", err)
	}
	if !slices.Equal(fake.lastReq.Wants, []hash.ObjectID{detached}) {
		t.Fatalf("wants = %v, want the head alone", fake.lastReq.Wants)
	}
}

func TestFetchAsksForTheHeadOnceWhenABranchPointsAtIt(t *testing.T) {
	r := newTestRepo(t, defaultRemoteConfig("git://example.com/repo.git"))
	rem := loadTestRemote(t, r, "origin")
	server := newFakeObjectStore()
	main := server.putCommit(time.Unix(1_700_000_000, 0).UTC(), "main")
	adv := transport.Advertisement{Refs: []transport.Ref{{Name: "HEAD", ID: main}, {Name: "refs/heads/main", ID: main}}, Head: "refs/heads/main"}
	fake := withDial(t, &fakeSession{adv: adv}, nil)
	fake.fetchFunc = func(context.Context, transport.FetchRequest, transport.Negotiator) (*transport.FetchResponse, error) {
		return packResponse(t, server, []hash.ObjectID{main}), nil
	}

	if _, err := Fetch(t.Context(), r, rem, FetchOptions{WantHead: true}); err != nil {
		t.Fatalf("Fetch returned error %v", err)
	}
	if !slices.Equal(fake.lastReq.Wants, []hash.ObjectID{main}) {
		t.Fatalf("wants = %v, want main once", fake.lastReq.Wants)
	}
}

func TestAdvertisedHeadIgnoresAMissingOrUnbornHead(t *testing.T) {
	unborn := transport.Advertisement{Refs: []transport.Ref{{Name: "HEAD"}}}
	if _, ok := advertisedHead(unborn, true); ok {
		t.Fatal("an unborn head was wanted")
	}
	if _, ok := advertisedHead(transport.Advertisement{}, true); ok {
		t.Fatal("a missing head was wanted")
	}
}
