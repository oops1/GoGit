package console

import (
	"errors"
	"slices"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/ops"
	"github.com/oops1/gogit/internal/gitcore/remote"
)

func TestRemoteAddsListsAndRemoves(t *testing.T) {
	r := newTestRepo(t)

	if got := r.run("remote add origin https://example.test/repo.git"); got != "Added remote origin" {
		t.Fatalf("out = %q", got)
	}
	r.reopen()
	if got := r.run("remote"); got != "origin" {
		t.Fatalf("remote = %q", got)
	}
	verbose := lines(r.run("remote -v"))
	want := []string{
		"origin\thttps://example.test/repo.git (fetch)",
		"origin\thttps://example.test/repo.git (push)",
	}
	if !slices.Equal(verbose, want) {
		t.Fatalf("remote -v = %#v", verbose)
	}
	if got := r.run("remote remove origin"); got != "Removed remote origin" {
		t.Fatalf("out = %q", got)
	}
	r.reopen()
	if got := r.run("remote"); got != "" {
		t.Fatalf("remote = %q", got)
	}
}

func TestRemoteRmIsAnAliasOfRemove(t *testing.T) {
	r := newTestRepo(t)
	r.run("remote add origin https://example.test/repo.git")
	r.reopen()

	r.run("remote rm origin")
	r.reopen()

	if got := r.run("remote"); got != "" {
		t.Fatalf("remote = %q", got)
	}
}

func TestRemoteSetsTheFetchAndPushURLs(t *testing.T) {
	r := newTestRepo(t)
	r.run("remote add origin https://example.test/one.git")
	r.reopen()

	if got := r.run("remote set-url origin https://example.test/two.git"); got != "Set the URL of origin" {
		t.Fatalf("out = %q", got)
	}
	r.run("remote set-url --push origin https://example.test/push.git")
	r.reopen()

	verbose := lines(r.run("remote -v"))
	want := []string{
		"origin\thttps://example.test/two.git (fetch)",
		"origin\thttps://example.test/push.git (push)",
	}
	if !slices.Equal(verbose, want) {
		t.Fatalf("remote -v = %#v", verbose)
	}
}

func TestRemoteRefusesBadArguments(t *testing.T) {
	r := newTestRepo(t)

	for _, line := range []string{"remote nonsense", "remote extra-word", "remote add origin", "remote remove", "remote set-url origin", "remote -v extra"} {
		if err := r.runFails(line); !errors.Is(err, ErrUsage) {
			t.Fatalf("%q: err = %v", line, err)
		}
	}
	if err := r.runFails("remote set-url --bogus a b"); !errors.Is(err, ErrUnknownOption) {
		t.Fatalf("err = %v", err)
	}
}

func TestRemoteReportsCoreFailures(t *testing.T) {
	r := newTestRepo(t)
	r.run("remote add origin https://example.test/one.git")
	r.reopen()

	if err := r.runFails("remote add origin https://example.test/two.git"); !errors.Is(err, ops.ErrRemoteExists) {
		t.Fatalf("err = %v", err)
	}
	if err := r.runFails("remote remove missing"); !errors.Is(err, remote.ErrNoRemote) {
		t.Fatalf("err = %v", err)
	}
	if err := r.runFails("remote set-url missing https://example.test/x.git"); !errors.Is(err, remote.ErrNoRemote) {
		t.Fatalf("err = %v", err)
	}
}
