package ops

import (
	"errors"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/refs"
)

func TestPushFinishedFlowSendsAReleaseFinishedWithoutPush(t *testing.T) {
	r, _ := newFlowRepo(t)
	server := flowServer(t, r)
	startFlowRelease(t, r, "1.0", originFlow)
	commitFlowFile(t, r, "VERSION", "1.0\n", "bump")
	pushFlowBranch(t, r, "release/1.0")
	opts := FinishFlowOptions{DeleteBranch: true, Network: originFlow}

	result, err := FinishFlow(t.Context(), r.repo, FlowKindRelease, "1.0", opts)
	if err != nil || !result.Finished() || result.Pushed {
		t.Fatalf("FinishFlow = %+v, %v", result, err)
	}
	if _, err := server.refs().Lookup(refs.TagName("1.0")); !errors.Is(err, refs.ErrNotFound) {
		t.Fatalf("finishing without a push sent the tag: %v", err)
	}

	if err := PushFinishedFlow(t.Context(), r.repo, FlowKindRelease, "1.0", opts); err != nil {
		t.Fatalf("PushFinishedFlow returned error %v", err)
	}

	tag, err := r.refs().Lookup(refs.TagName("1.0"))
	if err != nil {
		t.Fatalf("Lookup returned error %v", err)
	}
	store := server.refs()
	for name, want := range map[refs.Name]hash.ObjectID{
		refs.BranchName("main"):    r.branchTarget("main"),
		refs.BranchName("develop"): r.branchTarget("develop"),
		refs.TagName("1.0"):        tag.Target,
	} {
		got, err := store.Lookup(name)
		if err != nil || got.Target != want {
			t.Fatalf("server %s = %+v, %v; want %s", name, got, err, want)
		}
	}
	if _, err := store.Lookup(refs.BranchName("release/1.0")); !errors.Is(err, refs.ErrNotFound) {
		t.Fatalf("server release branch lookup = %v, want it removed", err)
	}
}

func TestFinishReportsThatItPushed(t *testing.T) {
	r, _ := newFlowRepo(t)
	flowServer(t, r)
	startFlowRelease(t, r, "1.0", originFlow)

	result, err := FinishFlow(t.Context(), r.repo, FlowKindRelease, "1.0", FinishFlowOptions{Push: true, Network: originFlow})

	if err != nil || !result.Pushed {
		t.Fatalf("FinishFlow = %+v, %v", result, err)
	}
}

func TestPushFinishedFlowNeedsAConfiguredFlowAndAValidName(t *testing.T) {
	if err := PushFinishedFlow(t.Context(), newTestRepo(t).repo, FlowKindRelease, "1.0", FinishFlowOptions{Network: originFlow}); !errors.Is(err, ErrFlowNotConfigured) {
		t.Fatalf("unconfigured PushFinishedFlow returned %v", err)
	}
	r, _ := newFlowRepo(t)
	if err := PushFinishedFlow(t.Context(), r.repo, FlowKindRelease, "a:b", FinishFlowOptions{Network: originFlow}); err == nil {
		t.Fatal("PushFinishedFlow accepted a name that cannot be a refspec")
	}
}
