package app

import (
	"cmp"
	"context"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/gitcore/linelog"
	"github.com/oops1/gogit/internal/gitcore/ops"
	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/gitdiff"
	"github.com/oops1/gogit/internal/ui/investigate"
)

const investigateProgressStep = 256

var newInvestigateView = investigate.NewView

var readLineHistory = ops.LineHistory

var readLineSpecs = ops.LineHistorySpecs

type investigation struct {
	rev       string
	selection ops.LineSelection
}

func (a *App) investigateFileItem(path string, enabled bool) widget.MenuItem {
	return enabledItem("Menu.Files.Investigate", func() { a.investigateFile(path) }, enabled && path != "" && a.opened() != nil)
}

func (a *App) investigateFile(path string) {
	a.investigate(investigation{rev: a.filesRevision(), selection: ops.LineSelection{Path: path}})
}

func (a *App) filesRevision() string {
	a.filesMu.Lock()
	mode := a.filesMode
	a.filesMu.Unlock()
	if commit := a.selectedCommitID(); mode == filesModeCommit && !commit.IsZero() {
		return commit.String()
	}
	return "HEAD"
}

func (a *App) investigateLinesItem(target diffTarget, spot gitdiff.Spot) widget.MenuItem {
	found, ok := a.diffInvestigation(target, spot)
	return enabledItem("Menu.Files.Investigate", func() { a.investigate(found) }, ok)
}

func (a *App) diffInvestigation(target diffTarget, spot gitdiff.Spot) (investigation, bool) {
	left := spot.Side == widget.DiffLeft
	path, shown := cmp.Or(target.file.NewPath, target.file.OldPath), target.newData
	if left {
		path, shown = cmp.Or(target.file.OldPath, target.file.NewPath), target.oldData
	}
	if a.opened() == nil || target.file.Binary || len(shown) == 0 || spot.From < 0 {
		return investigation{}, false
	}
	rev := "HEAD"
	if target.kind == diffKindNone {
		commit := a.selectedCommitID()
		if commit.IsZero() {
			return investigation{}, false
		}
		rev = commit.String()
		if left {
			rev += "^"
		}
	}
	selection := ops.LineSelection{Path: path, First: spot.From + 1, Last: spot.To, Shown: shown}
	return investigation{rev: rev, selection: selection}, true
}

func (a *App) investigateBlamedLine(rev, path string, number int) {
	a.investigate(investigation{rev: rev, selection: ops.LineSelection{Path: path, First: number, Last: number}})
}

func (a *App) investigate(target investigation) {
	var specs []linelog.Spec
	a.startRead(func(ctx context.Context, r *gitrepo.Repository) error {
		read, err := readLineSpecs(ctx, r, target.rev, target.selection)
		specs = read
		return err
	}, func(err error) {
		switch {
		case err != nil:
			a.log.Warn("investigate lines failed", "path", target.selection.Path, "error", err)
			a.statusLabel.SetText(i18n.Tf("Status.InvestigateFailed", err))
		case len(specs) == 0:
			a.statusLabel.SetText(i18n.T("Status.InvestigateUncommitted"))
		default:
			a.showInvestigation(target, specs)
		}
	})
}

func (a *App) showInvestigation(target investigation, specs []linelog.Spec) {
	o := a.opened()
	if o == nil {
		return
	}
	view, err := newInvestigateView()
	if err != nil {
		a.log.Warn("open investigate dialog failed", "error", err)
		a.statusLabel.SetText(i18n.Tf("Status.InvestigateFailed", err))
		return
	}
	base, _ := a.claimRead("")
	ctx, cancel := context.WithCancel(base)
	view.SetTarget(target.selection.Path, linesLabel(target.selection), target.rev)
	view.Start()
	view.OnCancel = cancel
	view.OnClose = func() {
		cancel()
		a.eng.CloseModal(view.Dialog())
	}
	if bounds := a.root.Bounds(); !bounds.Empty() {
		view.FitWithin(bounds.Dx(), bounds.Dy())
	}
	a.showModal(view.Dialog(), view)
	r := o.repo
	a.readWG.Go(func() {
		err := a.collectInvestigation(ctx, r, target.rev, specs, view)
		a.Post(func() {
			cancel()
			view.Finish(err)
		})
	})
}

func (a *App) collectInvestigation(ctx context.Context, r *gitrepo.Repository, rev string, specs []linelog.Spec, view *investigate.View) error {
	opts := ops.LineHistoryOptions{Progress: func(done, total int) {
		if done%investigateProgressStep == 0 {
			a.Post(func() { view.Progress(done, total) })
		}
	}}
	for entry, err := range readLineHistory(ctx, r, rev, specs, opts) {
		if err != nil {
			return err
		}
		row := investigateEntry(entry)
		a.Post(func() { view.Append(row) })
	}
	return nil
}

func investigateEntry(entry ops.LineHistoryEntry) investigate.Entry {
	return investigate.Entry{
		Commit:  entry.Commit,
		Author:  entry.Author.Name,
		When:    entry.Author.When,
		Subject: entry.Subject,
		Merge:   len(entry.Parents) > 1,
		Files:   entry.Files,
	}
}

func linesLabel(selection ops.LineSelection) string {
	if selection.First < 1 {
		return i18n.T("Dialog.Investigate.WholeFile")
	}
	return i18n.Tf("Dialog.Investigate.Lines", selection.First, max(selection.First, selection.Last))
}
