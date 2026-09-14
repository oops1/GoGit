package app

import (
	"context"
	"errors"
	"slices"

	"github.com/oops1/gogit/internal/gitcore/ops"
	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/flow"
)

var (
	newFlowStartView      = flow.NewStartView
	newFlowFinishView     = flow.NewFinishView
	newFlowConfigView     = flow.NewConfigView
	newFlowConfiguredView = flow.NewConfiguredView
	newFlowIntegrateView  = flow.NewIntegrateView
	runStartFlow          = ops.StartFlow
	runFinishFlow         = ops.FinishFlow
	runPushFinishedFlow   = ops.PushFinishedFlow
	runIntegrateDevelop   = ops.IntegrateDevelop
	runConfigureFlow      = ops.ConfigureFlow
	runSwitchOffFlow      = ops.SwitchOffFlow
)

const flowMainBranch = "main"

type flowOperationKeys struct {
	start    string
	finish   string
	finished string
	stopped  string
}

var flowOperationTexts = map[string]flowOperationKeys{
	ops.FlowKindFeature: {"Operation.Title.StartFeature", "Operation.Title.FinishFeature", "Operation.Log.FeatureFinished", "Operation.Log.FeatureStopped"},
	ops.FlowKindRelease: {"Operation.Title.StartRelease", "Operation.Title.FinishRelease", "Operation.Log.ReleaseFinished", "Operation.Log.ReleaseStopped"},
	ops.FlowKindHotfix:  {"Operation.Title.StartHotfix", "Operation.Title.FinishHotfix", "Operation.Log.HotfixFinished", "Operation.Log.HotfixStopped"},
	ops.FlowKindSupport: {start: "Operation.Title.StartSupport"},
}

var flowStartCommands = map[CommandID]string{
	CmdFlowStartFeature: ops.FlowKindFeature,
	CmdFlowStartRelease: ops.FlowKindRelease,
	CmdFlowStartHotfix:  ops.FlowKindHotfix,
	CmdFlowStartSupport: ops.FlowKindSupport,
}

var flowFinishCommands = map[CommandID]string{
	CmdFlowFinishFeature: ops.FlowKindFeature,
	CmdFlowFinishRelease: ops.FlowKindRelease,
	CmdFlowFinishHotfix:  ops.FlowKindHotfix,
}

type flowStatus struct {
	configured bool
	light      bool
	current    FlowBranch
	pending    FlowBranch
}

func (a *App) registerFlowHandlers() {
	for id, kind := range flowStartCommands {
		a.handlers[id] = func() { a.openStartFlow(kind) }
	}
	for id, kind := range flowFinishCommands {
		a.handlers[id] = func() { a.openFinishFlow(kind) }
	}
	a.handlers[CmdFlowIntegrateDevelop] = a.openIntegrateDevelop
	a.handlers[CmdFlowConfigure] = a.openFlowConfig
}

func (a *App) withFlowRepo(fn func(o *openedRepository, r *gitrepo.Repository, cfg ops.FlowConfig, configured bool)) {
	o := a.opened()
	if o == nil {
		return
	}
	r, err := a.freshRepo(o)
	if err != nil {
		a.log.Warn("read git-flow settings failed", "error", err)
		a.statusLabel.SetText(i18n.Tf("Status.FlowReadFailed", err))
		return
	}
	defer func() { _ = r.Close() }()
	cfg, configured := ops.ReadFlowConfig(r)
	fn(o, r, cfg, configured)
}

func (a *App) localBranchNames(o *openedRepository) []string {
	snap, err := loadBranchSnapshot(o.store)
	if err != nil {
		a.log.Warn("read branches failed", "error", err)
		return nil
	}
	names := make([]string, 0, len(snap.Local))
	for _, branch := range snap.Local {
		names = append(names, branch.Name.Short())
	}
	return names
}

func (a *App) openStartFlow(kind string) {
	a.withFlowRepo(func(o *openedRepository, _ *gitrepo.Repository, cfg ops.FlowConfig, configured bool) {
		branch, err := cfg.Branch(kind)
		if !configured || err != nil {
			return
		}
		view, err := newFlowStartView(kind)
		if err != nil {
			a.log.Warn("open git-flow start dialog failed", "kind", kind, "error", err)
			return
		}
		names := a.localBranchNames(o)
		bases := names
		if kind == ops.FlowKindSupport {
			bases = append(a.tagNames(), names...)
		}
		view.SetKnown(flow.StartKnown{Base: branch.Base, Bases: bases, Prefix: branch.Prefix, Taken: names})
		view.OnOK = func(model flow.StartModel) {
			a.eng.CloseModal(view.Dialog())
			a.startFlow(kind, model.Name, model.Base)
		}
		view.OnCancel = func() { a.eng.CloseModal(view.Dialog()) }
		a.showModal(view.Dialog(), view)
	})
}

func (a *App) runFlowOperation(title string, fn func(ctx context.Context, r *gitrepo.Repository, reporter OperationReporter) error) {
	o := a.opened()
	if o == nil {
		return
	}
	a.RunOperation(title, func(ctx context.Context, reporter OperationReporter) error {
		defer a.Post(a.finishMerge)
		r, err := a.freshRepo(o)
		if err != nil {
			return err
		}
		defer func() { _ = r.Close() }()
		return fn(ctx, r, reporter)
	})
}

func (a *App) startFlow(kind, name, base string) {
	a.runFlowOperation(i18n.Tf(flowOperationTexts[kind].start, name), func(ctx context.Context, r *gitrepo.Repository, reporter OperationReporter) error {
		branch, err := runStartFlow(ctx, r, kind, name, ops.StartFlowOptions{Base: base, Network: a.flowNetwork(r, reporter)})
		if errors.Is(err, ops.ErrFlowTagExists) {
			cfg, _ := ops.ReadFlowConfig(r)
			reporter.Log(i18n.Tf("Operation.Log.FlowTagExists", cfg.VersionTagPrefix+name))
		}
		if err != nil {
			return err
		}
		reporter.Log(i18n.Tf("Operation.Log.FlowStarted", branch.Short()))
		return nil
	})
}

func (a *App) openIntegrateDevelop() {
	a.withFlowRepo(func(_ *openedRepository, _ *gitrepo.Repository, cfg ops.FlowConfig, configured bool) {
		current := a.State().FlowCurrent
		if !configured || current.Kind != ops.FlowKindFeature {
			return
		}
		view, err := newFlowIntegrateView()
		if err != nil {
			a.log.Warn("open integrate develop dialog failed", "error", err)
			return
		}
		feature := cfg.FeaturePrefix + current.Name
		view.SetKnown(feature, cfg.Develop)
		view.OnOK = func(rebase bool) {
			a.eng.CloseModal(view.Dialog())
			a.integrateDevelop(current.Name, feature, cfg.Develop, rebase)
		}
		view.OnCancel = func() { a.eng.CloseModal(view.Dialog()) }
		a.showModal(view.Dialog(), view)
	})
}

func (a *App) integrateDevelop(name, feature, develop string, rebase bool) {
	a.runFlowOperation(i18n.Tf("Operation.Title.IntegrateDevelop", feature), func(ctx context.Context, r *gitrepo.Repository, reporter OperationReporter) error {
		conflicts, err := runIntegrateDevelop(ctx, r, name, ops.IntegrateDevelopOptions{Rebase: rebase})
		if err != nil {
			return err
		}
		if len(conflicts) == 0 {
			reporter.Log(i18n.Tf("Operation.Log.DevelopIntegrated", develop, feature))
			return nil
		}
		logConflicts(reporter, conflicts)
		reporter.Log(i18n.Tf("Operation.Log.IntegrateStopped", len(conflicts)))
		return nil
	})
}

func logConflicts(reporter OperationReporter, conflicts []string) {
	for _, path := range conflicts {
		reporter.Log(i18n.Tf("Operation.Log.MergeConflictPath", path))
	}
}

func (a *App) openFinishFlow(kind string) {
	a.withFlowRepo(func(_ *openedRepository, r *gitrepo.Repository, cfg ops.FlowConfig, configured bool) {
		branch, err := cfg.Branch(kind)
		state := a.State()
		target := state.flowFinishTarget()
		if !configured || err != nil || target.Kind != kind {
			return
		}
		view, err := newFlowFinishView(kind)
		if err != nil {
			a.log.Warn("open git-flow finish dialog failed", "kind", kind, "error", err)
			return
		}
		full := branch.Prefix + target.Name
		canFetch, canPush := a.flowRemoteChoices(r, cfg, kind, full)
		view.SetKnown(flow.FinishKnown{
			Name:     target.Name,
			Branch:   full,
			Master:   cfg.Master,
			Develop:  cfg.Develop,
			Tag:      cfg.VersionTagPrefix + target.Name,
			CanFetch: canFetch,
			CanPush:  canPush,
			Resuming: state.FlowPending.Name != "",
		})
		view.OnOK = func(model flow.FinishModel) {
			a.eng.CloseModal(view.Dialog())
			a.finishFlow(kind, target.Name, model)
		}
		view.OnCancel = func() { a.eng.CloseModal(view.Dialog()) }
		a.showModal(view.Dialog(), view)
	})
}

func (a *App) flowRemoteChoices(r *gitrepo.Repository, cfg ops.FlowConfig, kind, branch string) (bool, bool) {
	name := a.flowRemoteName(r)
	has := func(ref string) bool {
		found, err := ops.HasRemoteBranch(r, name, ref)
		return name != "" && err == nil && found
	}
	if kind == ops.FlowKindFeature {
		tracked := has(branch)
		return tracked, tracked
	}
	targets := has(cfg.Master) || has(cfg.Develop)
	return targets, targets || has(branch)
}

func (a *App) finishFlow(kind, name string, model flow.FinishModel) {
	a.runFlowOperation(i18n.Tf(flowOperationTexts[kind].finish, name), func(ctx context.Context, r *gitrepo.Repository, reporter OperationReporter) error {
		opts := ops.FinishFlowOptions{
			Message:      model.Message,
			Integration:  model.Integration,
			TagName:      model.TagName,
			SkipTag:      model.SkipTag,
			SkipDevelop:  model.SkipDevelop,
			Fetch:        model.Fetch,
			Push:         model.Push,
			DeleteBranch: model.DeleteBranch,
			Network:      a.flowNetwork(r, reporter),
		}
		result, err := runFinishFlow(ctx, r, kind, name, opts)
		reportFinishFlow(reporter, kind, name, result, err)
		opts.SkipTag = opts.SkipTag || result.KeptTag != ""
		if err == nil && result.Finished() && !result.Pushed && opts.Network.Remote != "" {
			reporter.Log(i18n.Tf("Operation.Log.FlowNotPushed", name))
			a.Post(func() { a.offerFlowPush(kind, name, opts) })
		}
		return err
	})
}

func (a *App) offerFlowPush(kind, name string, opts ops.FinishFlowOptions) {
	a.askConfirm(i18n.T("Dialog.FlowPush.Title"), i18n.Tf("Dialog.FlowPush.Message", name), func(ok bool) {
		if !ok {
			return
		}
		a.runFlowOperation(i18n.Tf("Operation.Title.FlowPush", name), func(ctx context.Context, r *gitrepo.Repository, reporter OperationReporter) error {
			opts.Network = a.flowNetwork(r, reporter)
			err := runPushFinishedFlow(ctx, r, kind, name, opts)
			if err == nil {
				reporter.Log(i18n.Tf("Operation.Log.FlowPushed", name))
			}
			return err
		})
	})
}

func reportFinishFlow(reporter OperationReporter, kind, name string, result ops.FinishFlowResult, err error) {
	keys := flowOperationTexts[kind]
	if result.KeptTag != "" {
		reporter.Log(i18n.Tf("Operation.Log.FlowTagKept", result.KeptTag))
	}
	switch {
	case errors.Is(err, ops.ErrFlowBehind):
		reporter.Log(i18n.T("Operation.Log.FlowBehind"))
	case err != nil:
	case result.Finished():
		reporter.Log(i18n.Tf(keys.finished, name))
	default:
		logConflicts(reporter, result.Conflicts)
		reporter.Log(i18n.Tf(keys.stopped, len(result.Conflicts)))
	}
}

func (a *App) flowRemoteName(r *gitrepo.Repository) string {
	cfg, _ := ops.ReadFlowConfig(r)
	for _, name := range []string{cfg.Remote, a.effectiveDefaultRemote(r)} {
		if _, ok := r.Config().Remote(name); ok {
			return name
		}
	}
	return ""
}

func (a *App) flowNetwork(r *gitrepo.Repository, reporter OperationReporter) ops.FlowNetwork {
	name := a.flowRemoteName(r)
	if name == "" {
		return ops.FlowNetwork{}
	}
	prog := newOperationProgress(reporter)
	return ops.FlowNetwork{Remote: name, Progress: prog, Transport: a.transportOptions(prog)}
}

func (a *App) openFlowConfig() {
	a.withFlowRepo(func(o *openedRepository, r *gitrepo.Repository, cfg ops.FlowConfig, configured bool) {
		var remotes []string
		for _, known := range r.Config().Remotes() {
			remotes = append(remotes, known.Name)
		}
		if !configured {
			a.showFlowConfig(suggestedFlowConfig(cfg, a.localBranchNames(o)), remotes)
			return
		}
		view, err := newFlowConfiguredView()
		if err != nil {
			a.log.Warn("open git-flow question failed", "error", err)
			return
		}
		view.OnChange = func() {
			a.eng.CloseModal(view.Dialog())
			a.showFlowConfig(cfg, remotes)
		}
		view.OnSwitchOff = func() {
			a.eng.CloseModal(view.Dialog())
			a.switchOffFlow()
		}
		view.OnCancel = func() { a.eng.CloseModal(view.Dialog()) }
		a.showModal(view.Dialog(), view)
	})
}

func (a *App) showFlowConfig(cfg ops.FlowConfig, remotes []string) {
	view, err := newFlowConfigView()
	if err != nil {
		a.log.Warn("open git-flow settings dialog failed", "error", err)
		return
	}
	view.SetRemotes(remotes)
	view.SetModel(flow.ConfigModelOf(cfg))
	view.OnOK = func(model flow.ConfigModel) {
		a.eng.CloseModal(view.Dialog())
		a.configureFlow(model.FlowConfig())
	}
	view.OnCancel = func() { a.eng.CloseModal(view.Dialog()) }
	a.showModal(view.Dialog(), view)
}

func suggestedFlowConfig(cfg ops.FlowConfig, branches []string) ops.FlowConfig {
	if !slices.Contains(branches, cfg.Master) && slices.Contains(branches, flowMainBranch) {
		cfg.Master = flowMainBranch
	}
	return cfg
}

func (a *App) configureFlow(cfg ops.FlowConfig) {
	a.runFlowOperation(i18n.T("Operation.Title.ConfigureFlow"), func(ctx context.Context, r *gitrepo.Repository, reporter OperationReporter) error {
		created, err := runConfigureFlow(ctx, r, cfg)
		for _, branch := range created {
			reporter.Log(i18n.Tf("Operation.Log.FlowBranchCreated", branch.Short()))
		}
		if err != nil {
			return err
		}
		reporter.Log(i18n.T("Operation.Log.FlowConfigured"))
		return nil
	})
}

func (a *App) switchOffFlow() {
	a.runFlowOperation(i18n.T("Operation.Title.SwitchOffFlow"), func(_ context.Context, r *gitrepo.Repository, reporter OperationReporter) error {
		if err := runSwitchOffFlow(r); err != nil {
			return err
		}
		reporter.Log(i18n.T("Operation.Log.FlowSwitchedOff"))
		return nil
	})
}

func (a *App) refreshFlowState(o *openedRepository, current string) {
	var status flowStatus
	if r, err := a.freshRepo(o); err == nil {
		cfg, configured := ops.ReadFlowConfig(r)
		status.configured, status.light = configured, configured && cfg.Light()
		if kind, name, ok := cfg.BranchKind(current); configured && ok {
			status.current = FlowBranch{Kind: kind, Name: name}
		}
		if finish, found, err := ops.PendingFlowFinish(r); err == nil && found {
			status.pending = FlowBranch{Kind: finish.Kind, Name: finish.Name}
		}
		_ = r.Close()
	}
	a.setFlowState(status)
}

func (a *App) setFlowState(status flowStatus) {
	a.mu.Lock()
	next := a.state
	next.FlowConfigured, next.FlowLight = status.configured, status.light
	next.FlowCurrent, next.FlowPending = status.current, status.pending
	changed := next != a.state
	a.state = next
	a.mu.Unlock()
	if changed {
		a.refreshCommands()
	}
}
