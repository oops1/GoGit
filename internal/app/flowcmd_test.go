package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/oops1/headless-gui/v3/widget"

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

func waitForFlowView[V any](t *testing.T, a *App, views *[]V, count int) V {
	t.Helper()
	deadline := time.Now().Add(testTimeout)
	for {
		if n := readOnDispatcher(t, a, func() int { return len(*views) }); n >= count {
			return readOnDispatcher(t, a, func() V { return (*views)[count-1] })
		}
		if time.Now().After(deadline) {
			t.Fatal("the dialog did not open")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func openRepoAt(t *testing.T, target string) *gitrepo.Repository {
	t.Helper()
	r, err := gitrepo.Open(target, gitrepo.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	return r
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
	r := openRepoAt(t, target)
	if err := ops.CreateBranch(t.Context(), r, "develop", head, ops.CreateBranchOptions{}); err != nil {
		t.Fatal(err)
	}
	if configure {
		cfg := ops.DefaultFlowConfig()
		cfg.Master, cfg.FeaturePrefix = "main", "topic/"
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

func runOnDispatcher(t *testing.T, a *App, fn func()) {
	t.Helper()
	readOnDispatcher(t, a, func() bool { fn(); return true })
}

func flowOn(current, pending FlowBranch) flowStatus {
	return flowStatus{configured: true, current: current, pending: pending}
}

var (
	releaseBranch = FlowBranch{Kind: ops.FlowKindRelease, Name: "1.0"}
	featureBranch = FlowBranch{Kind: ops.FlowKindFeature, Name: "login"}
	hotfixBranch  = FlowBranch{Kind: ops.FlowKindHotfix, Name: "1.0.1"}
)

func TestGitFlowCommandsFollowTheSettingsAndTheBranch(t *testing.T) {
	starts := []CommandID{CmdFlowStartFeature, CmdFlowStartHotfix, CmdFlowStartRelease, CmdFlowStartSupport}
	finishes := []CommandID{CmdFlowFinishFeature, CmdFlowFinishHotfix, CmdFlowFinishRelease}
	ready := State{ActiveRepository: "r", FlowConfigured: true}
	with := func(change func(*State)) State {
		s := ready
		change(&s)
		return s
	}
	for i, c := range []struct {
		state     State
		start     []bool
		finish    []bool
		integrate bool
		configure bool
	}{
		{State{}, []bool{false, false, false, false}, []bool{false, false, false}, false, false},
		{State{ActiveRepository: "r"}, []bool{false, false, false, false}, []bool{false, false, false}, false, true},
		{ready, []bool{true, true, true, true}, []bool{false, false, false}, false, true},
		{with(func(s *State) { s.FlowLight = true }), []bool{true, false, false, false}, []bool{false, false, false}, false, true},
		{with(func(s *State) { s.Merging, s.FlowCurrent = true, releaseBranch }), []bool{false, false, false, false}, []bool{false, false, false}, false, true},
		{with(func(s *State) { s.FlowCurrent = releaseBranch }), []bool{true, true, true, true}, []bool{false, false, true}, false, true},
		{with(func(s *State) { s.FlowCurrent = featureBranch }), []bool{true, true, true, true}, []bool{true, false, false}, true, true},
		{with(func(s *State) { s.FlowCurrent, s.FlowPending = featureBranch, hotfixBranch }), []bool{true, true, true, true}, []bool{false, true, false}, false, true},
	} {
		for j, id := range starts {
			if c.state.Enabled(id) != c.start[j] {
				t.Errorf("case %d: %s enabled = %v", i, id, !c.start[j])
			}
		}
		for j, id := range finishes {
			if c.state.Enabled(id) != c.finish[j] {
				t.Errorf("case %d: %s enabled = %v", i, id, !c.finish[j])
			}
		}
		if c.state.Enabled(CmdFlowIntegrateDevelop) != c.integrate || c.state.Enabled(CmdFlowConfigure) != c.configure {
			t.Errorf("case %d: integrate or configure", i)
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

	if want := []bool{false, true, true, false, false, true, false, false, true, false, false, false, false}; !slices.Equal(disabled, want) {
		t.Fatalf("git-flow items disabled = %v, want %v", disabled, want)
	}
}

func TestTheToolbarGitFlowMenuFollowsTheMode(t *testing.T) {
	bare := newTestApp(t)
	buttonOf := func(app *App) *widget.MenuButton {
		return readOnDispatcher(t, app, func() *widget.MenuButton { btn, _ := app.flowButton(); return btn })
	}
	if btn := buttonOf(bare); btn == nil || readOnDispatcher(t, bare, btn.IsEnabled) {
		t.Fatal("the git-flow button is missing or enabled without a repository")
	}

	a, _ := flowReadyApp(t, true)
	starts := captureKindViews(t, &newFlowStartView)
	btn := buttonOf(a)
	if btn == nil || !readOnDispatcher(t, a, btn.IsEnabled) || readOnDispatcher(t, a, func() bool { return btn.Icon == nil }) {
		t.Fatal("the git-flow button is missing, disabled or has no icon")
	}
	full := readOnDispatcher(t, a, func() []widget.MenuItem { btn.OnOpening(); return btn.Items })
	runOnDispatcher(t, a, func() { a.setFlowState(flowStatus{configured: true, light: true}) })
	light := readOnDispatcher(t, a, func() []widget.MenuItem { btn.OnOpening(); return btn.Items })
	runOnDispatcher(t, a, func() { light[0].OnClick() })

	if len(full) != len(flowMenuLeaves) || !full[3].Separator || full[0].Text != i18n.T("Menu.Branch.GitFlow.StartFeature") {
		t.Fatalf("full menu = %+v", full)
	}
	if len(light) != 5 || light[4].Text != i18n.T("Menu.Branch.GitFlow.Configure") || light[1].Disabled != true {
		t.Fatalf("light menu = %+v", light)
	}
	waitForFlowView(t, a, starts, 1)
}

func TestConfiguringGitFlowCreatesBranchesAndSwitchesOff(t *testing.T) {
	a, target := flowReadyApp(t, false)
	if err := ops.AddRemote(openRepoAt(t, target), "origin", "https://example.com/flow.git"); err != nil {
		t.Fatal(err)
	}
	configs := captureFlowViews(t, &newFlowConfigView)
	questions := captureFlowViews(t, &newFlowConfiguredView)
	operations := captureOperationViews(t)

	runOnDispatcher(t, a, func() { a.Dispatch(CmdFlowConfigure) })
	config := waitForFlowView(t, a, configs, 1)
	model := readOnDispatcher(t, a, config.Model)
	if model.Master != "main" || model.Develop != "develop" || model.Light || model.Remote != "origin" {
		t.Fatalf("suggested settings = %+v", model)
	}
	runOnDispatcher(t, a, func() { config.OnOK(model) })
	if lines := operationLines(t, a, operations); !slices.Contains(lines, i18n.T("Operation.Log.FlowConfigured")) {
		t.Fatalf("configure log = %v", lines)
	}
	if got := readOnDispatcher(t, a, a.State); !got.FlowConfigured || got.FlowLight {
		t.Fatalf("state after configuring = %+v", got)
	}

	runOnDispatcher(t, a, func() { a.Dispatch(CmdFlowConfigure) })
	question := waitForFlowView(t, a, questions, 1)
	runOnDispatcher(t, a, func() { question.OnChange() })
	light := waitForFlowView(t, a, configs, 2)
	runOnDispatcher(t, a, func() { light.OnOK(flow.ConfigModel{Light: true, Develop: "trunk", Feature: "feature/"}) })
	lines := operationLines(t, a, operations)
	if !slices.Contains(lines, i18n.Tf("Operation.Log.FlowBranchCreated", "trunk")) || !readOnDispatcher(t, a, a.State).FlowLight {
		t.Fatalf("light configure log = %v", lines)
	}

	runOnDispatcher(t, a, func() { a.Dispatch(CmdFlowConfigure) })
	question = waitForFlowView(t, a, questions, 2)
	runOnDispatcher(t, a, func() { question.OnSwitchOff() })
	if lines := operationLines(t, a, operations); !slices.Contains(lines, i18n.T("Operation.Log.FlowSwitchedOff")) {
		t.Fatalf("switch off log = %v", lines)
	}
	if readOnDispatcher(t, a, a.State).FlowConfigured {
		t.Fatal("git-flow is still configured")
	}
}

func TestAFailedConfigurationIsReported(t *testing.T) {
	a, _ := flowReadyApp(t, false)
	operations := captureOperationViews(t)
	prevConfigure, prevOff := runConfigureFlow, runSwitchOffFlow
	runConfigureFlow = func(context.Context, *gitrepo.Repository, ops.FlowConfig) ([]refs.Name, error) {
		return []refs.Name{refs.BranchName("trunk")}, errors.New("locked")
	}
	runSwitchOffFlow = func(*gitrepo.Repository) error { return errors.New("locked") }
	t.Cleanup(func() { runConfigureFlow, runSwitchOffFlow = prevConfigure, prevOff })

	runOnDispatcher(t, a, func() { a.configureFlow(ops.DefaultFlowConfig()) })
	lines := operationLines(t, a, operations)
	runOnDispatcher(t, a, func() { a.switchOffFlow() })
	off := operationLines(t, a, operations)

	if !slices.Contains(lines, i18n.Tf("Operation.Log.FlowBranchCreated", "trunk")) || slices.Contains(lines, i18n.T("Operation.Log.FlowConfigured")) {
		t.Fatalf("configure log = %v", lines)
	}
	if slices.Contains(off, i18n.T("Operation.Log.FlowSwitchedOff")) {
		t.Fatalf("switch off log = %v", off)
	}
}

func TestStartingAndFinishingAReleaseFromTheMenu(t *testing.T) {
	a, _ := flowReadyApp(t, true)
	starts := captureKindViews(t, &newFlowStartView)
	finishes := captureKindViews(t, &newFlowFinishView)
	operations := captureOperationViews(t)

	runOnDispatcher(t, a, func() { a.Dispatch(CmdFlowStartRelease) })
	start := waitForFlowView(t, a, starts, 1)
	runOnDispatcher(t, a, func() { start.OnOK(flow.StartModel{Name: "1.0", Base: "develop"}) })
	lines := operationLines(t, a, operations)
	waitForWorkingIdle(t, a)
	if !slices.Contains(lines, i18n.Tf("Operation.Log.FlowStarted", "release/1.0")) || readOnDispatcher(t, a, a.State).FlowCurrent != releaseBranch {
		t.Fatalf("start log = %v", lines)
	}
	runOnDispatcher(t, a, func() { a.startFlow(ops.FlowKindRelease, "1.0", "develop") })
	if lines := operationLines(t, a, operations); slices.Contains(lines, i18n.Tf("Operation.Log.FlowStarted", "release/1.0")) {
		t.Fatalf("a second start of the same release succeeded: %v", lines)
	}

	runOnDispatcher(t, a, func() { a.Dispatch(CmdFlowFinishRelease) })
	finish := waitForFlowView(t, a, finishes, 1)
	runOnDispatcher(t, a, func() { finish.OnOK(finish.Model()) })
	lines = operationLines(t, a, operations)
	waitForWorkingIdle(t, a)

	if !slices.Contains(lines, i18n.Tf("Operation.Log.ReleaseFinished", "1.0")) || readOnDispatcher(t, a, a.currentBranchName) != "develop" {
		t.Fatalf("finish log = %v", lines)
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
	count := readOnDispatcher(t, a, func() int { return len(*views) }) + 1
	runOnDispatcher(t, a, func() { a.Dispatch(id) })
	view := waitForFlowView(t, a, views, count)
	runOnDispatcher(t, a, func() { confirm(view) })
	lines := operationLines(t, a, operations)
	waitForWorkingIdle(t, a)
	return lines
}

func TestFeaturesHotfixesAndSupportBranchesFromTheMenu(t *testing.T) {
	a, _ := flowReadyApp(t, true)
	operations := captureOperationViews(t)
	starts := captureKindViews(t, &newFlowStartView)
	finishes := captureKindViews(t, &newFlowFinishView)
	integrations := captureFlowViews(t, &newFlowIntegrateView)
	finishWithDefaults := func(v *flow.FinishView) { v.OnOK(v.Model()) }

	lines := runFlowDialog(t, a, operations, starts, CmdFlowStartFeature, func(v *flow.StartView) { v.OnOK(flow.StartModel{Name: "login", Base: "develop"}) })
	if got := readOnDispatcher(t, a, a.State).FlowCurrent; got != featureBranch {
		t.Fatalf("flow branch = %+v, log = %v", got, lines)
	}
	lines = runFlowDialog(t, a, operations, integrations, CmdFlowIntegrateDevelop, func(v *flow.IntegrateView) { v.OnOK(false) })
	if !slices.Contains(lines, i18n.Tf("Operation.Log.DevelopIntegrated", "develop", "topic/login")) {
		t.Fatalf("integrate log = %v", lines)
	}
	lines = runFlowDialog(t, a, operations, finishes, CmdFlowFinishFeature, finishWithDefaults)
	if !slices.Contains(lines, i18n.Tf("Operation.Log.FeatureFinished", "login")) || readOnDispatcher(t, a, a.currentBranchName) != "develop" {
		t.Fatalf("feature finish log = %v", lines)
	}

	runFlowDialog(t, a, operations, starts, CmdFlowStartHotfix, func(v *flow.StartView) { v.OnOK(v.Model()) })
	if got := readOnDispatcher(t, a, a.currentBranchName); got != "main" {
		runOnDispatcher(t, a, func() { a.startFlow(ops.FlowKindHotfix, "1.0.1", "main") })
		operationLines(t, a, operations)
		waitForWorkingIdle(t, a)
	}
	lines = runFlowDialog(t, a, operations, finishes, CmdFlowFinishHotfix, finishWithDefaults)
	if !slices.Contains(lines, i18n.Tf("Operation.Log.HotfixFinished", "1.0.1")) || !slices.Contains(readOnDispatcher(t, a, a.tagNames), "1.0.1") {
		t.Fatalf("hotfix finish log = %v", lines)
	}

	runFlowDialog(t, a, operations, starts, CmdFlowStartSupport, func(v *flow.StartView) { v.OnOK(flow.StartModel{Name: "1.x", Base: "1.0.1"}) })
	if got := readOnDispatcher(t, a, a.State).FlowCurrent; got != (FlowBranch{Kind: ops.FlowKindSupport, Name: "1.x"}) {
		t.Fatalf("support branch = %+v", got)
	}
}

func TestIntegrateDevelopLogsConflictsAndFailures(t *testing.T) {
	a, _ := flowReadyApp(t, true)
	operations := captureOperationViews(t)
	prev := runIntegrateDevelop
	t.Cleanup(func() { runIntegrateDevelop = prev })

	runIntegrateDevelop = func(context.Context, *gitrepo.Repository, string, ops.IntegrateDevelopOptions) ([]string, error) {
		return []string{"a.txt"}, nil
	}
	runOnDispatcher(t, a, func() { a.integrateDevelop("login", "topic/login", "develop", true) })
	lines := operationLines(t, a, operations)
	runIntegrateDevelop = func(context.Context, *gitrepo.Repository, string, ops.IntegrateDevelopOptions) ([]string, error) {
		return nil, errors.New("boom")
	}
	runOnDispatcher(t, a, func() { a.integrateDevelop("login", "topic/login", "develop", false) })
	failed := operationLines(t, a, operations)

	if !slices.Contains(lines, i18n.Tf("Operation.Log.MergeConflictPath", "a.txt")) || !slices.Contains(lines, i18n.Tf("Operation.Log.IntegrateStopped", 1)) {
		t.Fatalf("conflict log = %v", lines)
	}
	if slices.Contains(failed, i18n.Tf("Operation.Log.DevelopIntegrated", "develop", "topic/login")) {
		t.Fatalf("failure log = %v", failed)
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
		runOnDispatcher(t, a, func() {
			a.RunOperation("finish", func(_ context.Context, reporter OperationReporter) error {
				reportFinishFlow(reporter, c.kind, "1.0", c.result, c.err)
				return nil
			})
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
	if err := os.WriteFile(filepath.Join(target, ".git", "GOGIT_FLOW"), []byte("hotfix\n2.0\n1\nfalse\nfalse\n2.0\ntrue\n0\n"), 0o666); err != nil {
		t.Fatal(err)
	}
	runOnDispatcher(t, a, a.RefreshRepository)
	if got := readOnDispatcher(t, a, a.State).FlowPending; got != (FlowBranch{Kind: ops.FlowKindHotfix, Name: "2.0"}) {
		t.Fatalf("pending finish = %+v", got)
	}

	prev := openGitRepository
	openGitRepository = func(string, gitrepo.OpenOptions) (*gitrepo.Repository, error) { return nil, errors.New("gone") }
	t.Cleanup(func() { openGitRepository = prev })
	operations := captureOperationViews(t)
	runOnDispatcher(t, a, func() { a.refreshFlowState(a.opened(), "release/1.0") })
	if got := readOnDispatcher(t, a, a.State); got.FlowConfigured || got.FlowPending != (FlowBranch{}) || got.FlowCurrent != (FlowBranch{}) {
		t.Fatalf("flow state over an unreadable repository = %+v", got)
	}
	runOnDispatcher(t, a, func() {
		a.openStartFlow(ops.FlowKindRelease)
		a.openFinishFlow(ops.FlowKindRelease)
		a.openIntegrateDevelop()
		a.openFlowConfig()
	})
	waitForStatusText(t, a, i18n.Tf("Status.FlowReadFailed", errors.New("gone")))
	runOnDispatcher(t, a, func() { a.startFlow(ops.FlowKindRelease, "1.0", "develop") })
	operationLines(t, a, operations)
	runOnDispatcher(t, a, func() { a.finishFlow(ops.FlowKindRelease, "1.0", flow.FinishModel{}) })
	operationLines(t, a, operations)
}

func TestGitFlowCommandsNeedAnOpenRepository(t *testing.T) {
	a := newTestApp(t)
	starts := captureKindViews(t, &newFlowStartView)
	operations := captureOperationViews(t)

	runOnDispatcher(t, a, func() {
		a.openStartFlow(ops.FlowKindRelease)
		a.openFinishFlow(ops.FlowKindRelease)
		a.openIntegrateDevelop()
		a.openFlowConfig()
		a.startFlow(ops.FlowKindRelease, "1.0", "develop")
		a.finishFlow(ops.FlowKindRelease, "1.0", flow.FinishModel{})
		a.integrateDevelop("login", "feature/login", "develop", false)
		a.configureFlow(ops.DefaultFlowConfig())
		a.switchOffFlow()
	})

	if len(*starts) != 0 || len(*operations) != 0 {
		t.Fatalf("views = %d, operations = %d without a repository", len(*starts), len(*operations))
	}
}

func TestGitFlowDialogsOpenOnlyWhenTheyApply(t *testing.T) {
	unconfigured, _ := flowReadyApp(t, false)
	starts := captureKindViews(t, &newFlowStartView)
	finishes := captureKindViews(t, &newFlowFinishView)
	integrations := captureFlowViews(t, &newFlowIntegrateView)
	runOnDispatcher(t, unconfigured, func() {
		unconfigured.openStartFlow(ops.FlowKindFeature)
		unconfigured.openIntegrateDevelop()
	})

	a, _ := flowReadyApp(t, true)
	runOnDispatcher(t, a, func() {
		a.setFlowState(flowOn(releaseBranch, FlowBranch{}))
		a.openFinishFlow(ops.FlowKindHotfix)
		a.openIntegrateDevelop()
		a.openStartFlow("bugfix")
	})

	if len(*starts)+len(*finishes)+len(*integrations) != 0 {
		t.Fatalf("dialogs opened: %d starts, %d finishes, %d integrations", len(*starts), len(*finishes), len(*integrations))
	}
}

func TestGitFlowDialogsThatCannotOpenAreLeftAlone(t *testing.T) {
	a, _ := flowReadyApp(t, true)
	failKindView(t, &newFlowStartView)
	failKindView(t, &newFlowFinishView)
	failFlowView(t, &newFlowConfigView)
	failFlowView(t, &newFlowConfiguredView)
	failFlowView(t, &newFlowIntegrateView)
	operations := captureOperationViews(t)

	runOnDispatcher(t, a, func() {
		a.setFlowState(flowOn(featureBranch, FlowBranch{}))
		a.openStartFlow(ops.FlowKindRelease)
		a.openFinishFlow(ops.FlowKindFeature)
		a.openIntegrateDevelop()
		a.openFlowConfig()
		a.showFlowConfig(ops.DefaultFlowConfig(), nil)
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
	questions := captureFlowViews(t, &newFlowConfiguredView)
	integrations := captureFlowViews(t, &newFlowIntegrateView)
	operations := captureOperationViews(t)

	runOnDispatcher(t, a, func() {
		a.setFlowState(flowOn(featureBranch, FlowBranch{}))
		a.openStartFlow(ops.FlowKindRelease)
		a.openFinishFlow(ops.FlowKindFeature)
		a.openIntegrateDevelop()
		a.Dispatch(CmdFlowConfigure)
		a.showFlowConfig(ops.DefaultFlowConfig(), []string{"origin"})
		(*starts)[0].OnCancel()
		(*finishes)[0].OnCancel()
		(*integrations)[0].OnCancel()
		(*questions)[0].OnCancel()
		(*configs)[0].OnCancel()
	})

	if len(*operations) != 0 {
		t.Fatalf("a cancelled dialog started %d operations", len(*operations))
	}
}

func writeTrackingRef(t *testing.T, target, remote, branch string, id hash.ObjectID) {
	t.Helper()
	path := filepath.Join(target, ".git", "refs", "remotes", remote, filepath.FromSlash(branch))
	if err := os.MkdirAll(filepath.Dir(path), 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(id.String()+"\n"), 0o666); err != nil {
		t.Fatal(err)
	}
}

func TestTheFlowUsesTheConfiguredRemoteOrTheDefault(t *testing.T) {
	a, target := flowReadyApp(t, true)
	r := openRepoAt(t, target)
	cfg, _ := ops.ReadFlowConfig(r)
	if net := a.flowNetwork(r, OperationReporter{app: a}); net.Remote != "" {
		t.Fatalf("network without a remote = %+v", net)
	}
	if fetch, push := a.flowRemoteChoices(r, cfg, ops.FlowKindRelease, "release/1.0"); fetch || push {
		t.Fatal("the finish offers the network without a remote")
	}
	if err := ops.AddRemote(r, "upstream", "https://example.com/flow.git"); err != nil {
		t.Fatal(err)
	}
	cfg.Remote = "upstream"
	if err := ops.WriteFlowConfig(r, cfg); err != nil {
		t.Fatal(err)
	}
	head := readOnDispatcher(t, a, func() hash.ObjectID {
		snap, err := loadBranchSnapshot(a.opened().store)
		if err != nil {
			t.Error(err)
		}
		return snap.HeadID
	})
	writeTrackingRef(t, target, "upstream", "release/1.0", head)
	writeTrackingRef(t, target, "upstream", "topic/login", head)
	withRemote := openRepoAt(t, target)

	net := a.flowNetwork(withRemote, OperationReporter{app: a})
	if net.Remote != "upstream" || net.Progress == nil {
		t.Fatalf("network = %+v", net)
	}
	for _, c := range []struct {
		kind, branch string
		fetch, push  bool
	}{
		{ops.FlowKindRelease, "release/1.0", false, true},
		{ops.FlowKindFeature, "topic/login", true, true},
		{ops.FlowKindFeature, "topic/other", false, false},
	} {
		if fetch, push := a.flowRemoteChoices(withRemote, cfg, c.kind, c.branch); fetch != c.fetch || push != c.push {
			t.Errorf("%s %s: fetch %v push %v", c.kind, c.branch, fetch, push)
		}
	}
	writeTrackingRef(t, target, "upstream", "develop", head)
	if fetch, push := a.flowRemoteChoices(openRepoAt(t, target), cfg, ops.FlowKindHotfix, "hotfix/2.0"); !fetch || !push {
		t.Fatal("a tracked develop does not offer fetch and push")
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
