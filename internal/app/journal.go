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
	a.stopJournal()
	a.journalView.Reset()
	if a.open == nil {
		return
	}
	source := revision.Context{Objects: a.open.db, Refs: a.open.store}
	opts := revision.Options{MaxCount: a.cfg.Git.LogMaxCount}
	ctx, cancel := context.WithCancel(context.Background())
	pager := newJournalPager(ctx, source, opts)
	more := make(chan struct{}, 1)
	a.journalMu.Lock()
	a.journalCancel = cancel
	a.journalMore = more
	more <- struct{}{}
	a.journalMu.Unlock()
	a.journalWG.Go(func() { a.runJournal(pager, more) })
}

func (a *App) runJournal(pager journalPager, more chan struct{}) {
	defer pager.Cancel()
	for range more {
		rows, done, err := pager.Next(a.journalPageSize)
		if err != nil && !errors.Is(err, context.Canceled) {
			a.log.Warn("load journal failed", "error", err)
		}
		if len(rows) > 0 {
			a.Post(func() { a.journalView.Append(rows) })
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
	a.commitSelected = true
	a.statusLabel.SetText(i18n.Tf("Status.CommitSelected", row.ShortHash, row.Message))
	a.startDiff(row.ID)
}
