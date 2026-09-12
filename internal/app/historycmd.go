package app

import (
	"context"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/gitcore/blame"
	"github.com/oops1/gogit/internal/gitcore/ops"
	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/blameview"
	"github.com/oops1/gogit/internal/ui/filehistory"
)

var newFileHistoryView = filehistory.NewView

var newBlameView = blameview.NewView

var readFileHistory = ops.FileHistory

var readBlame = ops.Blame

func (a *App) historyItems(path string) []widget.MenuItem {
	if path == "" {
		return nil
	}
	history := menuItem("Menu.Context.FileHistory", func() { a.openFileHistory(path) })
	lines := menuItem("Menu.Context.Blame", func() { a.openBlame("HEAD", path) })
	enabled := a.State().ActiveRepository != ""
	history.Disabled, lines.Disabled = !enabled, !enabled
	return []widget.MenuItem{menuSeparator(), history, lines}
}

func (a *App) openFileHistory(path string) {
	o := a.opened()
	if o == nil {
		return
	}
	var entries []ops.HistoryEntry
	a.startRead(func(ctx context.Context, r *gitrepo.Repository) error {
		read, err := readFileHistory(ctx, r, "HEAD", path, ops.HistoryOptions{Follow: true})
		entries = read
		return err
	}, func(err error) {
		if err != nil {
			a.log.Warn("read file history failed", "path", path, "error", err)
			a.statusLabel.SetText(i18n.Tf("Status.FileHistoryFailed", err))
			return
		}
		a.showFileHistory(path, entries)
	})
}

func (a *App) showFileHistory(path string, entries []ops.HistoryEntry) {
	view, err := newFileHistoryView()
	if err != nil {
		a.log.Warn("open file history dialog failed", "error", err)
		a.statusLabel.SetText(i18n.Tf("Status.FileHistoryFailed", err))
		return
	}
	view.SetEntries(path, historyRows(entries))
	view.OnBlame = func(entry filehistory.Entry) {
		a.eng.CloseModal(view.Dialog())
		a.openBlame(entry.Commit.String(), entry.Path)
	}
	view.OnClose = func() { a.eng.CloseModal(view.Dialog()) }
	a.showModal(view.Dialog(), view)
}

func historyRows(entries []ops.HistoryEntry) []filehistory.Entry {
	rows := make([]filehistory.Entry, 0, len(entries))
	for _, entry := range entries {
		rows = append(rows, filehistory.Entry{
			Commit:  entry.Commit,
			Author:  entry.Author.Name,
			When:    entry.When.When,
			Subject: entry.Subject,
			Path:    entry.Path,
			Old:     entry.Old,
		})
	}
	return rows
}

func (a *App) openBlame(rev, path string) {
	o := a.opened()
	if o == nil {
		return
	}
	var result blame.Result
	a.startRead(func(ctx context.Context, r *gitrepo.Repository) error {
		read, err := readBlame(ctx, r, rev, path, ops.BlameOptions{Follow: true})
		result = read
		return err
	}, func(err error) {
		if err != nil {
			a.log.Warn("blame failed", "path", path, "error", err)
			a.statusLabel.SetText(i18n.Tf("Status.BlameFailed", err))
			return
		}
		a.showBlame(path, result)
	})
}

func (a *App) showBlame(path string, result blame.Result) {
	view, err := newBlameView()
	if err != nil {
		a.log.Warn("open blame dialog failed", "error", err)
		a.statusLabel.SetText(i18n.Tf("Status.BlameFailed", err))
		return
	}
	view.SetLines(path, blameRows(result))
	view.OnClose = func() { a.eng.CloseModal(view.Dialog()) }
	a.showModal(view.Dialog(), view)
}

func blameRows(result blame.Result) []blameview.Line {
	rows := make([]blameview.Line, 0, len(result.Lines))
	for _, line := range result.Lines {
		rows = append(rows, blameview.Line{
			Commit:  line.Commit,
			Author:  line.Author.Name,
			When:    line.Author.When,
			Summary: line.Summary,
			Path:    line.Path,
			Number:  line.Number,
			Text:    line.Text,
		})
	}
	return rows
}

type readFunc func(ctx context.Context, r *gitrepo.Repository) error

func (a *App) startRead(fn readFunc, onDone func(error)) bool {
	o := a.opened()
	if o == nil {
		return false
	}
	r := o.repo
	a.readWG.Go(func() {
		err := fn(context.Background(), r)
		a.Post(func() { onDone(err) })
	})
	return true
}
