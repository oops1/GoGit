package ops

import (
	"errors"
	"slices"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/refs"
)

func TestStartReleaseAndHotfixRefuseAnExistingVersionTag(t *testing.T) {
	r, _ := newFlowRepo(t)
	cfg := mainFlowConfig()
	cfg.VersionTagPrefix = "v"
	useFlowConfig(t, r, cfg)
	if _, err := CreateTag(t.Context(), r.repo, "v1.0", "main", CreateTagOptions{}); err != nil {
		t.Fatalf("CreateTag returned error %v", err)
	}

	for _, kind := range []string{FlowKindRelease, FlowKindHotfix} {
		if _, err := StartFlow(t.Context(), r.repo, kind, "1.0", StartFlowOptions{}); !errors.Is(err, ErrFlowTagExists) {
			t.Fatalf("StartFlow %s over the tag v1.0 returned %v", kind, err)
		}
		if !refMissing(t, r, refs.BranchName(kind+"/1.0")) {
			t.Fatalf("the %s branch was created over an existing tag", kind)
		}
	}
	startFlow(t, r, FlowKindFeature, "1.0", StartFlowOptions{})
}

func TestFinishReleaseKeepsATagOnAnotherCommitAndDoesNotPushIt(t *testing.T) {
	r, _ := newFlowRepo(t)
	server := flowServer(t, r)
	startFlowRelease(t, r, "1.0", originFlow)
	release := commitFlowFile(t, r, "VERSION", "1.0\n", "bump")
	earlier, err := CreateTag(t.Context(), r.repo, "1.0", "main", CreateTagOptions{Message: "earlier"})
	if err != nil {
		t.Fatalf("CreateTag returned error %v", err)
	}
	developBefore := r.branchTarget("develop")

	result, err := FinishFlow(t.Context(), r.repo, FlowKindRelease, "1.0", FinishFlowOptions{Push: true, DeleteBranch: true, Network: originFlow})
	if err != nil || !result.Finished() || result.KeptTag != "1.0" || result.Tag != (TagResult{}) {
		t.Fatalf("FinishFlow = %+v, %v", result, err)
	}

	if tag, err := r.refs().Lookup(refs.TagName("1.0")); err != nil || tag.Target != earlier.Tag {
		t.Fatalf("tag 1.0 = %+v, %v; want it left at %s", tag, err, earlier.Tag)
	}
	if got := flowParents(t, r, r.branchTarget("develop")); !slices.Equal(got, []hash.ObjectID{developBefore, release}) {
		t.Fatalf("develop merge parents = %v, want %v and the release %v", got, developBefore, release)
	}
	if !refMissing(t, server, refs.TagName("1.0")) {
		t.Fatal("the kept tag was pushed")
	}
	if got, err := server.refs().Lookup(refs.BranchName("main")); err != nil || got.Target != r.branchTarget("main") {
		t.Fatalf("server main = %+v, %v", got, err)
	}
}

func TestFinishReleaseLeavesATagThatAlreadyNamesMain(t *testing.T) {
	r, _ := newFlowRepo(t)
	startFlowRelease(t, r, "1.0", FlowNetwork{})
	existing, err := CreateTag(t.Context(), r.repo, "1.0", "main", CreateTagOptions{Message: "ready"})
	if err != nil {
		t.Fatalf("CreateTag returned error %v", err)
	}

	result, err := FinishFlow(t.Context(), r.repo, FlowKindRelease, "1.0", FinishFlowOptions{})
	if err != nil || !result.Finished() || result.KeptTag != "" || result.Tag != (TagResult{}) {
		t.Fatalf("FinishFlow = %+v, %v", result, err)
	}
	if tag, err := r.refs().Lookup(refs.TagName("1.0")); err != nil || tag.Target != existing.Tag {
		t.Fatalf("tag 1.0 = %+v, %v; want %s", tag, err, existing.Tag)
	}
}

func TestFinishReleaseReportsATagThatNamesNoCommit(t *testing.T) {
	r := newReleaseRepo(t)
	commit, err := r.db().Commit(r.branchTarget("main"))
	if err != nil {
		t.Fatalf("Commit returned error %v", err)
	}
	tx := r.refs().Begin()
	if err := tx.Set(refs.TagName("1.0"), commit.Tree); err != nil {
		t.Fatalf("Set returned error %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit returned error %v", err)
	}

	if _, err := FinishFlow(t.Context(), r.repo, FlowKindRelease, "1.0", FinishFlowOptions{}); !errors.Is(err, ErrTargetNotFound) {
		t.Fatalf("FinishFlow over a tag of a tree returned %v", err)
	}
}
