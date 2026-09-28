package console

import (
	"errors"
	"slices"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/ops"
	"github.com/oops1/gogit/internal/gitcore/remote"
	"github.com/oops1/gogit/internal/gitcore/transport"
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

func TestRemoteVerboseListsEveryPushAddressAndHidesPasswords(t *testing.T) {
	r := newTestRepo(t)
	r.run("remote add origin https://valeriy:secret@example.test/repo.git")
	r.reopen()
	if got := r.run("remote set-url --push --add origin https://valeriy:secret@example.test/repo.git"); got != "Added a URL to origin" {
		t.Fatalf("out = %q", got)
	}
	r.reopen()
	r.run("remote set-url --push --add origin http://mirror.test/repo.git")
	r.reopen()

	want := []string{
		"origin\thttps://valeriy:***@example.test/repo.git (fetch)",
		"origin\thttps://valeriy:***@example.test/repo.git (push)",
		"origin\thttp://mirror.test/repo.git (push)",
	}
	if got := lines(r.run("remote -v")); !slices.Equal(got, want) {
		t.Fatalf("remote -v = %#v", got)
	}
}

func TestRemoteSetURLDeletesOneAddress(t *testing.T) {
	r := newTestRepo(t)
	r.run("remote add origin https://example.test/repo.git")
	r.reopen()
	r.run("remote set-url --push --add origin https://example.test/repo.git")
	r.reopen()
	r.run("remote set-url --push --add origin http://mirror.test/repo.git")
	r.reopen()

	if got := r.run("remote set-url --push --delete origin http://mirror.test/repo.git"); got != "Removed a URL of origin" {
		t.Fatalf("out = %q", got)
	}
	r.reopen()

	want := []string{
		"origin\thttps://example.test/repo.git (fetch)",
		"origin\thttps://example.test/repo.git (push)",
	}
	if got := lines(r.run("remote -v")); !slices.Equal(got, want) {
		t.Fatalf("remote -v = %#v", got)
	}
}

func TestRemoteSetURLRefusesAddAndDeleteTogether(t *testing.T) {
	r := newTestRepo(t)
	r.run("remote add origin https://example.test/repo.git")
	r.reopen()

	if err := r.runFails("remote set-url --add --delete origin https://example.test/other.git"); !errors.Is(err, ErrUsage) {
		t.Fatalf("err = %v, want %v", err, ErrUsage)
	}
}

func TestPushPrintsEveryAddressWhenTheRemoteHasSeveral(t *testing.T) {
	result := remote.PushResult{
		Targets: []remote.TargetResult{
			{
				URL:  "https://valeriy:secret@example.test/repo.git",
				Sent: []remote.PushUpdate{{Source: "refs/heads/main", Target: "refs/heads/main"}},
			},
			{URL: "http://mirror.test/repo.git"},
			{URL: "http://down.test/repo.git", Err: errors.New("connection refused")},
		},
	}

	want := []string{
		"To https://valeriy:***@example.test/repo.git",
		"  main -> main",
		"To http://mirror.test/repo.git",
		"  Everything up-to-date",
		"To http://down.test/repo.git",
		"! connection refused",
	}
	if got := lines(formatPush(result)); !slices.Equal(got, want) {
		t.Fatalf("out = %#v", got)
	}
}

func TestRemoteSetURLReportsWhatWentWrong(t *testing.T) {
	r := newTestRepo(t)

	if err := r.runFails("remote set-url --push --add origin https://example.test/repo.git"); !errors.Is(err, remote.ErrNoRemote) {
		t.Fatalf("err = %v, want %v", err, remote.ErrNoRemote)
	}
	r.run("remote add origin https://example.test/repo.git")
	r.reopen()
	if err := r.runFails("remote set-url --push --delete origin https://example.test/missing.git"); !errors.Is(err, ops.ErrNoSuchRemoteURL) {
		t.Fatalf("err = %v, want %v", err, ops.ErrNoSuchRemoteURL)
	}
}

func TestPushPrintsWhatAMirrorRefused(t *testing.T) {
	result := remote.PushResult{
		Targets: []remote.TargetResult{
			{URL: "http://one.test/repo.git"},
			{
				URL:      "http://two.test/repo.git",
				Rejected: []transport.RefStatus{{Name: "refs/heads/main", Message: "non-fast-forward"}},
			},
		},
	}

	want := []string{
		"To http://one.test/repo.git",
		"  Everything up-to-date",
		"To http://two.test/repo.git",
		"! refs/heads/main non-fast-forward",
	}
	if got := lines(formatPush(result)); !slices.Equal(got, want) {
		t.Fatalf("out = %#v", got)
	}
}
