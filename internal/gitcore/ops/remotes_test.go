package ops

import (
	"errors"
	"os"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/remote"
)

func TestAddRemoteWritesURLAndDefaultFetchRefspec(t *testing.T) {
	r := newTestRepo(t)
	if err := AddRemote(r.repo, "origin", "https://example.com/repo.git"); err != nil {
		t.Fatalf("AddRemote returned error %v", err)
	}
	reopened := r.reopen()
	if url, _ := reopened.Config().Get("remote.origin.url"); url != "https://example.com/repo.git" {
		t.Fatalf("remote.origin.url = %q", url)
	}
	if fetch, _ := reopened.Config().Get("remote.origin.fetch"); fetch != "+refs/heads/*:refs/remotes/origin/*" {
		t.Fatalf("remote.origin.fetch = %q", fetch)
	}
}

func TestAddRemoteFailsWhenNameAlreadyExists(t *testing.T) {
	r := newTestRepo(t)
	if err := AddRemote(r.repo, "origin", "https://example.com/a.git"); err != nil {
		t.Fatalf("AddRemote returned error %v", err)
	}
	err := AddRemote(r.repo, "origin", "https://example.com/b.git")
	if !errors.Is(err, ErrRemoteExists) {
		t.Fatalf("AddRemote returned %v, want %v", err, ErrRemoteExists)
	}
}

func TestAddRemoteRejectsInvalidName(t *testing.T) {
	r := newTestRepo(t)
	if err := AddRemote(r.repo, "", "https://example.com/a.git"); !errors.Is(err, ErrInvalidRemoteName) {
		t.Fatalf("AddRemote returned %v, want %v", err, ErrInvalidRemoteName)
	}
	if err := AddRemote(r.repo, "has space", "https://example.com/a.git"); !errors.Is(err, ErrInvalidRemoteName) {
		t.Fatalf("AddRemote returned %v, want %v", err, ErrInvalidRemoteName)
	}
}

func TestAddRemoteFailsWithoutLocalConfig(t *testing.T) {
	r := newTestRepo(t)
	if err := os.Remove(r.repo.CommonPath("config")); err != nil {
		t.Fatalf("Remove returned error %v", err)
	}
	reopened := r.reopen()
	if err := AddRemote(reopened, "origin", "https://example.com/a.git"); !errors.Is(err, ErrNoLocalConfig) {
		t.Fatalf("AddRemote returned %v, want %v", err, ErrNoLocalConfig)
	}
}

func TestRemoveRemoteDeletesItsSection(t *testing.T) {
	r := newTestRepo(t)
	if err := AddRemote(r.repo, "origin", "https://example.com/a.git"); err != nil {
		t.Fatalf("AddRemote returned error %v", err)
	}
	if err := RemoveRemote(r.repo, "origin"); err != nil {
		t.Fatalf("RemoveRemote returned error %v", err)
	}
	reopened := r.reopen()
	if _, ok := reopened.Config().Remote("origin"); ok {
		t.Fatalf("remote origin still present after RemoveRemote")
	}
}

func TestRemoveRemoteFailsWithoutLocalConfig(t *testing.T) {
	r := newTestRepo(t)
	if err := os.Remove(r.repo.CommonPath("config")); err != nil {
		t.Fatalf("Remove returned error %v", err)
	}
	reopened := r.reopen()
	if err := RemoveRemote(reopened, "origin"); !errors.Is(err, ErrNoLocalConfig) {
		t.Fatalf("RemoveRemote returned %v, want %v", err, ErrNoLocalConfig)
	}
}

func TestRemoveRemoteFailsWhenUnknown(t *testing.T) {
	r := newTestRepo(t)
	err := RemoveRemote(r.repo, "origin")
	if !errors.Is(err, remote.ErrNoRemote) {
		t.Fatalf("RemoveRemote returned %v, want %v", err, remote.ErrNoRemote)
	}
}

func TestSetRemoteURLReplacesFetchURL(t *testing.T) {
	r := newTestRepo(t)
	if err := AddRemote(r.repo, "origin", "https://example.com/a.git"); err != nil {
		t.Fatalf("AddRemote returned error %v", err)
	}
	if err := SetRemoteURL(r.repo, "origin", "https://example.com/b.git", false); err != nil {
		t.Fatalf("SetRemoteURL returned error %v", err)
	}
	reopened := r.reopen()
	if url, _ := reopened.Config().Get("remote.origin.url"); url != "https://example.com/b.git" {
		t.Fatalf("remote.origin.url = %q", url)
	}
}

func TestSetRemoteURLReplacesPushURLWhenRequested(t *testing.T) {
	r := newTestRepo(t)
	if err := AddRemote(r.repo, "origin", "https://example.com/a.git"); err != nil {
		t.Fatalf("AddRemote returned error %v", err)
	}
	if err := SetRemoteURL(r.repo, "origin", "https://example.com/push.git", true); err != nil {
		t.Fatalf("SetRemoteURL returned error %v", err)
	}
	reopened := r.reopen()
	rem, ok := reopened.Config().Remote("origin")
	if !ok || len(rem.PushURLs) != 1 || rem.PushURLs[0] != "https://example.com/push.git" {
		t.Fatalf("remote.origin.pushurl = %+v", rem)
	}
	if len(rem.URLs) != 1 || rem.URLs[0] != "https://example.com/a.git" {
		t.Fatalf("remote.origin.url changed unexpectedly: %+v", rem)
	}
}

func TestSetRemoteURLFailsWithoutLocalConfig(t *testing.T) {
	r := newTestRepo(t)
	if err := os.Remove(r.repo.CommonPath("config")); err != nil {
		t.Fatalf("Remove returned error %v", err)
	}
	reopened := r.reopen()
	if err := SetRemoteURL(reopened, "origin", "https://example.com/a.git", false); !errors.Is(err, ErrNoLocalConfig) {
		t.Fatalf("SetRemoteURL returned %v, want %v", err, ErrNoLocalConfig)
	}
}

func TestSetRemoteURLFailsWhenUnknown(t *testing.T) {
	r := newTestRepo(t)
	err := SetRemoteURL(r.repo, "origin", "https://example.com/a.git", false)
	if !errors.Is(err, remote.ErrNoRemote) {
		t.Fatalf("SetRemoteURL returned %v, want %v", err, remote.ErrNoRemote)
	}
}
