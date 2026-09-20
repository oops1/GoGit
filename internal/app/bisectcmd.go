package app

import (
	"context"
	"slices"

	"github.com/oops1/gogit/internal/gitcore/ops"
	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/branches"
	"github.com/oops1/gogit/internal/ui/dialogs/bisect"
)

var newBisectView = bisect.NewView

var runStartBisect = ops.StartBisect

var runMarkBisect = ops.MarkBisect

var readBisectStatus = ops.ReadBisectStatus

func (a *App) registerBisectHandlers() {
	a.handlers[CmdBisect] = a.openBisect
	a.banner.good.OnClick = func() { a.markBisect(ops.BisectGood) }
	a.banner.bad.OnClick = func() { a.markBisect(ops.BisectBad) }
	a.banner.skip.OnClick = func() { a.markBisect(ops.BisectSkip) }
}

func bisectCandidates(snap branches.Snapshot) bisect.Known {
	known := bisect.Known{Bad: snap.Current}
	for _, b := range snap.Local {
		known.Revs = append(known.Revs, b.Name.Short())
	}
	for _, r := range snap.Remotes {
		for _, b := range r.Branches {
			if b.SymbolicTarget == "" {
				known.Revs = append(known.Revs, b.Name.Short())
			}
		}
	}
	for _, t := range snap.Tags {
		known.Revs = append(known.Revs, t.Name.Short())
	}
	if !slices.Contains(known.Revs, known.Bad) && len(known.Revs) > 0 {
		known.Bad = known.Revs[0]
	}
	for _, rev := range known.Revs {
		if rev != known.Bad {
			known.Good = rev
			break
		}
	}
	return known
}

func (a *App) openBisect() {
	o := a.opened()
	if o == nil {
		return
	}
	snap, err := loadBranchSnapshot(o.store)
	if err != nil {
		a.log.Warn("read branches for bisect dialog failed", "error", err)
		return
	}
	view, err := newBisectView()
	if err != nil {
		a.log.Warn("open bisect dialog failed", "error", err)
		return
	}
	view.SetKnown(bisectCandidates(snap))
	view.OnOK = func(choice bisect.Choice) {
		a.eng.CloseModal(view.Dialog())
		a.startBisect(choice)
	}
	view.OnCancel = func() { a.eng.CloseModal(view.Dialog()) }
	a.showModal(view.Dialog(), view)
}

func (a *App) startBisect(choice bisect.Choice) {
	var status ops.BisectStatus
	a.startWrite(func(ctx context.Context, r *gitrepo.Repository) error {
		var err error
		status, err = runStartBisect(ctx, r, ops.BisectStartOptions{Bad: choice.Bad, Good: []string{choice.Good}})
		return err
	}, func(err error) { a.reportBisect(status, err) })
}

func (a *App) markBisect(mark ops.BisectMark) {
	var status ops.BisectStatus
	a.startWrite(func(ctx context.Context, r *gitrepo.Repository) error {
		var err error
		status, err = runMarkBisect(ctx, r, mark)
		return err
	}, func(err error) { a.reportBisect(status, err) })
}

func (a *App) reportBisect(status ops.BisectStatus, err error) {
	if err != nil {
		a.log.Warn("bisect step failed", "error", err)
		a.statusLabel.SetText(i18n.Tf("Status.BisectFailed", err))
		return
	}
	a.statusLabel.SetText(bisectStatusText(status))
	a.RefreshRepository()
}

func bisectStatusText(status ops.BisectStatus) string {
	switch status.Outcome {
	case ops.BisectFound:
		return i18n.Tf("Status.Bisect.Found", shortHash(status.Current), status.Subject)
	case ops.BisectOnlySkipped:
		return i18n.Tf("Status.Bisect.OnlySkipped", len(status.Candidates))
	case ops.BisectAmbiguous:
		return i18n.Tf("Status.Bisect.Ambiguous", shortHash(status.Current))
	case ops.BisectMergeBase:
		return i18n.Tf("Status.Bisect.MergeBase", shortHash(status.Current))
	case ops.BisectTesting:
		return i18n.Tf("Status.Bisect.Testing", status.Remaining, status.Steps)
	}
	return i18n.T("Status.Bisect.Pending")
}

func bisectBannerText(status ops.BisectStatus) string {
	origin := bisectOriginLabel(status.Start)
	switch status.Outcome {
	case ops.BisectFound:
		return i18n.Tf("Banner.Bisect.Found", shortHash(status.Current), status.Subject)
	case ops.BisectOnlySkipped:
		return i18n.Tf("Banner.Bisect.OnlySkipped", len(status.Candidates))
	case ops.BisectAmbiguous:
		return i18n.Tf("Banner.Bisect.Ambiguous", shortHash(status.Current))
	case ops.BisectMergeBase:
		return i18n.Tf("Banner.Bisect.MergeBase", origin, shortHash(status.Current))
	case ops.BisectTesting:
		return i18n.Tf("Banner.Bisect.Testing", origin, status.Remaining, status.Steps)
	}
	return i18n.Tf("Banner.Bisect.Active", origin)
}

func bisectAcceptsMarks(status ops.BisectStatus) bool {
	switch status.Outcome {
	case ops.BisectPending, ops.BisectTesting, ops.BisectMergeBase:
		return true
	}
	return false
}

func (a *App) workingBisectStatus(state ops.MergeState) ops.BisectStatus {
	if !state.Bisecting {
		return ops.BisectStatus{}
	}
	o := a.opened()
	if o == nil {
		return ops.BisectStatus{Start: state.BisectStart}
	}
	status, err := readBisectStatus(context.Background(), o.repo)
	if err != nil {
		a.log.Warn("read bisect status failed", "error", err)
		return ops.BisectStatus{Start: state.BisectStart}
	}
	return status
}
