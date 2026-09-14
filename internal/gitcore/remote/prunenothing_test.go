package remote

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/transport"
)

func TestFetchPrunesEveryTrackingRefWhenTheServerHasNoBranchesLeft(t *testing.T) {
	r := newTestRepo(t, defaultRemoteConfig("git://example.com/repo.git"))
	rem := loadTestRemote(t, r, "origin")
	db := openTestODB(t, r)
	store := openTestRefs(t, r, db)
	gone := putLocalCommit(t, db, time.Unix(1_600_000_000, 0).UTC(), "gone")
	setLocalBranch(t, store, refs.RemoteBranchName("origin", "gone"), gone)
	withDial(t, &fakeSession{adv: transport.Advertisement{}}, nil)

	kept, err := Fetch(t.Context(), r, rem, FetchOptions{})
	if err != nil || len(kept.Changes) != 0 {
		t.Fatalf("Fetch without prune = %+v, %v", kept, err)
	}
	result, err := Fetch(t.Context(), r, rem, FetchOptions{Prune: true})
	if err != nil || len(result.Changes) != 1 || !result.Changes[0].Deleted {
		t.Fatalf("Fetch with prune = %+v, %v", result, err)
	}
	if _, err := store.Lookup(refs.RemoteBranchName("origin", "gone")); !errors.Is(err, refs.ErrNotFound) {
		t.Fatalf("origin/gone still resolves: %v", err)
	}
}

func TestFetchReportsAPruneItCannotFinishWithoutMatches(t *testing.T) {
	broken := newTestRepo(t, defaultRemoteConfig("git://example.com/repo.git"))
	if err := os.MkdirAll(filepath.Join(broken.GitDir(), "refs", "remotes", "origin"), 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(broken.GitDir(), "refs", "remotes", "origin", "bad"), []byte("junk\n"), 0o666); err != nil {
		t.Fatal(err)
	}
	withDial(t, &fakeSession{adv: transport.Advertisement{}}, nil)
	if _, err := Fetch(t.Context(), broken, loadTestRemote(t, broken, "origin"), FetchOptions{Prune: true}); !errors.Is(err, refs.ErrMalformedRef) {
		t.Fatalf("Fetch over a malformed tracking ref returned %v", err)
	}

	locked := newTestRepo(t, defaultRemoteConfig("git://example.com/repo.git"))
	db := openTestODB(t, locked)
	store := openTestRefs(t, locked, db)
	setLocalBranch(t, store, refs.RemoteBranchName("origin", "gone"), putLocalCommit(t, db, time.Unix(1_600_000_000, 0).UTC(), "gone"))
	if err := os.WriteFile(filepath.Join(locked.GitDir(), "refs", "remotes", "origin", "gone.lock"), nil, 0o666); err != nil {
		t.Fatal(err)
	}
	withDial(t, &fakeSession{adv: transport.Advertisement{}}, nil)
	if _, err := Fetch(t.Context(), locked, loadTestRemote(t, locked, "origin"), FetchOptions{Prune: true}); !errors.Is(err, refs.ErrLocked) {
		t.Fatalf("Fetch over a locked tracking ref returned %v", err)
	}
}
