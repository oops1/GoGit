package app

import (
	"context"

	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/gitcore/ops"
	"github.com/oops1/gogit/internal/gitcore/refs"
	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/reflog"
)

var newReflogView = reflog.NewView

var readReflog = ops.Reflog

func (a *App) registerReflogHandlers() {
	a.handlers[CmdReflog] = func() { a.openReflog("HEAD") }
}

func (a *App) reflogItems(ref refs.Name) []widget.MenuItem {
	if !ref.IsBranch() {
		return nil
	}
	item := menuItem("Menu.Context.Reflog", func() { a.openReflog(ref.Short()) })
	item.Disabled = a.State().ActiveRepository == ""
	return []widget.MenuItem{item}
}

func (a *App) openReflog(name string) {
	o := a.opened()
	if o == nil {
		return
	}
	var records []ops.ReflogRecord
	a.startRead(func(ctx context.Context, r *gitrepo.Repository) error {
		read, err := readReflog(ctx, r, name, ops.ReflogOptions{})
		records = read
		return err
	}, func(err error) {
		if err != nil {
			a.log.Warn("read reflog failed", "ref", name, "error", err)
			a.statusLabel.SetText(i18n.Tf("Status.ReflogFailed", err))
			return
		}
		a.showReflog(name, records)
	})
}

func (a *App) showReflog(name string, records []ops.ReflogRecord) {
	view, err := newReflogView()
	if err != nil {
		a.log.Warn("open reflog dialog failed", "error", err)
		a.statusLabel.SetText(i18n.Tf("Status.ReflogFailed", err))
		return
	}
	view.SetRecords(name, reflogRows(records))
	view.OnReset = func(record reflog.Record) {
		a.eng.CloseModal(view.Dialog())
		a.openReset(record.Commit)
	}
	view.OnClose = func() { a.eng.CloseModal(view.Dialog()) }
	a.showModal(view.Dialog(), view)
}

func reflogRows(records []ops.ReflogRecord) []reflog.Record {
	rows := make([]reflog.Record, 0, len(records))
	for _, record := range records {
		rows = append(rows, reflog.Record{
			Selector: record.Selector(),
			Commit:   record.New,
			When:     record.Committer.When,
			Who:      record.Committer.Name,
			Message:  record.Message,
		})
	}
	return rows
}
