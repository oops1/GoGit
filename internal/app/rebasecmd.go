package app

import (
	"context"
	"errors"

	"github.com/oops1/gogit/internal/gitcore/ops"
	"github.com/oops1/gogit/internal/gitcore/progress"
	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/commit"
	"github.com/oops1/gogit/internal/ui/rebase"
	"github.com/oops1/gogit/internal/ui/rebasetodo"
)

var newRebaseView = rebase.NewView

var newRebaseTodoView = rebasetodo.NewView

var plannedRebase = ops.PlannedRebase

var runRebase = ops.Rebase

var runContinueRebase = ops.ContinueRebase

var runSkipRebase = ops.SkipRebase

func (a *App) registerRebaseHandlers() {
	a.handlers[CmdRebase] = func() { a.openRebase("") }
	a.handlers[CmdRebaseSteps] = func() { a.openRebaseSteps("") }
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

func (a *App) openRebaseSteps(selected string) {
	o := a.opened()
	if o == nil {
		return
	}
	snap, err := loadBranchSnapshot(o.store)
	if err != nil {
		a.log.Warn("read branches for the rebase dialog failed", "error", err)
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
		a.openTodo(onto)
	}
	view.OnCancel = func() { a.eng.CloseModal(view.Dialog()) }
	a.showModal(view.Dialog(), view)
}

func (a *App) openTodo(onto string) {
	o := a.opened()
	if o == nil {
		return
	}
	r, err := a.freshRepo(o)
	if err != nil {
		a.log.Warn("open repository for the rebase plan failed", "error", err)
		return
	}
	defer func() { _ = r.Close() }()
	planned, err := plannedRebase(context.Background(), r, onto)
	if err != nil {
		a.log.Warn("read the rebase plan failed", "error", err)
		return
	}
	view, err := newRebaseTodoView()
	if err != nil {
		a.log.Warn("open rebase steps dialog failed", "error", err)
		return
	}
	view.SetSteps(onto, todoSteps(planned))
	view.OnOK = func(steps []rebasetodo.Step) {
		a.eng.CloseModal(view.Dialog())
		a.startRebaseSteps(onto, steps)
	}
	view.OnCancel = func() { a.eng.CloseModal(view.Dialog()) }
	a.showModal(view.Dialog(), view)
}

func todoSteps(planned []ops.RebaseStep) []rebasetodo.Step {
	steps := make([]rebasetodo.Step, 0, len(planned))
	for _, step := range planned {
		steps = append(steps, rebasetodo.Step{Action: step.Action, Commit: step.Commit, Subject: step.Subject})
	}
	return steps
}

func plannedSteps(steps []rebasetodo.Step) []ops.RebaseStep {
	planned := make([]ops.RebaseStep, 0, len(steps))
	for _, step := range steps {
		planned = append(planned, ops.RebaseStep{Action: step.Action, Commit: step.Commit, Subject: step.Subject})
	}
	return planned
}

func (a *App) startRebaseSteps(onto string, steps []rebasetodo.Step) {
	todo := plannedSteps(steps)
	a.runRebaseJob(func(ctx context.Context, r *gitrepo.Repository, prog progress.Func) (ops.RebaseResult, error) {
		return runRebase(ctx, r, onto, ops.RebaseOptions{Progress: prog, Todo: todo})
	})
}

func (a *App) continueOperation() {
	switch state := a.State(); {
	case state.Rewording:
		a.openReword()
	case state.Rebasing:
		a.continueRebase("")
	default:
		a.openCommit()
	}
}

func (a *App) openReword() {
	a.mu.Lock()
	message := a.rewordMessage
	a.mu.Unlock()
	a.showCommit(commit.Model{Message: message, Merging: true}, func(model commit.Model, _ bool) {
		a.continueRebase(model.Message)
	})
}

func (a *App) continueRebase(message string) {
	a.runRebaseJob(func(ctx context.Context, r *gitrepo.Repository, prog progress.Func) (ops.RebaseResult, error) {
		return runContinueRebase(ctx, r, ops.RebaseOptions{Progress: prog, Message: message})
	})
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
		a.Post(func() { a.setReword(result.Message, result.Amending()) })
		return err
	})
}

func (a *App) setReword(message string, rewording bool) {
	a.mu.Lock()
	changed := a.state.Rewording != rewording
	a.state.Rewording, a.rewordMessage = rewording, message
	a.mu.Unlock()
	if changed {
		a.refreshCommands()
	}
}

func reportRebase(reporter OperationReporter, result ops.RebaseResult, err error) {
	var overwrite *ops.OverwriteError
	switch {
	case errors.As(err, &overwrite):
		reporter.Log(i18n.Tf("Operation.Log.RebaseBlocked", overwriteList(overwrite)))
	case err != nil:
	case result.UpToDate:
		reporter.Log(i18n.T("Operation.Log.MergeUpToDate"))
	case result.Amending():
		reporter.Log(i18n.Tf("Operation.Log.RebaseReword", shortHash(result.Stopped)))
	case !result.Finished():
		reportRebaseStop(reporter, result)
	default:
		reporter.Log(i18n.Tf("Operation.Log.Rebased", result.Applied, shortHash(result.New)))
	}
}
