package remote

import (
	"context"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/object"
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/refspec"
	"github.com/oops1/gogit/internal/gitcore/transport"
)

func pushAcceptingEverything(t *testing.T, names ...string) {
	t.Helper()
	statuses := make([]transport.RefStatus, 0, len(names))
	for _, name := range names {
		statuses = append(statuses, transport.RefStatus{Name: name, OK: true})
	}
	fake := &fakeSession{adv: transport.Advertisement{}}
	fake.pushFunc = func(_ context.Context, req transport.PushRequest) (*transport.PushResult, error) {
		drainPack(t, req)
		return &transport.PushResult{UnpackOK: true, Refs: statuses}, nil
	}
	withDial(t, fake, nil)
}

func collidingRemote(t *testing.T) Remote {
	t.Helper()
	x, err := refspec.Parse("refs/heads/x:refs/remotes/origin/x")
	if err != nil {
		t.Fatalf("Parse returned error %v", err)
	}
	y, err := refspec.Parse("refs/heads/y:refs/remotes/origin/x")
	if err != nil {
		t.Fatalf("Parse returned error %v", err)
	}
	return Remote{Name: "origin", Fetch: []refspec.RefSpec{x, y}}
}

func TestPushOfATagLeavesTheRemoteTrackingRefsAlone(t *testing.T) {
	r := newTestRepo(t, "")
	db := openTestODB(t, r)
	store := openTestRefs(t, r, db)
	blob := putBlob(t, db, "hello\n")
	tree := putTree(t, db, object.TreeEntry{Mode: object.ModeBlob, Name: "a.txt", ID: blob})
	commit := putCommitWithTree(t, db, testWhen(), "initial", tree)
	setLocalBranch(t, store, refs.TagName("v1.0.0"), commit)
	pushAcceptingEverything(t, "refs/tags/v1.0.0")

	result, err := Push(t.Context(), r, Remote{Name: "origin", URLs: []string{"fake://x"}}, PushOptions{Refspecs: mustParseSpecs(t, "refs/tags/v1.0.0:refs/tags/v1.0.0")})
	if err != nil {
		t.Fatalf("Push returned error %v", err)
	}

	if len(result.Changes) != 0 {
		t.Fatalf("Push of a tag changed %+v", result.Changes)
	}
	if _, existed, err := lookupCurrent(store, refs.RemoteBranchName("origin", "v1.0.0")); err != nil || existed {
		t.Fatalf("a remote-tracking branch for the tag exists: %v, %v", existed, err)
	}
}

func TestPushUpdatesTheTrackingRefTheFetchRefspecNames(t *testing.T) {
	r := newTestRepo(t, "")
	db := openTestODB(t, r)
	store := openTestRefs(t, r, db)
	blob := putBlob(t, db, "hello\n")
	tree := putTree(t, db, object.TreeEntry{Mode: object.ModeBlob, Name: "a.txt", ID: blob})
	commit := putCommitWithTree(t, db, testWhen(), "initial", tree)
	setLocalBranch(t, store, refs.BranchName("main"), commit)
	pushAcceptingEverything(t, "refs/heads/main")
	mirror, err := refspec.Parse("+refs/heads/*:refs/remotes/mirror/*")
	if err != nil {
		t.Fatalf("Parse returned error %v", err)
	}
	rem := Remote{Name: "origin", URLs: []string{"fake://x"}, Fetch: []refspec.RefSpec{mirror}}

	result, err := Push(t.Context(), r, rem, PushOptions{Refspecs: mustParseSpecs(t, "refs/heads/main:refs/heads/main")})
	if err != nil {
		t.Fatalf("Push returned error %v", err)
	}

	if len(result.Changes) != 1 || result.Changes[0].Name != refs.Name("refs/remotes/mirror/main") {
		t.Fatalf("Push changed %+v, want refs/remotes/mirror/main", result.Changes)
	}
}

func TestPushForceWithLeaseOnAnUntrackedRefExpectsNothingThere(t *testing.T) {
	r := newTestRepo(t, "")
	db := openTestODB(t, r)
	store := openTestRefs(t, r, db)
	blob := putBlob(t, db, "hello\n")
	tree := putTree(t, db, object.TreeEntry{Mode: object.ModeBlob, Name: "a.txt", ID: blob})
	commit := putCommitWithTree(t, db, testWhen(), "initial", tree)
	setLocalBranch(t, store, refs.TagName("v1.0.0"), commit)
	pushAcceptingEverything(t, "refs/tags/v1.0.0")

	result, err := Push(t.Context(), r, Remote{Name: "origin", URLs: []string{"fake://x"}}, PushOptions{
		Refspecs:       mustParseSpecs(t, "refs/tags/v1.0.0:refs/tags/v1.0.0"),
		ForceWithLease: map[string]hash.ObjectID{"refs/tags/v1.0.0": hash.Zero},
	})

	if err != nil || len(result.Rejected) != 0 {
		t.Fatalf("Push = %+v, %v; want the lease on an untracked tag to hold", result, err)
	}
}
