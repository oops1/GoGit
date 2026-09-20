package app

import (
	"context"
	"path/filepath"
	"slices"
	"sync/atomic"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/gitcore/ops"
	"github.com/oops1/gogit/internal/gitcore/refs"
	gitrepo "github.com/oops1/gogit/internal/gitcore/repo"
	"github.com/oops1/gogit/internal/repo/watch"
	"github.com/oops1/gogit/internal/ui/branches"
	"github.com/oops1/gogit/internal/ui/journal"
)

func countBranchSnapshots(t *testing.T) *atomic.Int32 {
	t.Helper()
	loads := new(atomic.Int32)
	prev := loadBranchSnapshot
	loadBranchSnapshot = func(s *refs.Store) (branches.Snapshot, error) {
		loads.Add(1)
		return prev(s)
	}
	t.Cleanup(func() { loadBranchSnapshot = prev })
	return loads
}

func TestRefreshRequestsQueuedTogetherRefreshOnce(t *testing.T) {
	target := filepath.Join(t.TempDir(), "main")
	initTestRepoWithBranch(t, target, "main")
	a := activatedWorkingApp(t, target)
	waitForWorkingRows(t, a, 0)
	loads := countBranchSnapshots(t)
	runOnDispatcher(t, a, a.RefreshRepository)
	perRefresh := loads.Load()
	loads.Store(0)

	runOnDispatcher(t, a, func() {
		a.handleChangeSet(watch.ChangeSet{watch.Change{Kind: watch.Refs}: struct{}{}})
		a.requestRefresh()
		a.finishMerge()
	})
	drainPostQueue(t, a)

	if got := loads.Load(); got != perRefresh {
		t.Fatalf("snapshot loads = %d, want the %d of a single refresh", got, perRefresh)
	}
	runOnDispatcher(t, a, a.requestRefresh)
	drainPostQueue(t, a)
	if got := loads.Load(); got != 2*perRefresh {
		t.Fatalf("snapshot loads = %d, want a later request to refresh again", got)
	}
}

func countDetailsReads(t *testing.T) *atomic.Int32 {
	t.Helper()
	reads := new(atomic.Int32)
	prev := readDetails
	readDetails = func(ctx context.Context, r *gitrepo.Repository, rev string, opts ops.DetailsOptions) (ops.CommitDetails, error) {
		reads.Add(1)
		return prev(ctx, r, rev, opts)
	}
	t.Cleanup(func() { readDetails = prev })
	return reads
}

func TestSelectingTheShownCommitAgainKeepsItsLoadedDetails(t *testing.T) {
	a, _ := forkedApp(t, false)
	waitForJournalRows(t, a, 1)
	reads := countDetailsReads(t)
	row := journalRowOnDispatcher(t, a, 0)
	selectJournalRow(t, a, 0)
	waitForDetails(t, a, row.ID)

	selectJournalRow(t, a, 0)
	waitForReads(a)
	drainPostQueue(t, a)

	if got := reads.Load(); got != 1 {
		t.Fatalf("details reads = %d, want the shown commit kept without a reload", got)
	}
}

func journalSelectionIDs(a *App) []hash.ObjectID {
	var ids []hash.ObjectID
	for _, item := range a.journalGrid().Grid.SelectedItems() {
		if row, ok := item.(journal.Row); ok {
			ids = append(ids, row.ID)
		}
	}
	return ids
}

func TestARefreshPutsTheJournalSelectionBackOnTheShownCommitWithoutReloadingIt(t *testing.T) {
	a, _ := forkedApp(t, false)
	waitForJournalRows(t, a, 2)
	reads := countDetailsReads(t)
	row := journalRowOnDispatcher(t, a, 1)
	selectJournalRow(t, a, 1)
	waitForDetails(t, a, row.ID)
	runOnDispatcher(t, a, func() { a.journalGrid().Grid.SetSelectedIndexQuiet(0) })

	runOnDispatcher(t, a, a.RefreshRepository)

	waitOnDispatcher(t, a, func() bool { return a.journalView.Count() >= 2 })
	waitOnDispatcher(t, a, func() bool {
		return slices.Contains(journalSelectionIDs(a), row.ID)
	})
	waitForReads(a)
	drainPostQueue(t, a)
	if got := reads.Load(); got != 1 {
		t.Fatalf("details reads = %d, want the reselection to keep the loaded commit", got)
	}
	if !a.commitIsSelected() || a.selectedCommitID() != row.ID {
		t.Fatal("the refreshed journal must keep the selected commit")
	}
	if got := readOnDispatcher(t, a, func() []hash.ObjectID { return journalSelectionIDs(a) }); len(got) != 1 || got[0] != row.ID {
		t.Fatalf("journal selection = %v, want %s", got, row.ID)
	}
}
