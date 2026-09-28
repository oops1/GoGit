package remote

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/gitcore/transport"
)

func withDialPerURL(t *testing.T, open func(url string) (transport.Session, error)) *[]string {
	t.Helper()
	dialed := &[]string{}
	previous := dial
	dial = func(_ context.Context, url string, _ transport.Service, _ transport.Options) (transport.Session, error) {
		*dialed = append(*dialed, url)
		return open(url)
	}
	t.Cleanup(func() { dial = previous })
	return dialed
}

func acceptingSession(t *testing.T, name string) *fakeSession {
	t.Helper()
	session := &fakeSession{}
	session.pushFunc = func(_ context.Context, req transport.PushRequest) (*transport.PushResult, error) {
		drainPack(t, req)
		return &transport.PushResult{UnpackOK: true, Refs: []transport.RefStatus{{Name: name, OK: true}}}, nil
	}
	return session
}

func repoWithMainBranch(t *testing.T) (*repo.Repository, hash.ObjectID) {
	t.Helper()
	r := newTestRepo(t, "")
	db := openTestODB(t, r)
	store := openTestRefs(t, r, db)
	commit := putCommitWithTree(t, db, testWhen(), "root", putTree(t, db))
	setLocalBranch(t, store, refs.BranchName("main"), commit)
	return r, commit
}

func mainBranchPush(t *testing.T) PushOptions {
	t.Helper()
	return PushOptions{Refspecs: mustParseSpecs(t, "refs/heads/main:refs/heads/main")}
}

func TestPushReachesEveryPushURLInOrder(t *testing.T) {
	r, commit := repoWithMainBranch(t)
	dialed := withDialPerURL(t, func(string) (transport.Session, error) {
		return acceptingSession(t, "refs/heads/main"), nil
	})

	rem := Remote{Name: "origin", URLs: []string{"fake://fetch"}, PushURLs: []string{"fake://hub", "fake://mirror"}}
	result, err := Push(t.Context(), r, rem, mainBranchPush(t))

	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if want := []string{"fake://hub", "fake://mirror"}; !slices.Equal(*dialed, want) {
		t.Fatalf("dialed = %v, want %v", *dialed, want)
	}
	if len(result.Targets) != 2 {
		t.Fatalf("targets = %+v, want one per address", result.Targets)
	}
	for _, target := range result.Targets {
		if target.Err != nil || len(target.Sent) != 1 || target.Sent[0].New != commit {
			t.Fatalf("target %s = %+v", target.URL, target)
		}
	}
	if len(result.Changes) != 1 || result.Changes[0].New != commit {
		t.Fatalf("changes = %+v, want the changes of the first address", result.Changes)
	}
}

func TestPushUsesEveryPlainURLWhenNoPushURLIsConfigured(t *testing.T) {
	r, _ := repoWithMainBranch(t)
	dialed := withDialPerURL(t, func(string) (transport.Session, error) {
		return acceptingSession(t, "refs/heads/main"), nil
	})

	rem := Remote{Name: "origin", URLs: []string{"fake://one", "fake://two"}}
	if _, err := Push(t.Context(), r, rem, mainBranchPush(t)); err != nil {
		t.Fatalf("err = %v", err)
	}

	if want := []string{"fake://one", "fake://two"}; !slices.Equal(*dialed, want) {
		t.Fatalf("dialed = %v, want %v", *dialed, want)
	}
}

func TestPushKeepsGoingWhenOneAddressFailsAndNamesItWithoutThePassword(t *testing.T) {
	r, _ := repoWithMainBranch(t)
	refused := errors.New("connection refused")
	broken := "https://valeriy:secret@hub.example/repo.git"
	dialed := withDialPerURL(t, func(url string) (transport.Session, error) {
		if url == broken {
			return nil, refused
		}
		return acceptingSession(t, "refs/heads/main"), nil
	})

	rem := Remote{Name: "origin", PushURLs: []string{broken, "fake://mirror"}}
	result, err := Push(t.Context(), r, rem, mainBranchPush(t))

	if !errors.Is(err, refused) {
		t.Fatalf("err = %v, want the failure of the first address", err)
	}
	if text := err.Error(); !strings.Contains(text, "hub.example") || !strings.Contains(text, "***") || strings.Contains(text, "secret") {
		t.Fatalf("err = %q, want the address named without its password", text)
	}
	if len(*dialed) != 2 {
		t.Fatalf("dialed = %v, want the second address tried as well", *dialed)
	}
	if len(result.Targets) != 2 || result.Targets[0].Err == nil || result.Targets[1].Err != nil {
		t.Fatalf("targets = %+v, want the first failed and the second done", result.Targets)
	}
	if len(result.Targets[1].Sent) != 1 {
		t.Fatalf("mirror sent = %+v, want the branch pushed there", result.Targets[1].Sent)
	}
}

func TestPushWithASingleAddressLeavesItsErrorUntouched(t *testing.T) {
	r, _ := repoWithMainBranch(t)
	refused := errors.New("connection refused")
	withDialPerURL(t, func(string) (transport.Session, error) { return nil, refused })

	rem := Remote{Name: "origin", PushURLs: []string{"fake://only"}}
	_, err := Push(t.Context(), r, rem, mainBranchPush(t))

	if err == nil || err.Error() != refused.Error() {
		t.Fatalf("err = %v, want the bare failure when there is nothing to tell apart", err)
	}
}

func TestPushWithoutAnyUsableAddressSaysThereIsNone(t *testing.T) {
	r, _ := repoWithMainBranch(t)

	for name, rem := range map[string]Remote{
		"no addresses at all": {Name: "origin"},
		"an empty address":    {Name: "origin", URLs: []string{""}},
		"an empty pushurl":    {Name: "origin", URLs: []string{"fake://x"}, PushURLs: []string{""}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Push(t.Context(), r, rem, mainBranchPush(t)); !errors.Is(err, ErrNoURL) {
				t.Fatalf("err = %v, want %v", err, ErrNoURL)
			}
		})
	}
}

func TestBeforeSendRunsForEveryAddressWithItsOwnURL(t *testing.T) {
	r, _ := repoWithMainBranch(t)
	withDialPerURL(t, func(string) (transport.Session, error) {
		return acceptingSession(t, "refs/heads/main"), nil
	})

	var asked []string
	opts := mainBranchPush(t)
	opts.BeforeSend = func(_ context.Context, url string, _ []PushUpdate) error {
		asked = append(asked, url)
		return nil
	}

	rem := Remote{Name: "origin", PushURLs: []string{"fake://hub", "fake://mirror"}}
	if _, err := Push(t.Context(), r, rem, opts); err != nil {
		t.Fatalf("err = %v", err)
	}

	if want := []string{"fake://hub", "fake://mirror"}; !slices.Equal(asked, want) {
		t.Fatalf("asked = %v, want the hook run for each address", asked)
	}
}

func TestPushTargetsPrefersPushURLsOverPlainOnes(t *testing.T) {
	rem := Remote{URLs: []string{"fake://fetch"}, PushURLs: []string{"fake://hub", "fake://mirror"}}

	if want := []string{"fake://hub", "fake://mirror"}; !slices.Equal(rem.PushTargets(), want) {
		t.Fatalf("targets = %v, want %v", rem.PushTargets(), want)
	}
	if rem.PushURL() != "fake://hub" {
		t.Fatalf("push url = %q, want the first target", rem.PushURL())
	}
	empty := Remote{}
	if len(empty.PushTargets()) != 0 || empty.PushURL() != "" {
		t.Fatalf("an empty remote has targets %v", empty.PushTargets())
	}
}
