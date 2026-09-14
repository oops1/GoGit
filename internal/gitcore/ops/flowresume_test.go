//go:build !race

package ops

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/refs"
)

func stoppedRelease(t *testing.T) *testRepo {
	t.Helper()
	r, _ := newFlowRepo(t)
	startFlowRelease(t, r, "1.0", FlowNetwork{})
	commitFlowFile(t, r, "a.txt", "release\n", "release change")
	switchFlowBranch(t, r, "main")
	commitFlowFile(t, r, "a.txt", "hotfix\n", "main change")
	switchFlowBranch(t, r, "release/1.0")
	stopped, err := FinishFlow(t.Context(), r.repo, FlowKindRelease, "1.0", FinishFlowOptions{DeleteBranch: true})
	if err != nil || stopped.Stopped != FlowStepMergeMaster {
		t.Fatalf("FinishFlow = %+v, %v; want a stop while merging into main", stopped, err)
	}
	return r
}

func commitResolution(t *testing.T, r *testRepo) {
	t.Helper()
	r.writeFile("a.txt", "resolved\n")
	mustStage(t, r, "a.txt")
	if _, err := Commit(t.Context(), r.repo, CommitOptions{Message: "Finish 1.0"}); err != nil {
		t.Fatalf("Commit returned error %v", err)
	}
}

func TestFinishRunsAnAbortedMergeAgainInsteadOfSkippingIt(t *testing.T) {
	r := stoppedRelease(t)
	if err := AbortOperation(t.Context(), r.repo); err != nil {
		t.Fatalf("AbortOperation returned error %v", err)
	}

	again, err := FinishFlow(t.Context(), r.repo, FlowKindRelease, "1.0", FinishFlowOptions{})

	if err != nil || again.Stopped != FlowStepMergeMaster || !slices.Equal(again.Conflicts, []string{"a.txt"}) {
		t.Fatalf("FinishFlow after an abort = %+v, %v; want the merge to run again", again, err)
	}
	if !refMissing(t, r, refs.TagName("1.0")) {
		t.Fatal("the tag was created although main does not hold the release")
	}
}

func TestFinishRefusesToResumeOverAnUnresolvedIndex(t *testing.T) {
	r := stoppedRelease(t)
	if err := os.Remove(filepath.Join(r.repo.GitDir(), mergeHeadFile)); err != nil {
		t.Fatal(err)
	}

	if _, err := FinishFlow(t.Context(), r.repo, FlowKindRelease, "1.0", FinishFlowOptions{}); !errors.Is(err, ErrUnmergedPaths) {
		t.Fatalf("FinishFlow over conflicts returned %v", err)
	}
}

func TestFinishReportsResumeStateItCannotRead(t *testing.T) {
	squashDir := stoppedRelease(t)
	commitResolution(t, squashDir)
	if err := os.Mkdir(filepath.Join(squashDir.repo.GitDir(), squashMsgFile), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := FinishFlow(t.Context(), squashDir.repo, FlowKindRelease, "1.0", FinishFlowOptions{}); err == nil {
		t.Fatal("FinishFlow ignored an unreadable squash message")
	}

	corrupt := stoppedRelease(t)
	commitResolution(t, corrupt)
	corrupt.corruptIndexFile()
	if _, err := FinishFlow(t.Context(), corrupt.repo, FlowKindRelease, "1.0", FinishFlowOptions{}); err == nil {
		t.Fatal("FinishFlow ignored an unreadable index")
	}
}

func TestFinishReportsBranchesItCannotCompareOnResume(t *testing.T) {
	for _, branch := range []string{"release/1.0", "main"} {
		t.Run(branch, func(t *testing.T) {
			r := stoppedRelease(t)
			commitResolution(t, r)
			store := r.refs()
			tx := store.Begin()
			if err := tx.Delete(refs.BranchName(branch), hash.Zero); err != nil {
				t.Fatal(err)
			}
			if err := tx.Commit(); err != nil {
				t.Fatal(err)
			}
			if _, err := FinishFlow(t.Context(), r.repo, FlowKindRelease, "1.0", FinishFlowOptions{}); !errors.Is(err, ErrTargetNotFound) {
				t.Fatalf("FinishFlow without %s returned %v", branch, err)
			}
		})
	}

	r := stoppedRelease(t)
	commitResolution(t, r)
	swapRefsOpen(t, func(refs.Options) (*refs.Store, error) { return nil, errInjected })
	if _, err := FinishFlow(t.Context(), r.repo, FlowKindRelease, "1.0", FinishFlowOptions{}); !errors.Is(err, errInjected) {
		t.Fatalf("FinishFlow with unreadable refs returned %v", err)
	}
}

func TestFinishWaitsForTheSquashCommitBeforeResuming(t *testing.T) {
	r, _ := newFlowRepo(t)
	startFlow(t, r, FlowKindFeature, "login", StartFlowOptions{})
	commitFlowFile(t, r, "a.txt", "feature\n", "feature change")
	switchFlowBranch(t, r, "develop")
	commitFlowFile(t, r, "a.txt", "develop\n", "develop change")

	stopped, err := FinishFlow(t.Context(), r.repo, FlowKindFeature, "login", FinishFlowOptions{Integration: FlowSquash, Message: "Add login", DeleteBranch: true})
	if err != nil || stopped.Stopped != FlowStepMergeDevelop {
		t.Fatalf("FinishFlow = %+v, %v; want a stop while squashing into develop", stopped, err)
	}
	r.writeFile("a.txt", "resolved\n")
	mustStage(t, r, "a.txt")

	if _, err := FinishFlow(t.Context(), r.repo, FlowKindFeature, "login", FinishFlowOptions{}); !errors.Is(err, ErrMergeInProgress) {
		t.Fatalf("FinishFlow before the squash commit returned %v", err)
	}
	if refMissing(t, r, refs.BranchName("feature/login")) {
		t.Fatal("the feature branch was deleted before its squash was committed")
	}
}

func TestFinishResumesAReleaseAfterItsDevelopMergeWasCommitted(t *testing.T) {
	r, _ := newFlowRepo(t)
	startFlowRelease(t, r, "1.0", FlowNetwork{})
	commitFlowFile(t, r, "a.txt", "release\n", "release change")
	switchFlowBranch(t, r, "develop")
	commitFlowFile(t, r, "a.txt", "develop\n", "develop change")
	switchFlowBranch(t, r, "release/1.0")

	stopped, err := FinishFlow(t.Context(), r.repo, FlowKindRelease, "1.0", FinishFlowOptions{DeleteBranch: true})
	if err != nil || stopped.Stopped != FlowStepMergeDevelop {
		t.Fatalf("FinishFlow = %+v, %v; want a stop while merging into develop", stopped, err)
	}
	commitResolution(t, r)

	resumed, err := FinishFlow(t.Context(), r.repo, FlowKindRelease, "1.0", FinishFlowOptions{})

	if err != nil || !resumed.Finished() || !refMissing(t, r, refs.BranchName("release/1.0")) {
		t.Fatalf("resumed FinishFlow = %+v, %v", resumed, err)
	}
}
