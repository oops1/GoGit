package ops

import (
	"errors"
	"os"
	"slices"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/remote"
)

func remoteWithTwoPushURLs(t *testing.T) *testRepo {
	t.Helper()
	r := newTestRepo(t)
	if err := AddRemote(r.repo, "origin", "https://example.com/hub.git"); err != nil {
		t.Fatalf("AddRemote returned error %v", err)
	}
	for _, url := range []string{"https://example.com/hub.git", "http://mirror.example/hub.git"} {
		if err := AddRemoteURL(r.repo, "origin", url, true); err != nil {
			t.Fatalf("AddRemoteURL returned error %v", err)
		}
	}
	return r
}

func pushURLsOf(t *testing.T, r *testRepo) []string {
	t.Helper()
	rem, ok := r.reopen().Config().Remote("origin")
	if !ok {
		t.Fatal("the reopened repository has no origin")
	}
	return rem.PushURLs
}

func TestAddRemoteURLKeepsTheAddressesAlreadyThere(t *testing.T) {
	r := remoteWithTwoPushURLs(t)

	want := []string{"https://example.com/hub.git", "http://mirror.example/hub.git"}
	if got := pushURLsOf(t, r); !slices.Equal(got, want) {
		t.Fatalf("pushurl = %v, want %v", got, want)
	}
}

func TestAddRemoteURLAppendsFetchAddressesToo(t *testing.T) {
	r := newTestRepo(t)
	if err := AddRemote(r.repo, "origin", "https://example.com/a.git"); err != nil {
		t.Fatalf("AddRemote returned error %v", err)
	}
	if err := AddRemoteURL(r.repo, "origin", "https://example.com/b.git", false); err != nil {
		t.Fatalf("AddRemoteURL returned error %v", err)
	}

	rem, _ := r.reopen().Config().Remote("origin")
	want := []string{"https://example.com/a.git", "https://example.com/b.git"}
	if !slices.Equal(rem.URLs, want) {
		t.Fatalf("url = %v, want %v", rem.URLs, want)
	}
}

func TestDeleteRemoteURLRemovesOnlyTheNamedAddress(t *testing.T) {
	r := remoteWithTwoPushURLs(t)

	if err := DeleteRemoteURL(r.repo, "origin", "http://mirror.example/hub.git", true); err != nil {
		t.Fatalf("DeleteRemoteURL returned error %v", err)
	}

	want := []string{"https://example.com/hub.git"}
	if got := pushURLsOf(t, r); !slices.Equal(got, want) {
		t.Fatalf("pushurl = %v, want %v", got, want)
	}
}

func TestDeleteRemoteURLTakesTheLastPushAddressButNotTheLastFetchOne(t *testing.T) {
	r := remoteWithTwoPushURLs(t)
	for _, url := range []string{"https://example.com/hub.git", "http://mirror.example/hub.git"} {
		if err := DeleteRemoteURL(r.repo, "origin", url, true); err != nil {
			t.Fatalf("DeleteRemoteURL returned error %v", err)
		}
	}
	if got := pushURLsOf(t, r); len(got) != 0 {
		t.Fatalf("pushurl = %v, want the remote left without its own push addresses", got)
	}

	err := DeleteRemoteURL(r.repo, "origin", "https://example.com/hub.git", false)
	if !errors.Is(err, ErrLastRemoteURL) {
		t.Fatalf("DeleteRemoteURL returned %v, want %v", err, ErrLastRemoteURL)
	}
	rem, _ := r.reopen().Config().Remote("origin")
	if len(rem.URLs) != 1 {
		t.Fatalf("url = %v, want the only fetch address kept", rem.URLs)
	}
}

func TestDeleteRemoteURLSaysWhenThereIsNoSuchAddress(t *testing.T) {
	r := remoteWithTwoPushURLs(t)

	err := DeleteRemoteURL(r.repo, "origin", "https://example.com/other.git", true)

	if !errors.Is(err, ErrNoSuchRemoteURL) {
		t.Fatalf("DeleteRemoteURL returned %v, want %v", err, ErrNoSuchRemoteURL)
	}
}

func TestRemoteURLEditsFailForAnUnknownRemoteAndWithoutLocalConfig(t *testing.T) {
	edits := map[string]func(r *testRepo) error{
		"add":    func(r *testRepo) error { return AddRemoteURL(r.repo, "origin", "https://example.com/a.git", true) },
		"delete": func(r *testRepo) error { return DeleteRemoteURL(r.repo, "origin", "https://example.com/a.git", true) },
	}
	for name, edit := range edits {
		t.Run(name+" without the remote", func(t *testing.T) {
			r := newTestRepo(t)
			if err := edit(r); !errors.Is(err, remote.ErrNoRemote) {
				t.Fatalf("err = %v, want %v", err, remote.ErrNoRemote)
			}
		})
		t.Run(name+" without a local config", func(t *testing.T) {
			r := newTestRepo(t)
			if err := os.Remove(r.repo.CommonPath("config")); err != nil {
				t.Fatalf("Remove returned error %v", err)
			}
			reopened := r.reopen()
			var err error
			if name == "add" {
				err = AddRemoteURL(reopened, "origin", "https://example.com/a.git", true)
			} else {
				err = DeleteRemoteURL(reopened, "origin", "https://example.com/a.git", true)
			}
			if !errors.Is(err, ErrNoLocalConfig) {
				t.Fatalf("err = %v, want %v", err, ErrNoLocalConfig)
			}
		})
	}
}

func TestSetRemotePushURLsReplacesTheWholeList(t *testing.T) {
	r := remoteWithTwoPushURLs(t)

	if err := SetRemotePushURLs(r.repo, "origin", []string{"http://one.example/hub.git", "http://two.example/hub.git"}); err != nil {
		t.Fatalf("SetRemotePushURLs returned error %v", err)
	}

	want := []string{"http://one.example/hub.git", "http://two.example/hub.git"}
	if got := pushURLsOf(t, r); !slices.Equal(got, want) {
		t.Fatalf("pushurl = %v, want %v", got, want)
	}
}

func TestSetRemotePushURLsWithAnEmptyListLeavesTheRemoteWithTheFetchAddress(t *testing.T) {
	r := remoteWithTwoPushURLs(t)

	if err := SetRemotePushURLs(r.repo, "origin", nil); err != nil {
		t.Fatalf("SetRemotePushURLs returned error %v", err)
	}

	loaded, err := remote.Load(r.reopen().Config(), "origin")
	if err != nil {
		t.Fatalf("remote.Load returned error %v", err)
	}
	if len(loaded.PushURLs) != 0 {
		t.Fatalf("pushurl = %v, want none", loaded.PushURLs)
	}
	if got := loaded.PushTargets(); !slices.Equal(got, []string{"https://example.com/hub.git"}) {
		t.Fatalf("push targets = %v, want the fetch address", got)
	}
}

func TestSetRemotePushURLsFailsForAnUnknownRemote(t *testing.T) {
	r := newTestRepo(t)

	if err := SetRemotePushURLs(r.repo, "origin", []string{"http://one.example/hub.git"}); !errors.Is(err, remote.ErrNoRemote) {
		t.Fatalf("err = %v, want %v", err, remote.ErrNoRemote)
	}
}
