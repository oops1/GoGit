package remote

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/transport"
)

func TestFetchUsesTheDefaultRefspecWhenNoneAreConfigured(t *testing.T) {
	r := newTestRepo(t, "[remote \"origin\"]\n\turl = git://example.com/repo.git\n")
	rem := loadTestRemote(t, r, "origin")

	server := newFakeObjectStore()
	head := server.putCommit(time.Unix(1_700_000_000, 0).UTC(), "root")
	adv := transport.Advertisement{Refs: []transport.Ref{{Name: "refs/heads/master", ID: head}}}
	fake := withDial(t, &fakeSession{adv: adv}, nil)
	fake.fetchFunc = func(context.Context, transport.FetchRequest, transport.Negotiator) (*transport.FetchResponse, error) {
		return packResponse(t, server, []hash.ObjectID{head}), nil
	}

	result, err := Fetch(t.Context(), r, rem, FetchOptions{})
	if err != nil {
		t.Fatalf("Fetch returned error %v", err)
	}
	if len(result.Changes) != 1 || result.Changes[0].Name != refs.RemoteBranchName("origin", "master") {
		t.Fatalf("Fetch returned changes %+v, want a default refs/remotes/origin/master update", result.Changes)
	}
}

func TestFetchPropagatesObjectDatabaseOpenErrors(t *testing.T) {
	r := newTestRepo(t, defaultRemoteConfig("git://example.com/repo.git"))
	rem := loadTestRemote(t, r, "origin")
	withDial(t, &fakeSession{}, nil)
	wantErr := errors.New("boom")
	withOdbOpenError(t, wantErr)

	if _, err := Fetch(t.Context(), r, rem, FetchOptions{}); !errors.Is(err, wantErr) {
		t.Fatalf("Fetch returned %v, want %v", err, wantErr)
	}
}

func TestFetchPropagatesRefsStoreOpenErrors(t *testing.T) {
	r := newTestRepo(t, defaultRemoteConfig("git://example.com/repo.git"))
	rem := loadTestRemote(t, r, "origin")
	withDial(t, &fakeSession{}, nil)
	wantErr := errors.New("boom")
	withRefsOpenError(t, wantErr)

	if _, err := Fetch(t.Context(), r, rem, FetchOptions{}); !errors.Is(err, wantErr) {
		t.Fatalf("Fetch returned %v, want %v", err, wantErr)
	}
}

func TestFetchPropagatesShallowReadErrors(t *testing.T) {
	r := newTestRepo(t, defaultRemoteConfig("git://example.com/repo.git"))
	rem := loadTestRemote(t, r, "origin")
	mustMkdirAll(t, r.CommonPath("shallow"))
	withDial(t, &fakeSession{}, nil)

	if _, err := Fetch(t.Context(), r, rem, FetchOptions{}); err == nil {
		t.Fatal("Fetch returned no error although .git/shallow is a directory")
	}
}

func TestFetchPropagatesNegotiatorConstructionErrors(t *testing.T) {
	r := newTestRepo(t, defaultRemoteConfig("git://example.com/repo.git"))
	rem := loadTestRemote(t, r, "origin")
	db := openTestODB(t, r)
	store := openTestRefs(t, r, db)
	tx := store.Begin()
	if err := tx.SetSymbolic(refs.BranchName("a"), refs.BranchName("b")); err != nil {
		t.Fatalf("SetSymbolic returned error %v", err)
	}
	if err := tx.SetSymbolic(refs.BranchName("b"), refs.BranchName("a")); err != nil {
		t.Fatalf("SetSymbolic returned error %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit returned error %v", err)
	}
	setLocalHead(t, store, refs.BranchName("a"))

	server := newFakeObjectStore()
	head := server.putCommit(time.Unix(1_700_000_000, 0).UTC(), "root")
	adv := transport.Advertisement{Refs: []transport.Ref{{Name: "refs/heads/master", ID: head}}}
	fake := withDial(t, &fakeSession{adv: adv}, nil)
	fake.fetchFunc = func(context.Context, transport.FetchRequest, transport.Negotiator) (*transport.FetchResponse, error) {
		t.Fatal("Fetch should not have reached session.Fetch")
		return nil, nil
	}

	if _, err := Fetch(t.Context(), r, rem, FetchOptions{}); err == nil {
		t.Fatal("Fetch returned no error for a circular local HEAD symref")
	}
}

func TestFetchPropagatesPruneErrors(t *testing.T) {
	r := newTestRepo(t, defaultRemoteConfig("git://example.com/repo.git"))
	rem := loadTestRemote(t, r, "origin")
	mustWriteFile(t, filepath.Join(r.CommonPath("refs/remotes/origin"), "broken"), "not-a-valid-ref\n")

	server := newFakeObjectStore()
	head := server.putCommit(time.Unix(1_700_000_000, 0).UTC(), "root")
	adv := transport.Advertisement{Refs: []transport.Ref{{Name: "refs/heads/master", ID: head}}}
	fake := withDial(t, &fakeSession{adv: adv}, nil)
	fake.fetchFunc = func(context.Context, transport.FetchRequest, transport.Negotiator) (*transport.FetchResponse, error) {
		return packResponse(t, server, []hash.ObjectID{head}), nil
	}

	if _, err := Fetch(t.Context(), r, rem, FetchOptions{Prune: true}); err == nil {
		t.Fatal("Fetch returned no error for a malformed loose remote-tracking ref")
	}
}

func TestFetchPropagatesFetchHeadWriteErrors(t *testing.T) {
	r := newTestRepo(t, defaultRemoteConfig("git://example.com/repo.git"))
	rem := loadTestRemote(t, r, "origin")
	mustMkdirAll(t, r.CommonPath(fetchHeadFile))

	server := newFakeObjectStore()
	head := server.putCommit(time.Unix(1_700_000_000, 0).UTC(), "root")
	adv := transport.Advertisement{Refs: []transport.Ref{{Name: "refs/heads/master", ID: head}}}
	fake := withDial(t, &fakeSession{adv: adv}, nil)
	fake.fetchFunc = func(context.Context, transport.FetchRequest, transport.Negotiator) (*transport.FetchResponse, error) {
		return packResponse(t, server, []hash.ObjectID{head}), nil
	}

	result, err := Fetch(t.Context(), r, rem, FetchOptions{})
	if err == nil {
		t.Fatal("Fetch returned no error although FETCH_HEAD is a directory")
	}
	if len(result.Changes) != 1 {
		t.Fatalf("Fetch returned changes %+v, want the ref update to still have applied", result.Changes)
	}
}

func TestFetchPropagatesShallowWriteErrors(t *testing.T) {
	r := newTestRepo(t, defaultRemoteConfig("git://example.com/repo.git"))
	rem := loadTestRemote(t, r, "origin")
	mustMkdirAll(t, r.CommonPath("shallow.lock"))

	server := newFakeObjectStore()
	head := server.putCommit(time.Unix(1_700_000_000, 0).UTC(), "root")
	adv := transport.Advertisement{Refs: []transport.Ref{{Name: "refs/heads/master", ID: head}}}
	fake := withDial(t, &fakeSession{adv: adv}, nil)
	fake.fetchFunc = func(context.Context, transport.FetchRequest, transport.Negotiator) (*transport.FetchResponse, error) {
		resp := packResponse(t, server, []hash.ObjectID{head})
		resp.Shallow = []hash.ObjectID{head}
		return resp, nil
	}

	result, err := Fetch(t.Context(), r, rem, FetchOptions{})
	if err == nil {
		t.Fatal("Fetch returned no error although shallow.lock is a directory")
	}
	if len(result.Changes) != 1 {
		t.Fatalf("Fetch returned changes %+v, want the ref update to still have applied", result.Changes)
	}
}

func TestFetchDeduplicatesWantsSharedByABranchAndATag(t *testing.T) {
	r := newTestRepo(t, defaultRemoteConfig("git://example.com/repo.git"))
	rem := loadTestRemote(t, r, "origin")

	server := newFakeObjectStore()
	head := server.putCommit(time.Unix(1_700_000_000, 0).UTC(), "root")
	adv := transport.Advertisement{Refs: []transport.Ref{
		{Name: "refs/heads/master", ID: head},
		{Name: "refs/tags/v1", ID: head},
	}}
	fake := withDial(t, &fakeSession{adv: adv}, nil)
	fake.fetchFunc = func(context.Context, transport.FetchRequest, transport.Negotiator) (*transport.FetchResponse, error) {
		return packResponse(t, server, []hash.ObjectID{head}), nil
	}

	if _, err := Fetch(t.Context(), r, rem, FetchOptions{Tags: TagsAll}); err != nil {
		t.Fatalf("Fetch returned error %v", err)
	}
	if len(fake.lastReq.Wants) != 1 {
		t.Fatalf("Fetch sent wants %v, want a single deduplicated id", fake.lastReq.Wants)
	}
}

func TestFetchDescribesANonBranchNonTagRefInFetchHead(t *testing.T) {
	r := newTestRepo(t, "[remote \"origin\"]\n\turl = git://example.com/repo.git\n\tfetch = refs/notes/x:refs/notes/x\n")
	rem := loadTestRemote(t, r, "origin")

	server := newFakeObjectStore()
	head := server.putCommit(time.Unix(1_700_000_000, 0).UTC(), "root")
	adv := transport.Advertisement{Refs: []transport.Ref{{Name: "refs/notes/x", ID: head}}}
	fake := withDial(t, &fakeSession{adv: adv}, nil)
	fake.fetchFunc = func(context.Context, transport.FetchRequest, transport.Negotiator) (*transport.FetchResponse, error) {
		return packResponse(t, server, []hash.ObjectID{head}), nil
	}

	if _, err := Fetch(t.Context(), r, rem, FetchOptions{}); err != nil {
		t.Fatalf("Fetch returned error %v", err)
	}
	data, err := r.Root().ReadFile(fetchHeadFile)
	if err != nil {
		t.Fatalf("reading FETCH_HEAD returned error %v", err)
	}
	want := head.String() + "\t\t'refs/notes/x' of git://example.com/repo.git\n"
	if string(data) != want {
		t.Fatalf("FETCH_HEAD = %q, want %q", data, want)
	}
}
