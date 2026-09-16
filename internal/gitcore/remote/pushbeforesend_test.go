package remote

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/gitcore/transport"
)

func TestBeforeSendSeesThePlannedUpdatesAndCanStopThePush(t *testing.T) {
	boom := errors.New("refused")
	for _, refuse := range []bool{false, true} {
		r := newTestRepo(t, "")
		db := openTestODB(t, r)
		store := openTestRefs(t, r, db)
		commit := putCommitWithTree(t, db, testWhen(), "root", putTree(t, db))
		setLocalBranch(t, store, refs.BranchName("main"), commit)

		pushed := false
		fake := &fakeSession{}
		fake.pushFunc = func(_ context.Context, req transport.PushRequest) (*transport.PushResult, error) {
			pushed = true
			drainPack(t, req)
			return &transport.PushResult{UnpackOK: true, Refs: []transport.RefStatus{{Name: "refs/heads/main", OK: true}}}, nil
		}
		withDial(t, fake, nil)

		var seen []PushUpdate
		opts := PushOptions{
			Refspecs: mustParseSpecs(t, "refs/heads/main:refs/heads/main"),
			BeforeSend: func(_ context.Context, updates []PushUpdate) error {
				seen = updates
				if refuse {
					return boom
				}
				return nil
			},
		}
		_, err := Push(t.Context(), r, Remote{Name: "origin", URLs: []string{"fake://x"}}, opts)

		want := []PushUpdate{{Source: refs.BranchName("main"), New: commit, Target: refs.BranchName("main")}}
		if !slices.Equal(seen, want) {
			t.Fatalf("updates = %+v, want %+v", seen, want)
		}
		if refuse != errors.Is(err, boom) || refuse == pushed {
			t.Fatalf("refuse %v: err = %v, pushed = %v", refuse, err, pushed)
		}
	}
}
