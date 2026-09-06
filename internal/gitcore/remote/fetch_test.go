package remote

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/refspec"
	"github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/gitcore/transport"
)

func defaultRemoteConfig(url string) string {
	return "[remote \"origin\"]\n\turl = " + url + "\n\tfetch = +refs/heads/*:refs/remotes/origin/*\n"
}

func loadTestRemote(t *testing.T, r *repo.Repository, name string) Remote {
	t.Helper()
	rem, err := Load(r.Config(), name)
	if err != nil {
		t.Fatalf("Load returned error %v", err)
	}
	return rem
}

func packResponse(t *testing.T, server *fakeObjectStore, ids []hash.ObjectID) *transport.FetchResponse {
	t.Helper()
	return &transport.FetchResponse{Pack: io.NopCloser(bytes.NewReader(server.buildPack(t, ids)))}
}

func TestFetchFailsWithoutAURL(t *testing.T) {
	r := newTestRepo(t, "")
	if _, err := Fetch(t.Context(), r, Remote{Name: "origin"}, FetchOptions{}); !errors.Is(err, ErrNoURL) {
		t.Fatalf("Fetch returned %v, want %v", err, ErrNoURL)
	}
}

func TestFetchPropagatesDialErrors(t *testing.T) {
	r := newTestRepo(t, defaultRemoteConfig("git://example.com/repo.git"))
	rem := loadTestRemote(t, r, "origin")
	wantErr := errors.New("boom")
	withDial(t, nil, wantErr)
	if _, err := Fetch(t.Context(), r, rem, FetchOptions{}); !errors.Is(err, wantErr) {
		t.Fatalf("Fetch returned %v, want %v", err, wantErr)
	}
}

func TestFetchPropagatesAdvertiseErrors(t *testing.T) {
	r := newTestRepo(t, defaultRemoteConfig("git://example.com/repo.git"))
	rem := loadTestRemote(t, r, "origin")
	wantErr := errors.New("boom")
	withDial(t, &fakeSession{advErr: wantErr}, nil)
	if _, err := Fetch(t.Context(), r, rem, FetchOptions{}); !errors.Is(err, wantErr) {
		t.Fatalf("Fetch returned %v, want %v", err, wantErr)
	}
}

func TestFetchWithNoMatchingRefsIsANoop(t *testing.T) {
	r := newTestRepo(t, defaultRemoteConfig("git://example.com/repo.git"))
	rem := loadTestRemote(t, r, "origin")
	adv := transport.Advertisement{Refs: []transport.Ref{{Name: "refs/tags/nothing-here", ID: hash.Zero}}, Head: "refs/heads/master"}
	fake := withDial(t, &fakeSession{adv: adv}, nil)
	fake.fetchFunc = func(context.Context, transport.FetchRequest, transport.Negotiator) (*transport.FetchResponse, error) {
		t.Fatal("Fetch should not have called session.Fetch")
		return nil, nil
	}

	result, err := Fetch(t.Context(), r, rem, FetchOptions{})
	if err != nil {
		t.Fatalf("Fetch returned error %v", err)
	}
	if len(result.Changes) != 0 {
		t.Fatalf("Fetch returned changes %v, want none", result.Changes)
	}
	if result.Head != "refs/heads/master" {
		t.Fatalf("Fetch returned head %q", result.Head)
	}
}

func TestFetchCreatesARemoteTrackingBranch(t *testing.T) {
	r := newTestRepo(t, defaultRemoteConfig("git://example.com/repo.git"))
	rem := loadTestRemote(t, r, "origin")

	server := newFakeObjectStore()
	head := server.putCommit(time.Unix(1_700_000_000, 0).UTC(), "root")
	adv := transport.Advertisement{Refs: []transport.Ref{{Name: "refs/heads/master", ID: head}}, Head: "refs/heads/master"}
	fake := withDial(t, &fakeSession{adv: adv}, nil)
	fake.fetchFunc = func(context.Context, transport.FetchRequest, transport.Negotiator) (*transport.FetchResponse, error) {
		return packResponse(t, server, []hash.ObjectID{head}), nil
	}

	result, err := Fetch(t.Context(), r, rem, FetchOptions{})
	if err != nil {
		t.Fatalf("Fetch returned error %v", err)
	}
	if len(result.Changes) != 1 || !result.Changes[0].Created || result.Changes[0].New != head {
		t.Fatalf("Fetch returned changes %+v", result.Changes)
	}
	if result.Objects != 1 {
		t.Fatalf("Fetch reported %d objects, want 1", result.Objects)
	}
	db := openTestODB(t, r)
	has, err := db.Has(head)
	if err != nil || !has {
		t.Fatalf("local object database is missing %s (err %v)", head, err)
	}
	store := openTestRefs(t, r, db)
	ref, err := store.Lookup(refs.RemoteBranchName("origin", "master"))
	if err != nil || ref.Target != head {
		t.Fatalf("refs/remotes/origin/master = %+v, err %v, want %s", ref, err, head)
	}
	data, err := r.Root().ReadFile(fetchHeadFile)
	if err != nil {
		t.Fatalf("reading FETCH_HEAD returned error %v", err)
	}
	line := head.String() + "\t\tbranch 'master' of git://example.com/repo.git\n"
	if string(data) != line {
		t.Fatalf("FETCH_HEAD = %q, want %q", data, line)
	}
}

func TestFetchAppliesAFastForwardUpdateWithoutForce(t *testing.T) {
	r := newTestRepo(t, "[remote \"origin\"]\n\turl = git://example.com/repo.git\n\tfetch = refs/heads/master:refs/remotes/origin/master\n")
	rem := loadTestRemote(t, r, "origin")
	db := openTestODB(t, r)
	store := openTestRefs(t, r, db)

	server := newFakeObjectStore()
	base := time.Unix(1_700_000_000, 0).UTC()
	root := server.putCommit(base, "root")
	putLocalCommit(t, db, base, "root")
	setLocalBranch(t, store, refs.RemoteBranchName("origin", "master"), root)
	next := server.putCommit(base.Add(time.Hour), "next", root)

	adv := transport.Advertisement{Refs: []transport.Ref{{Name: "refs/heads/master", ID: next}}}
	fake := withDial(t, &fakeSession{adv: adv}, nil)
	fake.fetchFunc = func(context.Context, transport.FetchRequest, transport.Negotiator) (*transport.FetchResponse, error) {
		return packResponse(t, server, []hash.ObjectID{root, next}), nil
	}

	result, err := Fetch(t.Context(), r, rem, FetchOptions{})
	if err != nil {
		t.Fatalf("Fetch returned error %v", err)
	}
	if len(result.Changes) != 1 || result.Changes[0].Forced || result.Changes[0].Old != root || result.Changes[0].New != next {
		t.Fatalf("Fetch returned changes %+v", result.Changes)
	}
}

func TestFetchRejectsANonFastForwardWithoutForce(t *testing.T) {
	r := newTestRepo(t, "[remote \"origin\"]\n\turl = git://example.com/repo.git\n\tfetch = refs/heads/master:refs/remotes/origin/master\n")
	rem := loadTestRemote(t, r, "origin")
	db := openTestODB(t, r)
	store := openTestRefs(t, r, db)

	server := newFakeObjectStore()
	base := time.Unix(1_700_000_000, 0).UTC()
	oldRoot := server.putCommit(base, "old-root")
	putLocalCommit(t, db, base, "old-root")
	setLocalBranch(t, store, refs.RemoteBranchName("origin", "master"), oldRoot)
	divergent := server.putCommit(base.Add(time.Hour), "divergent")

	adv := transport.Advertisement{Refs: []transport.Ref{{Name: "refs/heads/master", ID: divergent}}}
	fake := withDial(t, &fakeSession{adv: adv}, nil)
	fake.fetchFunc = func(context.Context, transport.FetchRequest, transport.Negotiator) (*transport.FetchResponse, error) {
		return packResponse(t, server, []hash.ObjectID{divergent}), nil
	}

	result, err := Fetch(t.Context(), r, rem, FetchOptions{})
	if !errors.Is(err, ErrNonFastForward) {
		t.Fatalf("Fetch returned %v, want %v", err, ErrNonFastForward)
	}
	if len(result.Changes) != 0 {
		t.Fatalf("Fetch returned changes %+v, want none", result.Changes)
	}
	ref, err := store.Lookup(refs.RemoteBranchName("origin", "master"))
	if err != nil || ref.Target != oldRoot {
		t.Fatalf("refs/remotes/origin/master = %+v, err %v, want unchanged at %s", ref, err, oldRoot)
	}
}

func TestFetchAppliesAForcedNonFastForwardUpdate(t *testing.T) {
	r := newTestRepo(t, "[remote \"origin\"]\n\turl = git://example.com/repo.git\n\tfetch = refs/heads/master:refs/remotes/origin/master\n")
	rem := loadTestRemote(t, r, "origin")
	db := openTestODB(t, r)
	store := openTestRefs(t, r, db)

	server := newFakeObjectStore()
	base := time.Unix(1_700_000_000, 0).UTC()
	oldRoot := server.putCommit(base, "old-root")
	putLocalCommit(t, db, base, "old-root")
	setLocalBranch(t, store, refs.RemoteBranchName("origin", "master"), oldRoot)
	divergent := server.putCommit(base.Add(time.Hour), "divergent")

	adv := transport.Advertisement{Refs: []transport.Ref{{Name: "refs/heads/master", ID: divergent}}}
	fake := withDial(t, &fakeSession{adv: adv}, nil)
	fake.fetchFunc = func(context.Context, transport.FetchRequest, transport.Negotiator) (*transport.FetchResponse, error) {
		return packResponse(t, server, []hash.ObjectID{divergent}), nil
	}

	result, err := Fetch(t.Context(), r, rem, FetchOptions{Force: true})
	if err != nil {
		t.Fatalf("Fetch returned error %v", err)
	}
	if len(result.Changes) != 1 || !result.Changes[0].Forced || result.Changes[0].New != divergent {
		t.Fatalf("Fetch returned changes %+v", result.Changes)
	}
}

func TestFetchPrunesRemoteTrackingRefsGoneFromTheServer(t *testing.T) {
	r := newTestRepo(t, defaultRemoteConfig("git://example.com/repo.git"))
	rem := loadTestRemote(t, r, "origin")
	db := openTestODB(t, r)
	store := openTestRefs(t, r, db)

	server := newFakeObjectStore()
	head := server.putCommit(time.Unix(1_700_000_000, 0).UTC(), "root")
	gone := putLocalCommit(t, db, time.Unix(1_600_000_000, 0).UTC(), "gone")
	setLocalBranch(t, store, refs.RemoteBranchName("origin", "gone"), gone)

	adv := transport.Advertisement{Refs: []transport.Ref{{Name: "refs/heads/master", ID: head}}}
	fake := withDial(t, &fakeSession{adv: adv}, nil)
	fake.fetchFunc = func(context.Context, transport.FetchRequest, transport.Negotiator) (*transport.FetchResponse, error) {
		return packResponse(t, server, []hash.ObjectID{head}), nil
	}

	result, err := Fetch(t.Context(), r, rem, FetchOptions{Prune: true})
	if err != nil {
		t.Fatalf("Fetch returned error %v", err)
	}
	foundDeleted := false
	for _, change := range result.Changes {
		if change.Deleted && change.Name == refs.RemoteBranchName("origin", "gone") {
			foundDeleted = true
		}
	}
	if !foundDeleted {
		t.Fatalf("Fetch returned changes %+v, want a deletion of origin/gone", result.Changes)
	}
	if _, err := store.Lookup(refs.RemoteBranchName("origin", "gone")); !errors.Is(err, refs.ErrNotFound) {
		t.Fatalf("origin/gone still resolves: %v", err)
	}
}

func TestFetchWithoutPruneKeepsStaleRemoteTrackingRefs(t *testing.T) {
	r := newTestRepo(t, defaultRemoteConfig("git://example.com/repo.git"))
	rem := loadTestRemote(t, r, "origin")
	db := openTestODB(t, r)
	store := openTestRefs(t, r, db)

	server := newFakeObjectStore()
	head := server.putCommit(time.Unix(1_700_000_000, 0).UTC(), "root")
	gone := putLocalCommit(t, db, time.Unix(1_600_000_000, 0).UTC(), "gone")
	setLocalBranch(t, store, refs.RemoteBranchName("origin", "gone"), gone)

	adv := transport.Advertisement{Refs: []transport.Ref{{Name: "refs/heads/master", ID: head}}}
	fake := withDial(t, &fakeSession{adv: adv}, nil)
	fake.fetchFunc = func(context.Context, transport.FetchRequest, transport.Negotiator) (*transport.FetchResponse, error) {
		return packResponse(t, server, []hash.ObjectID{head}), nil
	}

	if _, err := Fetch(t.Context(), r, rem, FetchOptions{}); err != nil {
		t.Fatalf("Fetch returned error %v", err)
	}
	if ref, err := store.Lookup(refs.RemoteBranchName("origin", "gone")); err != nil || ref.Target != gone {
		t.Fatalf("origin/gone = %+v, err %v, want unchanged at %s", ref, err, gone)
	}
}

func TestFetchWritesFetchHeadWithThePrimaryLineFirst(t *testing.T) {
	r := newTestRepo(t, defaultRemoteConfig("git://example.com/repo.git"))
	rem := loadTestRemote(t, r, "origin")

	server := newFakeObjectStore()
	master := server.putCommit(time.Unix(1_700_000_000, 0).UTC(), "master")
	other := server.putCommit(time.Unix(1_700_000_100, 0).UTC(), "other")
	adv := transport.Advertisement{
		Refs: []transport.Ref{
			{Name: "refs/heads/master", ID: master},
			{Name: "refs/heads/other", ID: other},
		},
		Head: "refs/heads/master",
	}
	fake := withDial(t, &fakeSession{adv: adv}, nil)
	fake.fetchFunc = func(context.Context, transport.FetchRequest, transport.Negotiator) (*transport.FetchResponse, error) {
		return packResponse(t, server, []hash.ObjectID{master, other}), nil
	}

	if _, err := Fetch(t.Context(), r, rem, FetchOptions{}); err != nil {
		t.Fatalf("Fetch returned error %v", err)
	}
	data, err := r.Root().ReadFile(fetchHeadFile)
	if err != nil {
		t.Fatalf("reading FETCH_HEAD returned error %v", err)
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("FETCH_HEAD has %d lines, want 2: %q", len(lines), data)
	}
	if !strings.HasPrefix(lines[0], master.String()+"\t\t") {
		t.Fatalf("first FETCH_HEAD line is %q, want the master ref marked for merge", lines[0])
	}
	if !strings.HasPrefix(lines[1], other.String()+"\tnot-for-merge\t") {
		t.Fatalf("second FETCH_HEAD line is %q, want other marked not-for-merge", lines[1])
	}
}

func TestFetchMarksEveryExplicitlyRequestedRefForMerge(t *testing.T) {
	r := newTestRepo(t, defaultRemoteConfig("git://example.com/repo.git"))
	rem := loadTestRemote(t, r, "origin")

	server := newFakeObjectStore()
	master := server.putCommit(time.Unix(1_700_000_000, 0).UTC(), "master")
	other := server.putCommit(time.Unix(1_700_000_100, 0).UTC(), "other")
	adv := transport.Advertisement{Refs: []transport.Ref{
		{Name: "refs/heads/master", ID: master},
		{Name: "refs/heads/other", ID: other},
	}}
	fake := withDial(t, &fakeSession{adv: adv}, nil)
	fake.fetchFunc = func(context.Context, transport.FetchRequest, transport.Negotiator) (*transport.FetchResponse, error) {
		return packResponse(t, server, []hash.ObjectID{master, other}), nil
	}

	specs, err := refspec.ParseAll([]string{
		"refs/heads/master:refs/remotes/origin/master",
		"refs/heads/other:refs/remotes/origin/other",
	})
	if err != nil {
		t.Fatalf("refspec.ParseAll returned error %v", err)
	}
	if _, err := Fetch(t.Context(), r, rem, FetchOptions{Refspecs: specs}); err != nil {
		t.Fatalf("Fetch returned error %v", err)
	}
	data, err := r.Root().ReadFile(fetchHeadFile)
	if err != nil {
		t.Fatalf("reading FETCH_HEAD returned error %v", err)
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("FETCH_HEAD has %d lines, want 2: %q", len(lines), data)
	}
	for _, line := range lines {
		if !strings.Contains(line, "\t\t") {
			t.Fatalf("FETCH_HEAD line %q is not marked for merge, want every explicitly requested ref for-merge like git does", line)
		}
	}
}

func TestFetchAppliesShallowInfoFromTheResponse(t *testing.T) {
	r := newTestRepo(t, defaultRemoteConfig("git://example.com/repo.git"))
	rem := loadTestRemote(t, r, "origin")

	server := newFakeObjectStore()
	head := server.putCommit(time.Unix(1_700_000_000, 0).UTC(), "root")
	adv := transport.Advertisement{Refs: []transport.Ref{{Name: "refs/heads/master", ID: head}}}
	fake := withDial(t, &fakeSession{adv: adv}, nil)
	fake.fetchFunc = func(context.Context, transport.FetchRequest, transport.Negotiator) (*transport.FetchResponse, error) {
		resp := packResponse(t, server, []hash.ObjectID{head})
		resp.Shallow = []hash.ObjectID{head}
		return resp, nil
	}

	result, err := Fetch(t.Context(), r, rem, FetchOptions{Depth: 1})
	if err != nil {
		t.Fatalf("Fetch returned error %v", err)
	}
	if len(result.Shallow) != 1 || result.Shallow[0] != head {
		t.Fatalf("Fetch returned shallow %v, want [%s]", result.Shallow, head)
	}
	shallow, err := r.Shallow()
	if err != nil {
		t.Fatalf("Shallow returned error %v", err)
	}
	if _, ok := shallow[head]; !ok || len(shallow) != 1 {
		t.Fatalf("Shallow returned %v, want only %s", shallow, head)
	}
	if fake.lastReq.Depth != 1 {
		t.Fatalf("Fetch sent depth %d, want 1", fake.lastReq.Depth)
	}
}

func TestFetchTagsNoneIgnoresTags(t *testing.T) {
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

	result, err := Fetch(t.Context(), r, rem, FetchOptions{Tags: TagsNone})
	if err != nil {
		t.Fatalf("Fetch returned error %v", err)
	}
	for _, change := range result.Changes {
		if change.Name.IsTag() {
			t.Fatalf("Fetch created tag %s with TagsNone", change.Name)
		}
	}
	if fake.lastReq.IncludeTags {
		t.Fatal("Fetch requested IncludeTags with TagsNone")
	}
}

func TestFetchTagsAllFetchesEveryTagRegardlessOfReachability(t *testing.T) {
	r := newTestRepo(t, defaultRemoteConfig("git://example.com/repo.git"))
	rem := loadTestRemote(t, r, "origin")

	server := newFakeObjectStore()
	head := server.putCommit(time.Unix(1_700_000_000, 0).UTC(), "root")
	unrelated := server.putCommit(time.Unix(1_600_000_000, 0).UTC(), "unrelated")
	adv := transport.Advertisement{Refs: []transport.Ref{
		{Name: "refs/heads/master", ID: head},
		{Name: "refs/tags/v1", ID: unrelated},
	}}
	fake := withDial(t, &fakeSession{adv: adv}, nil)
	fake.fetchFunc = func(context.Context, transport.FetchRequest, transport.Negotiator) (*transport.FetchResponse, error) {
		return packResponse(t, server, []hash.ObjectID{head, unrelated}), nil
	}

	result, err := Fetch(t.Context(), r, rem, FetchOptions{Tags: TagsAll})
	if err != nil {
		t.Fatalf("Fetch returned error %v", err)
	}
	found := false
	for _, change := range result.Changes {
		if change.Name == refs.TagName("v1") && change.New == unrelated {
			found = true
		}
	}
	if !found {
		t.Fatalf("Fetch returned changes %+v, want refs/tags/v1", result.Changes)
	}
}

func TestFetchTagsFollowSkipsATagWhoseObjectWasNotDelivered(t *testing.T) {
	r := newTestRepo(t, defaultRemoteConfig("git://example.com/repo.git"))
	rem := loadTestRemote(t, r, "origin")

	server := newFakeObjectStore()
	when := time.Unix(1_700_000_000, 0).UTC()
	head := server.putCommit(when, "root")
	tagID := server.putTag(when, "v1", head)
	adv := transport.Advertisement{Refs: []transport.Ref{
		{Name: "refs/heads/master", ID: head},
		{Name: "refs/tags/v1", ID: tagID, Peeled: head},
	}}
	fake := withDial(t, &fakeSession{adv: adv}, nil)
	fake.fetchFunc = func(context.Context, transport.FetchRequest, transport.Negotiator) (*transport.FetchResponse, error) {
		return packResponse(t, server, []hash.ObjectID{head}), nil
	}

	result, err := Fetch(t.Context(), r, rem, FetchOptions{Tags: TagsFollow})
	if err != nil {
		t.Fatalf("Fetch returned error %v", err)
	}
	for _, change := range result.Changes {
		if change.Name.IsTag() {
			t.Fatalf("Fetch created tag %s although the server never sent the tag object", change.Name)
		}
	}
	if !fake.lastReq.IncludeTags {
		t.Fatal("Fetch did not request IncludeTags with TagsFollow")
	}
}

func TestFetchTagsFollowCreatesATagWhoseObjectWasDelivered(t *testing.T) {
	r := newTestRepo(t, defaultRemoteConfig("git://example.com/repo.git"))
	rem := loadTestRemote(t, r, "origin")

	server := newFakeObjectStore()
	when := time.Unix(1_700_000_000, 0).UTC()
	head := server.putCommit(when, "root")
	tagID := server.putTag(when, "v1", head)
	adv := transport.Advertisement{Refs: []transport.Ref{
		{Name: "refs/heads/master", ID: head},
		{Name: "refs/tags/v1", ID: tagID, Peeled: head},
	}}
	fake := withDial(t, &fakeSession{adv: adv}, nil)
	fake.fetchFunc = func(context.Context, transport.FetchRequest, transport.Negotiator) (*transport.FetchResponse, error) {
		return packResponse(t, server, []hash.ObjectID{head, tagID}), nil
	}

	result, err := Fetch(t.Context(), r, rem, FetchOptions{Tags: TagsFollow})
	if err != nil {
		t.Fatalf("Fetch returned error %v", err)
	}
	found := false
	for _, change := range result.Changes {
		if change.Name == refs.TagName("v1") && change.New == tagID {
			found = true
		}
	}
	if !found {
		t.Fatalf("Fetch returned changes %+v, want refs/tags/v1", result.Changes)
	}
}

func TestFetchFailsWhenARequiredObjectIsNotReceived(t *testing.T) {
	r := newTestRepo(t, "[remote \"origin\"]\n\turl = git://example.com/repo.git\n\tfetch = refs/heads/master:refs/remotes/origin/master\n")
	rem := loadTestRemote(t, r, "origin")

	server := newFakeObjectStore()
	head := server.putCommit(time.Unix(1_700_000_000, 0).UTC(), "root")
	other := server.putCommit(time.Unix(1_700_000_100, 0).UTC(), "other")
	adv := transport.Advertisement{Refs: []transport.Ref{{Name: "refs/heads/master", ID: head}}}
	fake := withDial(t, &fakeSession{adv: adv}, nil)
	fake.fetchFunc = func(context.Context, transport.FetchRequest, transport.Negotiator) (*transport.FetchResponse, error) {
		return packResponse(t, server, []hash.ObjectID{other}), nil
	}

	result, err := Fetch(t.Context(), r, rem, FetchOptions{})
	if err == nil {
		t.Fatal("Fetch returned no error although the wanted object was never received")
	}
	if len(result.Changes) != 0 {
		t.Fatalf("Fetch returned changes %+v, want none", result.Changes)
	}
}

func TestFetchPropagatesIndexPackErrors(t *testing.T) {
	r := newTestRepo(t, defaultRemoteConfig("git://example.com/repo.git"))
	rem := loadTestRemote(t, r, "origin")

	adv := transport.Advertisement{Refs: []transport.Ref{{Name: "refs/heads/master", ID: hash.SumSHA1("commit", []byte("not-a-real-commit"))}}}
	fake := withDial(t, &fakeSession{adv: adv}, nil)
	fake.fetchFunc = func(context.Context, transport.FetchRequest, transport.Negotiator) (*transport.FetchResponse, error) {
		return &transport.FetchResponse{Pack: io.NopCloser(bytes.NewReader([]byte("not a pack")))}, nil
	}

	if _, err := Fetch(t.Context(), r, rem, FetchOptions{}); err == nil {
		t.Fatal("Fetch returned no error for a malformed pack")
	}
}

func TestFetchPropagatesFetchErrors(t *testing.T) {
	r := newTestRepo(t, defaultRemoteConfig("git://example.com/repo.git"))
	rem := loadTestRemote(t, r, "origin")

	adv := transport.Advertisement{Refs: []transport.Ref{{Name: "refs/heads/master", ID: hash.Zero}}}
	wantErr := context.Canceled
	withDial(t, &fakeSession{adv: adv, fetchErr: wantErr}, nil)

	if _, err := Fetch(t.Context(), r, rem, FetchOptions{}); !errors.Is(err, wantErr) {
		t.Fatalf("Fetch returned %v, want %v", err, wantErr)
	}
}

func TestFetchStopsNegotiatingWhenTheContextIsCancelled(t *testing.T) {
	r := newTestRepo(t, defaultRemoteConfig("git://example.com/repo.git"))
	rem := loadTestRemote(t, r, "origin")

	adv := transport.Advertisement{Refs: []transport.Ref{{Name: "refs/heads/master", ID: hash.Zero}}}
	fake := withDial(t, &fakeSession{adv: adv}, nil)
	fake.fetchFunc = func(ctx context.Context, _ transport.FetchRequest, _ transport.Negotiator) (*transport.FetchResponse, error) {
		return nil, ctx.Err()
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := Fetch(ctx, r, rem, FetchOptions{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Fetch returned %v, want context.Canceled", err)
	}
}

func TestFetchSendsExistingShallowBoundaryOnDeepen(t *testing.T) {
	r := newTestRepo(t, defaultRemoteConfig("git://example.com/repo.git"))
	rem := loadTestRemote(t, r, "origin")
	boundary := hash.SumSHA1("commit", []byte("boundary"))
	if err := r.WriteShallow([]hash.ObjectID{boundary}); err != nil {
		t.Fatalf("WriteShallow returned error %v", err)
	}

	server := newFakeObjectStore()
	head := server.putCommit(time.Unix(1_700_000_000, 0).UTC(), "root")
	adv := transport.Advertisement{Refs: []transport.Ref{{Name: "refs/heads/master", ID: head}}}
	fake := withDial(t, &fakeSession{adv: adv}, nil)
	fake.fetchFunc = func(context.Context, transport.FetchRequest, transport.Negotiator) (*transport.FetchResponse, error) {
		return packResponse(t, server, []hash.ObjectID{head}), nil
	}

	if _, err := Fetch(t.Context(), r, rem, FetchOptions{Deepen: 5}); err != nil {
		t.Fatalf("Fetch returned error %v", err)
	}
	if fake.lastReq.Depth != 5 {
		t.Fatalf("Fetch sent depth %d, want 5", fake.lastReq.Depth)
	}
	if len(fake.lastReq.Shallow) != 1 || fake.lastReq.Shallow[0] != boundary {
		t.Fatalf("Fetch sent shallow boundary %v, want [%s]", fake.lastReq.Shallow, boundary)
	}
}

func TestFetchMergesNewShallowInfoIntoAnExistingBoundary(t *testing.T) {
	r := newTestRepo(t, defaultRemoteConfig("git://example.com/repo.git"))
	rem := loadTestRemote(t, r, "origin")
	oldBoundary := hash.SumSHA1("commit", []byte("old-boundary"))
	if err := r.WriteShallow([]hash.ObjectID{oldBoundary}); err != nil {
		t.Fatalf("WriteShallow returned error %v", err)
	}

	server := newFakeObjectStore()
	head := server.putCommit(time.Unix(1_700_000_000, 0).UTC(), "root")
	adv := transport.Advertisement{Refs: []transport.Ref{{Name: "refs/heads/master", ID: head}}}
	fake := withDial(t, &fakeSession{adv: adv}, nil)
	fake.fetchFunc = func(context.Context, transport.FetchRequest, transport.Negotiator) (*transport.FetchResponse, error) {
		resp := packResponse(t, server, []hash.ObjectID{head})
		resp.Shallow = []hash.ObjectID{head}
		resp.Unshallow = []hash.ObjectID{oldBoundary}
		return resp, nil
	}

	if _, err := Fetch(t.Context(), r, rem, FetchOptions{Deepen: 5}); err != nil {
		t.Fatalf("Fetch returned error %v", err)
	}
	shallow, err := r.Shallow()
	if err != nil {
		t.Fatalf("Shallow returned error %v", err)
	}
	if _, ok := shallow[oldBoundary]; ok {
		t.Fatalf("Shallow still contains the unshallowed boundary %s", oldBoundary)
	}
	if _, ok := shallow[head]; !ok || len(shallow) != 1 {
		t.Fatalf("Shallow returned %v, want only %s", shallow, head)
	}
}

func TestFetchUnshallowRequestsMaximalDepth(t *testing.T) {
	r := newTestRepo(t, defaultRemoteConfig("git://example.com/repo.git"))
	rem := loadTestRemote(t, r, "origin")

	server := newFakeObjectStore()
	head := server.putCommit(time.Unix(1_700_000_000, 0).UTC(), "root")
	adv := transport.Advertisement{Refs: []transport.Ref{{Name: "refs/heads/master", ID: head}}}
	fake := withDial(t, &fakeSession{adv: adv}, nil)
	fake.fetchFunc = func(context.Context, transport.FetchRequest, transport.Negotiator) (*transport.FetchResponse, error) {
		resp := packResponse(t, server, []hash.ObjectID{head})
		resp.Unshallow = []hash.ObjectID{head}
		return resp, nil
	}

	result, err := Fetch(t.Context(), r, rem, FetchOptions{Unshallow: true})
	if err != nil {
		t.Fatalf("Fetch returned error %v", err)
	}
	if fake.lastReq.Depth == 0 {
		t.Fatal("Fetch did not request an increased depth for Unshallow")
	}
	if len(result.Unshallow) != 1 || result.Unshallow[0] != head {
		t.Fatalf("Fetch returned unshallow %v, want [%s]", result.Unshallow, head)
	}
}

func TestFetchOptionsRefspecsOverrideTheRemoteConfiguration(t *testing.T) {
	r := newTestRepo(t, defaultRemoteConfig("git://example.com/repo.git"))
	rem := loadTestRemote(t, r, "origin")

	server := newFakeObjectStore()
	head := server.putCommit(time.Unix(1_700_000_000, 0).UTC(), "root")
	adv := transport.Advertisement{Refs: []transport.Ref{{Name: "refs/heads/master", ID: head}}}
	fake := withDial(t, &fakeSession{adv: adv}, nil)
	fake.fetchFunc = func(context.Context, transport.FetchRequest, transport.Negotiator) (*transport.FetchResponse, error) {
		return packResponse(t, server, []hash.ObjectID{head}), nil
	}

	spec, err := refspec.Parse("refs/heads/master:refs/heads/imported")
	if err != nil {
		t.Fatalf("refspec.Parse returned error %v", err)
	}
	result, err := Fetch(t.Context(), r, rem, FetchOptions{Refspecs: []refspec.RefSpec{spec}})
	if err != nil {
		t.Fatalf("Fetch returned error %v", err)
	}
	if len(result.Changes) != 1 || result.Changes[0].Name != refs.BranchName("imported") {
		t.Fatalf("Fetch returned changes %+v, want refs/heads/imported", result.Changes)
	}
}

func TestLsRemoteReturnsTheAdvertisedRefs(t *testing.T) {
	adv := transport.Advertisement{Refs: []transport.Ref{{Name: "refs/heads/master", ID: hash.Zero}}}
	withDial(t, &fakeSession{adv: adv}, nil)

	got, err := LsRemote(t.Context(), "git://example.com/repo.git", transport.Options{})
	if err != nil {
		t.Fatalf("LsRemote returned error %v", err)
	}
	if len(got) != 1 || got[0].Name != "refs/heads/master" {
		t.Fatalf("LsRemote returned %+v", got)
	}
}

func TestLsRemotePropagatesDialErrors(t *testing.T) {
	wantErr := errors.New("boom")
	withDial(t, nil, wantErr)
	if _, err := LsRemote(t.Context(), "git://example.com/repo.git", transport.Options{}); !errors.Is(err, wantErr) {
		t.Fatalf("LsRemote returned %v, want %v", err, wantErr)
	}
}

func TestLsRemotePropagatesAdvertiseErrors(t *testing.T) {
	wantErr := errors.New("boom")
	withDial(t, &fakeSession{advErr: wantErr}, nil)
	if _, err := LsRemote(t.Context(), "git://example.com/repo.git", transport.Options{}); !errors.Is(err, wantErr) {
		t.Fatalf("LsRemote returned %v, want %v", err, wantErr)
	}
}
