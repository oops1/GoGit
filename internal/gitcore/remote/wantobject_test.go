package remote

import (
	"context"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/transport"
)

func TestFetchAsksForAnObjectNoRefAdvertises(t *testing.T) {
	r := newTestRepo(t, defaultRemoteConfig("git://example.com/repo.git"))
	rem := loadTestRemote(t, r, "origin")
	server := newFakeObjectStore()
	main := server.putCommit(time.Unix(1_700_000_000, 0).UTC(), "main")
	hidden := server.putCommit(time.Unix(1_700_000_100, 0).UTC(), "hidden", main)
	fake := withDial(t, &fakeSession{adv: transport.Advertisement{Refs: []transport.Ref{{Name: "refs/heads/main", ID: main}}}}, nil)
	fake.fetchFunc = func(context.Context, transport.FetchRequest, transport.Negotiator) (*transport.FetchResponse, error) {
		return packResponse(t, server, []hash.ObjectID{main, hidden}), nil
	}

	result, err := Fetch(t.Context(), r, rem, FetchOptions{Wants: []hash.ObjectID{hidden, main}})
	if err != nil {
		t.Fatalf("Fetch returned error %v", err)
	}
	if !slices.Equal(fake.lastReq.Wants, []hash.ObjectID{main, hidden}) {
		t.Fatalf("wants = %v, want main and the hidden commit once", fake.lastReq.Wants)
	}
	if len(result.Changes) != 1 {
		t.Fatalf("changes = %+v, want only the branch", result.Changes)
	}
	if has, err := openTestODB(t, r).Has(hidden); err != nil || !has {
		t.Fatalf("the hidden commit was not fetched: %v, %v", has, err)
	}
}

func TestFetchWritesTheShallowBoundarySortedLikeGit(t *testing.T) {
	r := newTestRepo(t, defaultRemoteConfig("git://example.com/repo.git"))
	rem := loadTestRemote(t, r, "origin")
	server := newFakeObjectStore()
	head := server.putCommit(time.Unix(1_700_000_000, 0).UTC(), "root")
	var boundaries []hash.ObjectID
	for i := range 16 {
		boundaries = append(boundaries, hash.SumSHA1("commit", []byte{byte(i)}))
	}
	fake := withDial(t, &fakeSession{adv: transport.Advertisement{Refs: []transport.Ref{{Name: "refs/heads/master", ID: head}}}}, nil)
	fake.fetchFunc = func(context.Context, transport.FetchRequest, transport.Negotiator) (*transport.FetchResponse, error) {
		resp := packResponse(t, server, []hash.ObjectID{head})
		resp.Shallow = boundaries
		return resp, nil
	}

	if _, err := Fetch(t.Context(), r, rem, FetchOptions{Depth: 1}); err != nil {
		t.Fatalf("Fetch returned error %v", err)
	}
	data, err := os.ReadFile(r.CommonPath("shallow"))
	if err != nil {
		t.Fatal(err)
	}
	sorted := slices.SortedFunc(slices.Values(boundaries), hash.ObjectID.Compare)
	var want strings.Builder
	for _, id := range sorted {
		want.WriteString(id.String() + "\n")
	}
	if string(data) != want.String() {
		t.Fatalf("shallow = %q, want %q", data, want.String())
	}
}

func TestFetchAsksForAnObjectEvenWhenNoRefspecMatches(t *testing.T) {
	r := newTestRepo(t, defaultRemoteConfig("git://example.com/repo.git"))
	rem := loadTestRemote(t, r, "origin")
	server := newFakeObjectStore()
	hidden := server.putCommit(time.Unix(1_700_000_000, 0).UTC(), "hidden")
	fake := withDial(t, &fakeSession{adv: transport.Advertisement{}}, nil)
	fake.fetchFunc = func(context.Context, transport.FetchRequest, transport.Negotiator) (*transport.FetchResponse, error) {
		return packResponse(t, server, []hash.ObjectID{hidden}), nil
	}

	if _, err := Fetch(t.Context(), r, rem, FetchOptions{Wants: []hash.ObjectID{hidden}}); err != nil {
		t.Fatalf("Fetch returned error %v", err)
	}
	if !slices.Equal(fake.lastReq.Wants, []hash.ObjectID{hidden}) {
		t.Fatalf("wants = %v, want the hidden commit", fake.lastReq.Wants)
	}
}
