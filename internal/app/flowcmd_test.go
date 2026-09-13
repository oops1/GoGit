package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/ops"
	"github.com/oops1/gogit/internal/gitcore/refs"
	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/branches"
	"github.com/oops1/gogit/internal/ui/flow"
	"github.com/oops1/gogit/internal/ui/operation"
)

func captureFlowViews[V any](t *testing.T, seam *func() (V, error)) *[]V {
	t.Helper()
	views := &[]V{}
	prev := *seam
	*seam = func() (V, error) {
		view, err := prev()
		if err == nil {
			*views = append(*views, view)
		}
		return view, err
	}
	t.Cleanup(func() { *seam = prev })
	return views
}

func captureKindViews[V any](t *testing.T, seam *func(string) (V, error)) *[]V {
	t.Helper()
	views := &[]V{}
	prev := *seam
	*seam = func(kind string) (V, error) {
		view, err := prev(kind)
		if err == nil {
			*views = append(*views, view)
		}
		return view, err
	}
	t.Cleanup(func() { *seam = prev })
	return views
}

func failFlowView[V any](t *testing.T, seam *func() (V, error)) {
	t.Helper()
	prev := *seam
	*seam = func() (V, error) {
		var none V
		return none, errors.New("no dialog")
	}
	t.Cleanup(func() { *seam = prev })
}

func failKindView[V any](t *testing.T, seam *func(string) (V, error)) {
	t.Helper()
	prev := *seam
	*seam = func(string) (V, error) {
		var none V
		return none, errors.New("no dialog")
	}
	t.Cleanup(func() { *seam = prev })
}

func waitForFlowView[V any](t *testing.T, a *App, views *[]V) V {
	t.Helper()
	deadline := time.Now().Add(testTimeout)
	for {
		if n := readOnDispatcher(t, a, func() int { return len(*views) }); n > 0 {
			return readOnDispatcher(t, a, func() V { return (*views)[n-1] })
		}
		if time.Now().After(deadline) {
			t.Fatal("the dialog did not open")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func flowReadyApp(t *testing.T, configure bool) (*App, string) {
	t.Helper()
	a, target := forkedApp(t, false)
	head := readOnDispatcher(t, a, func() hash.ObjectID {
		snap, err := loadBranchSnapshot(a.opened().store)
		if err != nil {
			t.Error(err)
		}
		return snap.HeadID
	})
	r, err := gitrepo.Open(target, gitrepo.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	if err := ops.CreateBranch(t.Context(), r, "develop", head, ops.CreateBranchOptions{}); err != nil {
		t.Fatal(err)
	}
	if configure {
		cfg := ops.DefaultFlowConfig()
		cfg.Master = "main"
		if err := ops.WriteFlowConfig(r, cfg); err != nil {
			t.Fatal(err)
		}
	}
	readOnDispatcher(t, a, func() bool { a.RefreshRepository(); return true })
	return a, target
}

func operationLines(t *testing.T, a *App, views *[]*operation.View) []string {
	t.Helper()
	view := lastOperationView(t, views)
	waitForFinishedOperation(t, a, view)
	waitForPostQueueDrain(t, a)
	return readOnDispatcher(t, a, view.Lines)
}

func TestGitFlowCommandsFollowTheRepositoryAndTheBranch(t *testing.T) {
	release := FlowBranch{Kind: ops.FlowKindRelease, Name: "1.0"}
	feature := FlowBranch{Kind: ops.FlowKindFeature, Name: "login"}
	hotfix := FlowBranch{Kind: ops.FlowKindHotfix, Name: "1.0.1"}
	starts := []CommandID{CmdFlowStartFeature, CmdFlowStartRelease, CmdFlowStartHotfix}
	finishes := []CommandID{CmdFlowFinishFeature, CmdFlowFinishRelease, CmdFlowFinishHotfix}
	for _, c := range []struct {
		state            State
		start, configure bool
		finish           []bool
	}{
		{State{}, false, false, []bool{false, false, false}},
		{State{ActiveRepository: "r"}, true, true, []bool{false, false, false}},
		{State{ActiveRepository: "r", Merging: true, FlowCurrent: release}, false, true, []bool{false, false, false}},
		{State{ActiveRepository: "r", FlowCurrent: release}, true, true, []bool{false, true, false}},
		{State{ActiveRepository: "r", FlowCurrent: feature}, true, true, []bool{true, false, false}},
		{State{ActiveRepository: "r", FlowCurrent: feature, FlowPending: hotfix}, true, true, []bool{false, false, true}},
	} {
		for i := range starts {
			if c.state.Enabled(starts[i]) != c.start || c.state.Enabled(finishes[i]) != c.finish[i] {
				t.Errorf("state %+v: %s or %s", c.state, starts[i], finishes[i])
			}
		}
		if c.state.Enabled(CmdFlowConfigure) != c.configure {
			t.Errorf("state %+v: configure", c.state)
		}
	}

	a, _ := flowReadyApp(t, true)
	disabled := readOnDispatcher(t, a, func() []bool {
		items := a.menu.Items()[branchMenuIndex].Items
		var off []bool
		for _, sub := range items[len(items)-1].SubItems {
			off = append(off, sub.Disabled)
		}
		return off
	})

	if !slices.Equal(disabled, []bool{false, true, false, true, false, true, false}) {
		t.Fatalf("git-flow items disabled = %v, want only the finishes off", disabled)
	}
}

func TestStartingAReleaseAsksForTheSettingsFirstAndCreatesTheBranch(t *testing.T) {
	a, _ := flowReadyApp(t, false)
	configs := captureFlowViews(t, &newFlowConfigView)
	starts := captureKindViews(t, &newFlowStartView)
	operations := captureOperationViews(t)

	if !readOnDispatcher(t, a, func() bool { return a.Dispatch(CmdFlowStartRelease) }) {
		t.Fatal("Start Release did not dispatch")
	}
	config := waitForFlowView(t, a, configs)
	model := readOnDispatcher(t, a, config.Model)
	if model.Master != "main" || model.Develop != "develop" {
		t.Fatalf("suggested settings = %+v", model)
	}
	readOnDispatcher(t, a, func() bool { config.OnOK(model); return true })
	start := waitForFlowView(t, a, starts)
	readOnDispatcher(t, a, func() bool { start.OnOK(flow.StartModel{Name: "1.0"}); return true })
	lines := operationLines(t, a, operations)
	waitForWorkingIdle(t, a)

	if !slices.Contains(lines, i18n.Tf("Operation.Log.FlowStarted", "release/1.0")) {
		t.Fatalf("operation log = %v", lines)
	}
	if got := readOnDispatcher(t, a, a.currentBranchName); got != "release/1.0" {
		t.Fatalf("current branch = %q", got)
	}
	if got := readOnDispatcher(t, a, a.State).FlowCurrent; got != (FlowBranch{Kind: ops.FlowKindRelease, Name: "1.0"}) {
		t.Fatalf("flow branch = %+v", got)
	}
}

func TestFinishingTheReleaseTagsItAndReturnsToDevelop(t *testing.T) {
	a, _ := flowReadyApp(t, true)
	operations := captureOperationViews(t)
	readOnDispatcher(t, a, func() bool { a.startFlow(ops.FlowKindRelease, "1.0"); return true })
	operationLines(t, a, operations)
	waitForWorkingIdle(t, a)
	readOnDispatcher(t, a, func() bool { a.startFlow(ops.FlowKindRelease, "1.0"); return true })
	if lines := operationLines(t, a, operations); slices.Contains(lines, i18n.Tf("Operation.Log.FlowStarted", "release/1.0")) {
		t.Fatalf("a second start of the same release succeeded: %v", lines)
	}
	finishes := captureKindViews(t, &newFlowFinishView)

	if !readOnDispatcher(t, a, func() bool { return a.Dispatch(CmdFlowFinishRelease) }) {
		t.Fatal("Finish Release did not dispatch")
	}
	view := waitForFlowView(t, a, finishes)
	readOnDispatcher(t, a, func() bool { view.OnOK(view.Model()); return true })
	lines := operationLines(t, a, operations)
	waitForWorkingIdle(t, a)

	if !slices.Contains(lines, i18n.Tf("Operation.Log.ReleaseFinished", "1.0")) {
		t.Fatalf("operation log = %v", lines)
	}
	if got := readOnDispatcher(t, a, a.currentBranchName); got != "develop" {
		t.Fatalf("current branch = %q", got)
	}
	if !slices.Contains(readOnDispatcher(t, a, a.tagNames), "1.0") {
		t.Fatal("the release tag is missing")
	}
	if got := readOnDispatcher(t, a, a.State); got.FlowCurrent != (FlowBranch{}) || got.FlowPending != (FlowBranch{}) {
		t.Fatalf("flow state = %+v", got)
	}
}

func runFlowDialog[V any](t *testing.T, a *App, operations *[]*operation.View, views *[]V, id CommandID, confirm func(V)) []string {
	t.Helper()
	if !readOnDispatcher(t, a, func() bool { return a.Dispatch(id) }) {
		t.Fatalf("%s did not dispatch", id)
	}
	view := waitForFlowView(t, a, views)
	readOnDispatcher(t, a, func() bool { confirm(view); return true })
	lines := operationLines(t, a, operations)
	waitForWorkingIdle(t, a)
	return lines
}

func TestFeaturesAndHotfixesStartAndFinishFromTheMenu(t *testing.T) {
	a, target := flowReadyApp(t, true)
	r, err := gitrepo.Open(target, gitrepo.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	cfg := ops.DefaultFlowConfig()
	cfg.Master, cfg.FeaturePrefix = "main", "topic/"
	err = ops.WriteFlowConfig(r, cfg)
	_ = r.Close()
	if err != nil {
		t.Fatal(err)
	}
	operations := captureOperationViews(t)
	starts := captureKindViews(t, &newFlowStartView)
	finishes := captureKindViews(t, &newFlowFinishView)
	finishWithDefaults := func(v *flow.FinishView) { v.OnOK(v.Model()) }

	lines := runFlowDialog(t, a, operations, starts, CmdFlowStartFeature, func(v *flow.StartView) { v.OnOK(flow.StartModel{Name: "login"}) })
	if got := readOnDispatcher(t, a, a.State).FlowCurrent; got != (FlowBranch{Kind: ops.FlowKindFeature, Name: "login"}) {
		t.Fatalf("flow branch = %+v, log = %v", got, lines)
	}
	lines = runFlowDialog(t, a, operations, finishes, CmdFlowFinishFeature, finishWithDefaults)
	if !slices.Contains(lines, i18n.Tf("Operation.Log.FeatureFinished", "login")) || readOnDispatcher(t, a, a.currentBranchName) != "develop" {
		t.Fatalf("feature finish log = %v", lines)
	}

	runFlowDialog(t, a, operations, starts, CmdFlowStartHotfix, func(v *flow.StartView) { v.OnOK(flow.StartModel{Name: "1.0.1"}) })
	if got := readOnDispatcher(t, a, a.currentBranchName); got != "hotfix/1.0.1" {
		t.Fatalf("current branch = %q", got)
	}
	lines = runFlowDialog(t, a, operations, finishes, CmdFlowFinishHotfix, finishWithDefaults)

	if !slices.Contains(lines, i18n.Tf("Operation.Log.HotfixFinished", "1.0.1")) || !slices.Contains(readOnDispatcher(t, a, a.tagNames), "1.0.1") {
		t.Fatalf("hotfix finish log = %v", lines)
	}
}

func TestFinishFlowLogsEveryOutcome(t *testing.T) {
	a := newTestApp(t)
	operations := captureOperationViews(t)
	for _, c := range []struct {
		kind   string
		result ops.FinishFlowResult
		err    error
		want   []string
	}{
		{ops.FlowKindRelease, ops.FinishFlowResult{}, nil, []string{i18n.Tf("Operation.Log.ReleaseFinished", "1.0")}},
		{ops.FlowKindFeature, ops.FinishFlowResult{Stopped: ops.FlowStepMergeDevelop, Conflicts: []string{"a.txt"}}, nil, []string{i18n.Tf("Operation.Log.MergeConflictPath", "a.txt"), i18n.Tf("Operation.Log.FeatureStopped", 1)}},
		{ops.FlowKindHotfix, ops.FinishFlowResult{}, ops.ErrFlowBehind, []string{i18n.T("Operation.Log.FlowBehind")}},
		{ops.FlowKindRelease, ops.FinishFlowResult{}, errors.New("boom"), nil},
	} {
		readOnDispatcher(t, a, func() bool {
			a.RunOperation("finish", func(_ context.Context, reporter OperationReporter) error {
				reportFinishFlow(reporter, c.kind, "1.0", c.result, c.err)
				return nil
			})
			return true
		})
		lines := operationLines(t, a, operations)
		for _, want := range c.want {
			if !slices.Contains(lines, want) {
				t.Fatalf("%+v, %v: log = %v, want %q", c.result, c.err, lines, want)
			}
		}
	}
}

func TestTheFlowStateFollowsAPendingFinishAndAnUnreadableRepository(t *testing.T) {
	a, target := flowReadyApp(t, true)
	if err := os.WriteFile(filepath.Join(target, ".git", "GOGIT_FLOW"), []byte("hotfix\n2.0\n1\nfalse\nfalse\n"), 0o666); err != nil {
		t.Fatal(err)
	}
	readOnDispatcher(t, a, func() bool { a.RefreshRepository(); return true })
	if got := readOnDispatcher(t, a, a.State).FlowPending; got != (FlowBranch{Kind: ops.FlowKindHotfix, Name: "2.0"}) {
		t.Fatalf("pending finish = %+v", got)
	}

	prev := openGitRepository
	openGitRepository = func(string, gitrepo.OpenOptions) (*gitrepo.Repository, error) { return nil, errors.New("gone") }
	t.Cleanup(func() { openGitRepository = prev })
	operations := captureOperationViews(t)
	readOnDispatcher(t, a, func() bool { a.refreshFlowState(a.opened(), "release/1.0"); return true })
	if got := readOnDispatcher(t, a, a.State); got.FlowPending != (FlowBranch{}) || got.FlowCurrent != (FlowBranch{}) {
		t.Fatalf("flow state over an unreadable repository = %+v", got)
	}
	readOnDispatcher(t, a, func() bool {
		a.openStartFlow(ops.FlowKindRelease)
		a.openFinishFlow(ops.FlowKindRelease)
		a.openFlowConfig(nil)
		return true
	})
	waitForStatusText(t, a, i18n.Tf("Status.FlowReadFailed", errors.New("gone")))
	readOnDispatcher(t, a, func() bool { a.startFlow(ops.FlowKindRelease, "1.0"); return true })
	operationLines(t, a, operations)
	readOnDispatcher(t, a, func() bool { a.finishFlow(ops.FlowKindRelease, "1.0", flow.FinishModel{}); return true })
	operationLines(t, a, operations)
}

func TestGitFlowCommandsNeedAnOpenRepository(t *testing.T) {
	a := newTestApp(t)
	starts := captureKindViews(t, &newFlowStartView)
	operations := captureOperationViews(t)

	readOnDispatcher(t, a, func() bool {
		a.openStartFlow(ops.FlowKindRelease)
		a.openFinishFlow(ops.FlowKindRelease)
		a.openFlowConfig(nil)
		a.startFlow(ops.FlowKindRelease, "1.0")
		a.finishFlow(ops.FlowKindRelease, "1.0", flow.FinishModel{})
		return true
	})

	if len(*starts) != 0 || len(*operations) != 0 {
		t.Fatalf("views = %d, operations = %d without a repository", len(*starts), len(*operations))
	}
}

func TestGitFlowDialogsThatCannotOpenAreLeftAlone(t *testing.T) {
	a, _ := flowReadyApp(t, true)
	failKindView(t, &newFlowStartView)
	failKindView(t, &newFlowFinishView)
	failFlowView(t, &newFlowConfigView)
	operations := captureOperationViews(t)

	readOnDispatcher(t, a, func() bool {
		a.setFlowState(FlowBranch{}, FlowBranch{})
		a.openFinishFlow(ops.FlowKindRelease)
		a.setFlowState(FlowBranch{Kind: ops.FlowKindRelease, Name: "1.0"}, FlowBranch{})
		a.openStartFlow(ops.FlowKindRelease)
		a.openFinishFlow(ops.FlowKindRelease)
		a.openFlowConfig(nil)
		return true
	})

	if len(*operations) != 0 {
		t.Fatalf("operations = %d", len(*operations))
	}
}

func TestGitFlowDialogsCanBeCancelled(t *testing.T) {
	a, _ := flowReadyApp(t, true)
	starts := captureKindViews(t, &newFlowStartView)
	finishes := captureKindViews(t, &newFlowFinishView)
	configs := captureFlowViews(t, &newFlowConfigView)
	operations := captureOperationViews(t)

	readOnDispatcher(t, a, func() bool {
		a.setFlowState(FlowBranch{}, FlowBranch{Kind: ops.FlowKindRelease, Name: "1.0"})
		a.openStartFlow(ops.FlowKindRelease)
		a.openFinishFlow(ops.FlowKindRelease)
		a.Dispatch(CmdFlowConfigure)
		(*starts)[0].OnCancel()
		(*finishes)[0].OnCancel()
		(*configs)[0].OnCancel()
		return true
	})

	if len(*operations) != 0 {
		t.Fatalf("a cancelled dialog started %d operations", len(*operations))
	}
}

func TestFinishingWithoutSettingsAsksForThemFirstAndReturns(t *testing.T) {
	a, _ := flowReadyApp(t, false)
	configs := captureFlowViews(t, &newFlowConfigView)
	finishes := captureKindViews(t, &newFlowFinishView)

	readOnDispatcher(t, a, func() bool { a.openFinishFlow(ops.FlowKindFeature); return true })
	config := waitForFlowView(t, a, configs)
	readOnDispatcher(t, a, func() bool { config.OnOK(config.Model()); return true })
	waitForStatusText(t, a, i18n.T("Status.FlowConfigSaved"))
	waitForPostQueueDrain(t, a)

	if n := readOnDispatcher(t, a, func() int { return len(*finishes) }); n != 0 {
		t.Fatalf("a finish dialog opened without a feature branch: %d", n)
	}
}

func TestAFailedSaveOfTheSettingsIsReported(t *testing.T) {
	a, _ := flowReadyApp(t, false)
	configs := captureFlowViews(t, &newFlowConfigView)
	prev := runWriteFlowConfig
	runWriteFlowConfig = func(*gitrepo.Repository, ops.FlowConfig) error { return errors.New("locked") }
	t.Cleanup(func() { runWriteFlowConfig = prev })

	readOnDispatcher(t, a, func() bool { a.openFlowConfig(nil); return true })
	config := waitForFlowView(t, a, configs)
	readOnDispatcher(t, a, func() bool { config.OnOK(config.Model()); return true })

	waitForStatusText(t, a, i18n.Tf("Status.FlowConfigFailed", errors.New("locked")))
}

func TestTheFlowUsesTheDefaultRemoteOnlyWhenItExists(t *testing.T) {
	a, target := flowReadyApp(t, true)
	r, err := gitrepo.Open(target, gitrepo.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	if net := a.flowNetwork(r, OperationReporter{app: a}); net.Remote != "" {
		t.Fatalf("network without a remote = %+v", net)
	}
	if err := ops.AddRemote(r, a.effectiveDefaultRemote(r), "https://example.com/flow.git"); err != nil {
		t.Fatal(err)
	}
	withRemote, err := gitrepo.Open(target, gitrepo.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = withRemote.Close() })

	net := a.flowNetwork(withRemote, OperationReporter{app: a})

	if net.Remote != a.effectiveDefaultRemote(withRemote) || net.Progress == nil {
		t.Fatalf("network = %+v", net)
	}
}

func TestTheSettingsSuggestMainWhenThereIsNoMaster(t *testing.T) {
	defaults := ops.DefaultFlowConfig()

	if got := suggestedFlowConfig(defaults, []string{"main", "develop"}); got.Master != "main" {
		t.Fatalf("master = %q, want main", got.Master)
	}
	if got := suggestedFlowConfig(defaults, []string{"master", "main"}); got.Master != "master" {
		t.Fatalf("master = %q, want master", got.Master)
	}
	if got := suggestedFlowConfig(defaults, []string{"trunk"}); got.Master != "master" {
		t.Fatalf("master = %q, want the default", got.Master)
	}
}

func TestBranchNamesAreEmptyWhenTheBranchesCannotBeRead(t *testing.T) {
	a, _ := flowReadyApp(t, true)
	prev := loadBranchSnapshot
	loadBranchSnapshot = func(*refs.Store) (branches.Snapshot, error) { return branches.Snapshot{}, errors.New("no refs") }
	t.Cleanup(func() { loadBranchSnapshot = prev })

	if names := readOnDispatcher(t, a, func() []string { return a.localBranchNames(a.opened()) }); names != nil {
		t.Fatalf("branch names = %v", names)
	}
}
