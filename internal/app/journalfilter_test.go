package app

import (
	"errors"
	"testing"

	"github.com/oops1/gogit/internal/gitcore/refs"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/branches"
	"github.com/oops1/gogit/internal/ui/journal"
)

func journalFilterOf(t *testing.T, a *App) journal.Filter {
	t.Helper()
	return readOnDispatcher(t, a, a.journalFilter)
}

func TestTheJournalFilterStartsOnEveryBranch(t *testing.T) {
	a, _ := forkedApp(t, false)
	waitForJournalRows(t, a, 1)

	filter := journalFilterOf(t, a)

	if !filter.Empty() {
		t.Fatalf("filter = %+v", filter)
	}
	if got := readOnDispatcher(t, a, a.journalFilterBranch.SelectedText); got != i18n.T(allBranchesKey) {
		t.Fatalf("branch = %q", got)
	}
	if got := readOnDispatcher(t, a, a.journalFilterLabel.Text); got == "" {
		t.Fatal("the journal shows no count")
	}
}

func TestTheJournalFilterListsTheBranches(t *testing.T) {
	a, _ := forkedApp(t, false)
	waitForJournalRows(t, a, 1)

	readOnDispatcher(t, a, func() bool {
		a.journalFilterBranch.SetSelected(1)
		a.journalFilterBranch.OnChange(1, a.journalFilterBranch.SelectedText())
		return true
	})

	filter := journalFilterOf(t, a)
	if filter.Branch == "" || filter.Tip.IsZero() {
		t.Fatalf("filter = %+v", filter)
	}
	waitForJournalRows(t, a, 1)
}

func TestTheJournalFilterNarrowsByAuthorAndMessage(t *testing.T) {
	a, _ := forkedApp(t, false)
	waitForJournalRows(t, a, 1)

	readOnDispatcher(t, a, func() bool {
		a.journalFilterAuthor.SetText("nobody at all")
		a.journalFilterAuthor.OnChange("nobody at all")
		return true
	})
	waitForPostQueueDrain(t, a)

	if got := readOnDispatcher(t, a, a.journalView.Count); got != 0 {
		t.Fatalf("rows = %d", got)
	}
	if got := readOnDispatcher(t, a, a.journalFilterLabel.Text); got != i18n.Tf("Journal.Filter.Found", 0) {
		t.Fatalf("count = %q", got)
	}

	readOnDispatcher(t, a, func() bool {
		a.journalFilterAuthor.SetText("")
		a.journalFilterAuthor.OnChange("")
		a.journalFilterMessage.SetText("nothing like this")
		a.journalFilterMessage.OnChange("nothing like this")
		return true
	})
	waitForPostQueueDrain(t, a)

	if got := readOnDispatcher(t, a, a.journalView.Count); got != 0 {
		t.Fatalf("rows = %d", got)
	}
}

func TestTheJournalFilterFindsTheCommitsItIsAskedFor(t *testing.T) {
	a, _ := forkedApp(t, false)
	waitForJournalRows(t, a, 1)

	readOnDispatcher(t, a, func() bool {
		a.journalFilterMessage.SetText("ours")
		a.journalFilterMessage.OnChange("ours")
		return true
	})

	waitForJournalRows(t, a, 1)
	if got := readOnDispatcher(t, a, a.journalFilterLabel.Text); got != i18n.Tf("Journal.Filter.Found", readOnDispatcher(t, a, a.journalView.Count)) {
		t.Fatalf("count = %q", got)
	}
}

func TestAnUnknownBranchInTheFilterHasNoTip(t *testing.T) {
	a, _ := forkedApp(t, false)

	if got := readOnDispatcher(t, a, func() bool { return a.branchTipOf("nowhere").IsZero() }); !got {
		t.Fatal("an unknown branch has a tip")
	}
}

func TestTheJournalFilterSurvivesWithoutARepository(t *testing.T) {
	a := newTestApp(t)

	if got := a.branchTipOf("main"); !got.IsZero() {
		t.Fatalf("tip = %s", got)
	}
	if filter := a.journalFilter(); !filter.Empty() {
		t.Fatalf("filter = %+v", filter)
	}
}

func TestTheJournalFilterReportsBranchesItCannotRead(t *testing.T) {
	a, _ := forkedApp(t, false)
	prev := loadBranchSnapshot
	loadBranchSnapshot = func(*refs.Store) (branches.Snapshot, error) { return branches.Snapshot{}, errors.New("no refs") }
	t.Cleanup(func() { loadBranchSnapshot = prev })

	if got := readOnDispatcher(t, a, func() bool { return a.branchTipOf("main").IsZero() }); !got {
		t.Fatal("a failing snapshot gave a tip")
	}
}

func TestTheJournalFilterKnowsRemoteBranches(t *testing.T) {
	a, _ := forkedApp(t, false)
	snap := branches.Snapshot{
		Local:   []branches.Branch{{Name: refs.BranchName("main")}},
		Remotes: []branches.Remote{{Name: "origin", Branches: []branches.Branch{{Name: refs.RemoteBranchName("origin", "fresh")}}}},
	}

	readOnDispatcher(t, a, func() bool { a.showJournalBranches(snap); return true })

	items := readOnDispatcher(t, a, func() int { return len(a.journalFilterBranch.Items()) })
	if items != 3 {
		t.Fatalf("items = %d", items)
	}
}
