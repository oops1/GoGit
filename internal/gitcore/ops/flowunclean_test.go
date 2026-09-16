package ops

import (
	"slices"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/merge"
	"github.com/oops1/gogit/internal/gitcore/refs"
)

func newSplitFeatureRepo(t *testing.T) *testRepo {
	t.Helper()
	r := newTestRepo(t)
	base := r.commitFiles("base", map[string]string{"lib/a": tenLines("a"), "lib/b": tenLines("b")})
	r.createBranch("develop", base)
	useFlowConfig(t, r, mainFlowConfig())
	startFlow(t, r, FlowKindFeature, "login", StartFlowOptions{})
	r.commitFiles("login", map[string]string{"lib/new": "new\n"})
	switchFlowBranch(t, r, "develop")
	r.commitFiles("split", map[string]string{"lib/a": "", "lib/b": "", "x/a": tenLines("a"), "y/b": tenLines("b")})
	return r
}

func uncleanOnly(t *testing.T, conflicts []string, warnings []merge.Warning) {
	t.Helper()
	if len(conflicts) != 0 || !slices.ContainsFunc(warnings, merge.Warning.Unclean) {
		t.Fatalf("conflicts = %v, warnings = %+v, want only an unclean directory rename", conflicts, warnings)
	}
}

func TestFinishFlowStopsOnAnUncleanDirectoryRename(t *testing.T) {
	for integration, step := range map[FlowIntegration]FlowStep{
		FlowMergeCommit: FlowStepMergeDevelop,
		FlowSquash:      FlowStepMergeDevelop,
		FlowRebase:      FlowStepRebase,
	} {
		r := newSplitFeatureRepo(t)

		result, err := FinishFlow(t.Context(), r.repo, FlowKindFeature, "login", FinishFlowOptions{Integration: integration, DeleteBranch: true})

		if err != nil || result.Finished() || result.Stopped != step {
			t.Fatalf("integration %d: FinishFlow = %+v, %v; want a stop at step %d", integration, result, err, step)
		}
		uncleanOnly(t, result.Conflicts, result.Warnings)
		if refMissing(t, r, refs.BranchName("feature/login")) {
			t.Fatalf("integration %d: the feature branch was deleted", integration)
		}
		if pending, found, err := PendingFlowFinish(r.repo); err != nil || !found || pending.Step != step {
			t.Fatalf("integration %d: PendingFlowFinish = %+v, %v, %v", integration, pending, found, err)
		}
	}
}

func TestIntegrateDevelopStopsOnAnUncleanDirectoryRename(t *testing.T) {
	for _, rebase := range []bool{false, true} {
		r := newSplitFeatureRepo(t)

		integrated, err := IntegrateDevelop(t.Context(), r.repo, "login", IntegrateDevelopOptions{Rebase: rebase})

		if err != nil || integrated.Clean() {
			t.Fatalf("rebase %v: IntegrateDevelop = %+v, %v", rebase, integrated, err)
		}
		uncleanOnly(t, integrated.Conflicts, integrated.Warnings)
		if state := r.mergeState(); !state.InProgress() {
			t.Fatalf("rebase %v: no operation left in progress", rebase)
		}
	}
}

func TestFlowMergeResultsAreCleanWithoutConflictsOrUncleanWarnings(t *testing.T) {
	for result, want := range map[*FlowMergeResult]bool{
		{}: true,
		{Warnings: []merge.Warning{{Kind: merge.WarningRenameLimit, Needed: 9}}}: true,
		{Conflicts: []string{"a"}}: false,
		{Warnings: []merge.Warning{{Kind: merge.WarningDirectoryRenameSplit}}}: false,
	} {
		if result.Clean() != want {
			t.Errorf("%+v: Clean() = %v, want %v", *result, result.Clean(), want)
		}
	}
}
