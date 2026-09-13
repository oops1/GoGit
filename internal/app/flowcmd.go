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
	newFlowStartView   = flow.NewStartView
	newFlowFinishView  = flow.NewFinishView
	newFlowConfigView  = flow.NewConfigView
	runStartFlow       = ops.StartFlow
	runFinishFlow      = ops.FinishFlow
	runWriteFlowConfig = ops.WriteFlowConfig
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
}

var flowStartCommands = map[CommandID]string{
	CmdFlowStartFeature: ops.FlowKindFeature,
	CmdFlowStartRelease: ops.FlowKindRelease,
	CmdFlowStartHotfix:  ops.FlowKindHotfix,
}

var flowFinishCommands = map[CommandID]string{
	CmdFlowFinishFeature: ops.FlowKindFeature,
	CmdFlowFinishRelease: ops.FlowKindRelease,
	CmdFlowFinishHotfix:  ops.FlowKindHotfix,
}

func (a *App) registerFlowHandlers() {
	for id, kind := range flowStartCommands {
		a.handlers[id] = func() { a.openStartFlow(kind) }
	}
	for id, kind := range flowFinishCommands {
		a.handlers[id] = func() { a.openFinishFlow(kind) }
	}
	a.handlers[CmdFlowConfigure] = func() { a.openFlowConfig(nil) }
}

func (a *App) readFlowConfig(o *openedRepository) (ops.FlowConfig, bool, bool) {
	r, err := a.freshRepo(o)
	if err != nil {
		a.log.Warn("read git-flow settings failed", "error", err)
		a.statusLabel.SetText(i18n.Tf("Status.FlowReadFailed", err))
		return ops.FlowConfig{}, false, false
	}
	defer func() { _ = r.Close() }()
	cfg, configured := ops.ReadFlowConfig(r)
	return cfg, configured, true
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

func (a *App) configuredFlowFor(o *openedRepository, reopen func()) (ops.FlowConfig, bool) {
	cfg, configured, ok := a.readFlowConfig(o)
	if ok && !configured {
		a.openFlowConfig(reopen)
	}
	return cfg, ok && configured
}

func (a *App) openStartFlow(kind string) {
	o := a.opened()
	if o == nil {
		return
	}
	cfg, ok := a.configuredFlowFor(o, func() { a.openStartFlow(kind) })
	if !ok {
		return
	}
	view, err := newFlowStartView(kind)
	if err != nil {
		a.log.Warn("open git-flow start dialog failed", "kind", kind, "error", err)
		return
	}
	branch, _ := cfg.Branch(kind)
	view.SetKnown(flow.StartKnown{Base: branch.Base, Prefix: branch.Prefix, Taken: a.localBranchNames(o)})
	view.OnOK = func(model flow.StartModel) {
		a.eng.CloseModal(view.Dialog())
		a.startFlow(kind, model.Name)
	}
	view.OnCancel = func() { a.eng.CloseModal(view.Dialog()) }
	a.showModal(view.Dialog(), view)
}

func (a *App) startFlow(kind, name string) {
	o := a.opened()
	if o == nil {
		return
	}
	a.RunOperation(i18n.Tf(flowOperationTexts[kind].start, name), func(ctx context.Context, reporter OperationReporter) error {
		defer a.Post(a.finishMerge)
		r, err := a.freshRepo(o)
		if err != nil {
			return err
		}
		defer func() { _ = r.Close() }()
		branch, err := runStartFlow(ctx, r, kind, name, ops.StartFlowOptions{Network: a.flowNetwork(r, reporter)})
		if err != nil {
			return err
		}
		reporter.Log(i18n.Tf("Operation.Log.FlowStarted", branch.Short()))
		return nil
	})
}

func (a *App) openFinishFlow(kind string) {
	o := a.opened()
	if o == nil {
		return
	}
	cfg, ok := a.configuredFlowFor(o, func() { a.openFinishFlow(kind) })
	if !ok {
		return
	}
	state := a.State()
	target := state.flowFinishTarget()
	if target.Kind != kind {
		return
	}
	view, err := newFlowFinishView(kind)
	if err != nil {
		a.log.Warn("open git-flow finish dialog failed", "kind", kind, "error", err)
		return
	}
	branch, _ := cfg.Branch(kind)
	view.SetKnown(flow.FinishKnown{
		Name:     target.Name,
		Branch:   branch.Prefix + target.Name,
		Master:   cfg.Master,
		Develop:  cfg.Develop,
		Tag:      cfg.VersionTagPrefix + target.Name,
		CanPush:  state.HasRemotes,
		Resuming: state.FlowPending.Name != "",
	})
	view.OnOK = func(model flow.FinishModel) {
		a.eng.CloseModal(view.Dialog())
		a.finishFlow(kind, target.Name, model)
	}
	view.OnCancel = func() { a.eng.CloseModal(view.Dialog()) }
	a.showModal(view.Dialog(), view)
}

func (a *App) finishFlow(kind, name string, model flow.FinishModel) {
	o := a.opened()
	if o == nil {
		return
	}
	a.RunOperation(i18n.Tf(flowOperationTexts[kind].finish, name), func(ctx context.Context, reporter OperationReporter) error {
		defer a.Post(a.finishMerge)
		r, err := a.freshRepo(o)
		if err != nil {
			return err
		}
		defer func() { _ = r.Close() }()
		result, err := runFinishFlow(ctx, r, kind, name, ops.FinishFlowOptions{
			Message:      model.Message,
			Push:         model.Push,
			DeleteBranch: model.DeleteBranch,
			Network:      a.flowNetwork(r, reporter),
		})
		reportFinishFlow(reporter, kind, name, result, err)
		return err
	})
}

func reportFinishFlow(reporter OperationReporter, kind, name string, result ops.FinishFlowResult, err error) {
	keys := flowOperationTexts[kind]
	switch {
	case errors.Is(err, ops.ErrFlowBehind):
		reporter.Log(i18n.T("Operation.Log.FlowBehind"))
	case err != nil:
	case result.Finished():
		reporter.Log(i18n.Tf(keys.finished, name))
	default:
		for _, path := range result.Conflicts {
			reporter.Log(i18n.Tf("Operation.Log.MergeConflictPath", path))
		}
		reporter.Log(i18n.Tf(keys.stopped, len(result.Conflicts)))
	}
}

func (a *App) flowNetwork(r *gitrepo.Repository, reporter OperationReporter) ops.FlowNetwork {
	name := a.effectiveDefaultRemote(r)
	if _, ok := r.Config().Remote(name); !ok {
		return ops.FlowNetwork{}
	}
	prog := newOperationProgress(reporter)
	return ops.FlowNetwork{Remote: name, Progress: prog, Transport: a.transportOptions(prog)}
}

func (a *App) openFlowConfig(then func()) {
	o := a.opened()
	if o == nil {
		return
	}
	cfg, configured, ok := a.readFlowConfig(o)
	if !ok {
		return
	}
	if !configured {
		cfg = suggestedFlowConfig(cfg, a.localBranchNames(o))
	}
	view, err := newFlowConfigView()
	if err != nil {
		a.log.Warn("open git-flow settings dialog failed", "error", err)
		return
	}
	view.SetModel(flow.ConfigModel{
		Master:     cfg.Master,
		Develop:    cfg.Develop,
		Feature:    cfg.FeaturePrefix,
		Release:    cfg.ReleasePrefix,
		Hotfix:     cfg.HotfixPrefix,
		Support:    cfg.SupportPrefix,
		VersionTag: cfg.VersionTagPrefix,
	})
	view.OnOK = func(model flow.ConfigModel) {
		a.eng.CloseModal(view.Dialog())
		a.saveFlowConfig(model, then)
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

func (a *App) saveFlowConfig(model flow.ConfigModel, then func()) {
	cfg := ops.FlowConfig{
		Master:           model.Master,
		Develop:          model.Develop,
		FeaturePrefix:    model.Feature,
		ReleasePrefix:    model.Release,
		HotfixPrefix:     model.Hotfix,
		SupportPrefix:    model.Support,
		VersionTagPrefix: model.VersionTag,
	}
	a.startWrite(func(_ context.Context, r *gitrepo.Repository) error {
		return runWriteFlowConfig(r, cfg)
	}, func(err error) {
		if err != nil {
			a.log.Warn("save git-flow settings failed", "error", err)
			a.statusLabel.SetText(i18n.Tf("Status.FlowConfigFailed", err))
			return
		}
		a.statusLabel.SetText(i18n.T("Status.FlowConfigSaved"))
		a.RefreshRepository()
		if then != nil {
			then()
		}
	})
}

func (a *App) refreshFlowState(o *openedRepository, current string) {
	var branch, pending FlowBranch
	if r, err := a.freshRepo(o); err == nil {
		if cfg, configured := ops.ReadFlowConfig(r); configured {
			if kind, name, ok := cfg.BranchKind(current); ok {
				branch = FlowBranch{Kind: kind, Name: name}
			}
		}
		if finish, found, err := ops.PendingFlowFinish(r); err == nil && found {
			pending = FlowBranch{Kind: finish.Kind, Name: finish.Name}
		}
		_ = r.Close()
	}
	a.setFlowState(branch, pending)
}

func (a *App) setFlowState(current, pending FlowBranch) {
	a.mu.Lock()
	changed := a.state.FlowCurrent != current || a.state.FlowPending != pending
	a.state.FlowCurrent, a.state.FlowPending = current, pending
	a.mu.Unlock()
	if changed {
		a.refreshCommands()
	}
}
