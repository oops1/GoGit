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
	a.startKeyedRead("history:"+path, func(ctx context.Context, r *gitrepo.Repository) error {
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
	a.startKeyedRead("blame:"+rev+":"+path, func(ctx context.Context, r *gitrepo.Repository) error {
		read, err := readBlame(ctx, r, rev, path, ops.BlameOptions{})
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
	return a.startKeyedRead("", fn, onDone)
}

func (a *App) startKeyedRead(key string, fn readFunc, onDone func(error)) bool {
	o := a.opened()
	if o == nil {
		return false
	}
	ctx, ok := a.claimRead(key)
	if !ok {
		return false
	}
	r := o.repo
	a.readWG.Go(func() {
		err := fn(ctx, r)
		a.Post(func() {
			a.releaseRead(key)
			if ctx.Err() != nil {
				return
			}
			onDone(err)
		})
	})
	return true
}

func (a *App) claimRead(key string) (context.Context, bool) {
	a.readMu.Lock()
	defer a.readMu.Unlock()
	if a.readKeys[key] {
		return nil, false
	}
	if a.readKeys == nil {
		a.readKeys = map[string]bool{}
	}
	if key != "" {
		a.readKeys[key] = true
	}
	if a.readCtx == nil {
		a.readCtx, a.readCancel = context.WithCancel(context.Background())
	}
	return a.readCtx, true
}

func (a *App) releaseRead(key string) {
	a.readMu.Lock()
	delete(a.readKeys, key)
	a.readMu.Unlock()
}

func (a *App) cancelReads() {
	a.readMu.Lock()
	cancel := a.readCancel
	a.readCtx, a.readCancel = nil, nil
	a.readMu.Unlock()
	if cancel != nil {
		cancel()
	}
}
