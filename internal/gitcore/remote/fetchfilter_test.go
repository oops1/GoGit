package remote

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/transport"
)

func TestFetchSendsTheFilterAndMarksTheReceivedPackAsPromisor(t *testing.T) {
	r := newTestRepo(t, defaultRemoteConfig("git://example.com/repo.git"))
	rem := loadTestRemote(t, r, "origin")
	server := newFakeObjectStore()
	main := server.putCommit(time.Unix(1_700_000_000, 0).UTC(), "main")
	fake := withDial(t, &fakeSession{adv: transport.Advertisement{Refs: []transport.Ref{{Name: "refs/heads/main", ID: main}}}}, nil)
	fake.fetchFunc = func(context.Context, transport.FetchRequest, transport.Negotiator) (*transport.FetchResponse, error) {
		return packResponse(t, server, []hash.ObjectID{main}), nil
	}

	if _, err := Fetch(t.Context(), r, rem, FetchOptions{Filter: transport.FilterBlobNone}); err != nil {
		t.Fatalf("Fetch returned error %v", err)
	}
	if fake.lastReq.Filter != transport.FilterBlobNone {
		t.Fatalf("the request carries filter %q, want %q", fake.lastReq.Filter, transport.FilterBlobNone)
	}
	marks, err := filepath.Glob(filepath.Join(r.PackDir(), "*.promisor"))
	if err != nil {
		t.Fatalf("Glob returned error %v", err)
	}
	if len(marks) != 1 {
		t.Fatalf("the fetch left %d promisor marks, want one", len(marks))
	}
}

func TestFetchForObjectsOnlyTouchesNoRefAndNoFetchHead(t *testing.T) {
	r := newTestRepo(t, defaultRemoteConfig("git://example.com/repo.git"))
	rem := loadTestRemote(t, r, "origin")
	server := newFakeObjectStore()
	main := server.putCommit(time.Unix(1_700_000_000, 0).UTC(), "main")
	fake := withDial(t, &fakeSession{adv: transport.Advertisement{Refs: []transport.Ref{{Name: "refs/heads/main", ID: main}}}}, nil)
	fake.fetchFunc = func(context.Context, transport.FetchRequest, transport.Negotiator) (*transport.FetchResponse, error) {
		return packResponse(t, server, []hash.ObjectID{main}), nil
	}

	result, err := Fetch(t.Context(), r, rem, FetchOptions{Wants: []hash.ObjectID{main}, ObjectsOnly: true})
	if err != nil {
		t.Fatalf("Fetch returned error %v", err)
	}
	if len(result.Changes) != 0 {
		t.Fatalf("changes = %+v, want none", result.Changes)
	}
	if _, err := os.Stat(r.CommonPath(fetchHeadFile)); err == nil {
		t.Fatal("the fetch wrote FETCH_HEAD")
	}
	if has, err := openTestODB(t, r).Has(main); err != nil || !has {
		t.Fatalf("the wanted object was not fetched: %v, %v", has, err)
	}
}
