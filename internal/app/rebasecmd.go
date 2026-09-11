package app

import (
	"context"
	"errors"

	"github.com/oops1/gogit/internal/gitcore/ops"
	"github.com/oops1/gogit/internal/gitcore/progress"
	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/rebase"
)

var newRebaseView = rebase.NewView

var runRebase = ops.Rebase

var runContinueRebase = ops.ContinueRebase

var runSkipRebase = ops.SkipRebase

func (a *App) registerRebaseHandlers() {
	a.handlers[CmdRebase] = func() { a.openRebase("") }
	a.handlers[CmdContinue] = a.continueOperation
	a.handlers[CmdSkip] = a.skipRebaseStep
}

func (a *App) openRebase(selected string) {
	o := a.opened()
	if o == nil {
		return
	}
	snap, err := loadBranchSnapshot(o.store)
	if err != nil {
		a.log.Warn("read branches for rebase dialog failed", "error", err)
		return
	}
	view, err := newRebaseView()
	if err != nil {
		a.log.Warn("open rebase dialog failed", "error", err)
		return
	}
	known := mergeCandidates(snap)
	view.SetKnown(rebase.Known{Current: known.Current, Candidates: known.Candidates}, selected)
	view.OnOK = func(onto string) {
		a.eng.CloseModal(view.Dialog())
		a.startRebase(onto)
	}
	view.OnCancel = func() { a.eng.CloseModal(view.Dialog()) }
	a.showModal(view.Dialog(), view)
}

func (a *App) startRebase(onto string) {
	a.runRebaseJob(func(ctx context.Context, r *gitrepo.Repository, prog progress.Func) (ops.RebaseResult, error) {
		return runRebase(ctx, r, onto, ops.RebaseOptions{Progress: prog})
	})
}

func (a *App) continueOperation() {
	if a.State().Rebasing {
		a.runRebaseJob(func(ctx context.Context, r *gitrepo.Repository, prog progress.Func) (ops.RebaseResult, error) {
			return runContinueRebase(ctx, r, ops.RebaseOptions{Progress: prog})
		})
		return
	}
	a.openCommit()
}

func (a *App) skipRebaseStep() {
	a.runRebaseJob(func(ctx context.Context, r *gitrepo.Repository, prog progress.Func) (ops.RebaseResult, error) {
		return runSkipRebase(ctx, r, ops.RebaseOptions{Progress: prog})
	})
}

type rebaseJob func(ctx context.Context, r *gitrepo.Repository, prog progress.Func) (ops.RebaseResult, error)

func (a *App) runRebaseJob(job rebaseJob) {
	o := a.opened()
	if o == nil {
		return
	}
	a.RunOperation(i18n.T("Operation.Title.Rebase"), func(ctx context.Context, reporter OperationReporter) error {
		defer a.Post(a.finishMerge)
		r, err := a.freshRepo(o)
		if err != nil {
			return err
		}
		defer func() { _ = r.Close() }()
		result, err := job(ctx, r, newOperationProgress(reporter))
		reportRebase(reporter, result, err)
		return err
	})
}

func reportRebase(reporter OperationReporter, result ops.RebaseResult, err error) {
	var overwrite *ops.OverwriteError
	switch {
	case errors.As(err, &overwrite):
		reporter.Log(i18n.Tf("Operation.Log.RebaseBlocked", overwriteList(overwrite)))
	case err != nil:
	case result.UpToDate:
		reporter.Log(i18n.T("Operation.Log.MergeUpToDate"))
	case !result.Finished():
		reportRebaseStop(reporter, result)
	default:
		reporter.Log(i18n.Tf("Operation.Log.Rebased", result.Applied, shortHash(result.New)))
	}
}
