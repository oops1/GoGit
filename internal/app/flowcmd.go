package app

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/oops1/gogit/internal/gitcore/ops"
	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/flow"
)

var (
	newFlowStartView   = flow.NewStartView
	newFlowFinishView  = flow.NewFinishView
	newFlowConfigView  = flow.NewConfigView
	runStartRelease    = ops.StartRelease
	runFinishRelease   = ops.FinishRelease
	runWriteFlowConfig = ops.WriteFlowConfig
)

const flowMainBranch = "main"

func (a *App) registerFlowHandlers() {
	a.handlers[CmdFlowStartRelease] = a.openStartRelease
	a.handlers[CmdFlowFinishRelease] = a.openFinishRelease
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

func (a *App) openStartRelease() {
	o := a.opened()
	if o == nil {
		return
	}
	cfg, configured, ok := a.readFlowConfig(o)
	if !ok {
		return
	}
	if !configured {
		a.openFlowConfig(a.openStartRelease)
		return
	}
	view, err := newFlowStartView()
	if err != nil {
		a.log.Warn("open start release dialog failed", "error", err)
		return
	}
	view.SetKnown(flow.StartKnown{Develop: cfg.Develop, Prefix: cfg.ReleasePrefix, Taken: a.localBranchNames(o)})
	view.OnOK = func(model flow.StartModel) {
		a.eng.CloseModal(view.Dialog())
		a.startRelease(model.Version)
	}
	view.OnCancel = func() { a.eng.CloseModal(view.Dialog()) }
	a.showModal(view.Dialog(), view)
}

func (a *App) startRelease(version string) {
	o := a.opened()
	if o == nil {
		return
	}
	a.RunOperation(i18n.Tf("Operation.Title.StartRelease", version), func(ctx context.Context, reporter OperationReporter) error {
		defer a.Post(a.finishMerge)
		r, err := a.freshRepo(o)
		if err != nil {
			return err
		}
		defer func() { _ = r.Close() }()
		branch, err := runStartRelease(ctx, r, version, ops.StartReleaseOptions{Network: a.flowNetwork(r, reporter)})
		if err != nil {
			return err
		}
		reporter.Log(i18n.Tf("Operation.Log.ReleaseStarted", branch.Short()))
		return nil
	})
}

func (a *App) openFinishRelease() {
	o := a.opened()
	if o == nil {
		return
	}
	cfg, configured, ok := a.readFlowConfig(o)
	if !ok {
		return
	}
	if !configured {
		a.openFlowConfig(a.openFinishRelease)
		return
	}
	state := a.State()
	version, resuming := state.FlowPending, state.FlowPending != ""
	if !resuming {
		version = state.FlowRelease
	}
	if version == "" {
		return
	}
	view, err := newFlowFinishView()
	if err != nil {
		a.log.Warn("open finish release dialog failed", "error", err)
		return
	}
	view.SetKnown(flow.FinishKnown{
		Version:  version,
		Branch:   cfg.ReleasePrefix + version,
		Master:   cfg.Master,
		Develop:  cfg.Develop,
		Tag:      cfg.VersionTagPrefix + version,
		CanPush:  state.HasRemotes,
		Resuming: resuming,
	})
	view.OnOK = func(model flow.FinishModel) {
		a.eng.CloseModal(view.Dialog())
		a.finishRelease(version, model)
	}
	view.OnCancel = func() { a.eng.CloseModal(view.Dialog()) }
	a.showModal(view.Dialog(), view)
}

func (a *App) finishRelease(version string, model flow.FinishModel) {
	o := a.opened()
	if o == nil {
		return
	}
	a.RunOperation(i18n.Tf("Operation.Title.FinishRelease", version), func(ctx context.Context, reporter OperationReporter) error {
		defer a.Post(a.finishMerge)
		r, err := a.freshRepo(o)
		if err != nil {
			return err
		}
		defer func() { _ = r.Close() }()
		result, err := runFinishRelease(ctx, r, version, ops.FinishReleaseOptions{
			TagMessage:   model.TagMessage,
			Push:         model.Push,
			DeleteBranch: model.DeleteBranch,
			Network:      a.flowNetwork(r, reporter),
		})
		reportFinishRelease(reporter, version, result, err)
		return err
	})
}

func reportFinishRelease(reporter OperationReporter, version string, result ops.FinishReleaseResult, err error) {
	switch {
	case errors.Is(err, ops.ErrFlowBehind):
		reporter.Log(i18n.T("Operation.Log.ReleaseBehind"))
	case err != nil:
	case result.Finished():
		reporter.Log(i18n.Tf("Operation.Log.ReleaseFinished", version))
	default:
		for _, path := range result.Conflicts {
			reporter.Log(i18n.Tf("Operation.Log.MergeConflictPath", path))
		}
		reporter.Log(i18n.Tf("Operation.Log.ReleaseStopped", len(result.Conflicts)))
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
	release, pending := "", ""
	if r, err := a.freshRepo(o); err == nil {
		cfg, configured := ops.ReadFlowConfig(r)
		if name, cut := strings.CutPrefix(current, cfg.ReleasePrefix); configured && cut && cfg.ReleasePrefix != "" && name != "" {
			release = name
		}
		if finish, found, err := ops.PendingFlowFinish(r); err == nil && found && finish.Kind == ops.FlowKindRelease {
			pending = finish.Name
		}
		_ = r.Close()
	}
	a.setFlowState(release, pending)
}

func (a *App) setFlowState(release, pending string) {
	a.mu.Lock()
	changed := a.state.FlowRelease != release || a.state.FlowPending != pending
	a.state.FlowRelease, a.state.FlowPending = release, pending
	a.mu.Unlock()
	if changed {
		a.refreshCommands()
	}
}
