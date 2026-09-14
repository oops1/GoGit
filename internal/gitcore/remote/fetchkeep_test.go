package remote

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/gitcore/transport"
)

func keepFilesIn(t *testing.T, r *repo.Repository) []string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(r.PackDir(), "*.keep"))
	if err != nil {
		t.Fatalf("Glob returned error %v", err)
	}
	return matches
}

func recordUnlocks(t *testing.T, check func(path string)) *[]string {
	t.Helper()
	var released []string
	original := unlockPack
	unlockPack = func(path string) {
		check(path)
		released = append(released, path)
		original(path)
	}
	t.Cleanup(func() { unlockPack = original })
	return &released
}

func TestFetchHoldsAKeepFileUntilTheRefsAreUpdated(t *testing.T) {
	r := newTestRepo(t, defaultRemoteConfig("git://example.com/repo.git"))
	rem := loadTestRemote(t, r, "origin")
	server := newFakeObjectStore()
	head := server.putCommit(time.Unix(1_700_000_000, 0).UTC(), "root")
	adv := transport.Advertisement{Refs: []transport.Ref{{Name: "refs/heads/master", ID: head}}, Head: "refs/heads/master"}
	fake := withDial(t, &fakeSession{adv: adv}, nil)
	fake.fetchFunc = func(context.Context, transport.FetchRequest, transport.Negotiator) (*transport.FetchResponse, error) {
		return packResponse(t, server, []hash.ObjectID{head}), nil
	}
	released := recordUnlocks(t, func(path string) {
		data, err := os.ReadFile(path)
		if err != nil || !strings.HasPrefix(string(data), "fetch-pack ") {
			t.Errorf("the keep file %s holds %q (err %v)", path, data, err)
		}
		db := openTestODB(t, r)
		ref, err := openTestRefs(t, r, db).Lookup(refs.RemoteBranchName("origin", "master"))
		if err != nil || ref.Target != head {
			t.Errorf("the keep file was released before the remote-tracking branch was written: %+v, %v", ref, err)
		}
	})

	if _, err := Fetch(t.Context(), r, rem, FetchOptions{}); err != nil {
		t.Fatalf("Fetch returned error %v", err)
	}

	if len(*released) != 1 || len(keepFilesIn(t, r)) != 0 {
		t.Fatalf("released %v, keep files left %v", *released, keepFilesIn(t, r))
	}
}

func TestFetchReleasesTheKeepFileWhenARefUpdateIsRejected(t *testing.T) {
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
	released := recordUnlocks(t, func(path string) {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("the keep file %s was gone before its release: %v", path, err)
		}
	})

	if _, err := Fetch(t.Context(), r, rem, FetchOptions{}); !errors.Is(err, ErrNonFastForward) {
		t.Fatalf("Fetch returned %v, want %v", err, ErrNonFastForward)
	}

	if len(*released) != 1 || len(keepFilesIn(t, r)) != 0 {
		t.Fatalf("released %v, keep files left %v", *released, keepFilesIn(t, r))
	}
}
