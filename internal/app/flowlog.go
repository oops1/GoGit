package app

import (
	"context"
	"errors"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/ops"
	"github.com/oops1/gogit/internal/gitcore/refs"
	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/gitcore/revision"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/flow"
)

var newFlowLogView = flow.NewLogView

var readFlowLog = flowLogCommits

func (a *App) openFlowLog(target *flow.FinishView) {
	o := a.opened()
	if o == nil {
		return
	}
	var commits []flow.LogCommit
	a.startRead(func(ctx context.Context, _ *gitrepo.Repository) error {
		loaded, err := readFlowLog(ctx, o, a.cfg.Git.LogMaxCount)
		commits = loaded
		return err
	}, func(err error) {
		if err != nil {
			a.log.Warn("read the log for a git-flow message failed", "error", err)
			a.statusLabel.SetText(i18n.Tf("Status.FlowLogFailed", err))
			return
		}
		a.showFlowLog(target, commits)
	})
}

func (a *App) showFlowLog(target *flow.FinishView, commits []flow.LogCommit) {
	view, err := newFlowLogView()
	if err != nil {
		a.log.Warn("open the log picker failed", "error", err)
		return
	}
	view.SetCommits(commits)
	view.OnOK = func(commit flow.LogCommit) {
		a.eng.CloseModal(view.Dialog())
		target.SetMessage(commit.Message)
	}
	view.OnCancel = func() { a.eng.CloseModal(view.Dialog()) }
	a.showModal(view.Dialog(), view)
}

func flowLogCommits(ctx context.Context, o *openedRepository, max int) ([]flow.LogCommit, error) {
	head, err := o.store.Resolve(refs.HEAD)
	if errors.Is(err, refs.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	graph, _ := ops.OpenCommitGraph(o.repo, o.db)
	source := revision.Context{Objects: o.db, Refs: o.store, Shallow: o.shallow, Graph: graph}
	walk := revision.Options{
		Context:  source,
		Include:  []hash.ObjectID{head.Target},
		Order:    revision.DateOrder,
		MaxCount: max,
	}
	var out []flow.LogCommit
	for commit, err := range revision.Walk(ctx, walk) {
		if err != nil {
			return nil, err
		}
		out = append(out, flow.LogCommit{
			Commit:  commit.ID,
			When:    commit.Author.When,
			Author:  commit.Author.Name,
			Message: commit.Message,
		})
	}
	return out, nil
}
