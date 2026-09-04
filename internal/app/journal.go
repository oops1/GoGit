package app

import (
	"context"
	"errors"

	"github.com/oops1/gogit/internal/gitcore/revision"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/journal"
)

const defaultJournalPageSize = 200

type journalPager interface {
	Next(n int) ([]journal.Row, bool, error)
	Cancel()
}

var newJournalPager = func(ctx context.Context, source revision.Context, opts revision.Options) journalPager {
	return journal.NewPager(ctx, source, opts)
}

func (a *App) startJournal() {
	a.journalRunMu.Lock()
	defer a.journalRunMu.Unlock()
	a.stopJournalLocked()
	a.journalView.Reset()
	o := a.opened()
	if o == nil {
		return
	}
	source := revision.Context{Objects: o.db, Refs: o.store}
	opts := revision.Options{MaxCount: a.cfg.Git.LogMaxCount}
	ctx, cancel := context.WithCancel(context.Background())
	pager := newJournalPager(ctx, source, opts)
	more := make(chan struct{}, 1)
	a.journalMu.Lock()
	a.journalCancel = cancel
	a.journalMore = more
	more <- struct{}{}
	a.journalMu.Unlock()
	a.journalWG.Go(func() { a.runJournal(ctx, pager, more) })
}

func (a *App) runJournal(ctx context.Context, pager journalPager, more chan struct{}) {
	defer pager.Cancel()
	for range more {
		rows, done, err := pager.Next(a.journalPageSize)
		if err != nil && !errors.Is(err, context.Canceled) {
			a.log.Warn("load journal failed", "error", err)
		}
		if len(rows) > 0 {
			a.Post(func() {
				if ctx.Err() != nil {
					return
				}
				a.journalView.Append(rows)
			})
		}
		if done {
			return
		}
	}
}

func (a *App) requestMoreJournal() {
	a.journalMu.Lock()
	defer a.journalMu.Unlock()
	if a.journalMore == nil {
		return
	}
	select {
	case a.journalMore <- struct{}{}:
	default:
	}
}

func (a *App) stopJournal() {
	a.journalRunMu.Lock()
	defer a.journalRunMu.Unlock()
	a.stopJournalLocked()
}

func (a *App) stopJournalLocked() {
	a.journalMu.Lock()
	cancel := a.journalCancel
	more := a.journalMore
	a.journalCancel = nil
	a.journalMore = nil
	a.journalMu.Unlock()
	if cancel != nil {
		cancel()
	}
	if more != nil {
		close(more)
	}
	a.journalWG.Wait()
}

func (a *App) onJournalRowSelected(row journal.Row) {
	a.selectedCommit = row.ID
	a.setCommitSelected(true)
	a.setFilesSelected(false)
	a.statusLabel.SetText(i18n.Tf("Status.CommitSelected", row.ShortHash, row.Message))
	a.startDiff(row.ID)
}
