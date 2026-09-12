package app

import (
	"errors"
	"testing"
	"time"

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

func typeIntoJournalFilter(t *testing.T, a *App, input interface {
	SetText(string)
}, onChange func(string), text string) {
	t.Helper()
	readOnDispatcher(t, a, func() bool {
		input.SetText(text)
		onChange(text)
		return true
	})
	waitForPostQueueDrain(t, a)
}

func journalMessages(t *testing.T, a *App) []string {
	t.Helper()
	return readOnDispatcher(t, a, func() []string {
		var messages []string
		for index := range a.journalView.Count() {
			if row, ok := a.journalGrid().Grid.ItemsSource().Get(index).(journal.Row); ok {
				messages = append(messages, row.Message)
			}
		}
		return messages
	})
}

func TestTheJournalFilterNarrowsToTheCommitsThatTouchAPath(t *testing.T) {
	a, _ := forkedApp(t, false)
	waitForJournalRows(t, a, 2)

	typeIntoJournalFilter(t, a, a.journalFilterPath, a.journalFilterPath.OnChange, "keep.txt")
	waitForJournalRows(t, a, 1)

	if got := journalMessages(t, a); len(got) != 1 || got[0] != "base" {
		t.Fatalf("rows = %q, want only the commit that added keep.txt", got)
	}
}

func TestTheJournalFilterFindsTheCommitThatChangedAText(t *testing.T) {
	a, _ := forkedApp(t, false)
	waitForJournalRows(t, a, 2)

	typeIntoJournalFilter(t, a, a.journalFilterContent, a.journalFilterContent.OnChange, "OURS")
	waitForJournalRows(t, a, 1)

	if got := journalMessages(t, a); len(got) != 1 || got[0] != "ours" {
		t.Fatalf("rows = %q, want only the commit that wrote OURS", got)
	}
}

func TestTheJournalFilterSearchesTheChangesByRegularExpression(t *testing.T) {
	a, _ := forkedApp(t, false)
	waitForJournalRows(t, a, 2)
	readOnDispatcher(t, a, func() bool {
		a.journalFilterRegexp.SetChecked(true)
		a.journalFilterRegexp.OnChange(true)
		return true
	})

	typeIntoJournalFilter(t, a, a.journalFilterContent, a.journalFilterContent.OnChange, "^OU.S$")
	waitForJournalRows(t, a, 1)

	if got := journalMessages(t, a); len(got) != 1 || got[0] != "ours" {
		t.Fatalf("rows = %q", got)
	}

	typeIntoJournalFilter(t, a, a.journalFilterContent, a.journalFilterContent.OnChange, "OU(RS")

	if got := readOnDispatcher(t, a, a.journalFilterLabel.Text); got != i18n.T("Journal.Filter.BadPattern") {
		t.Fatalf("count = %q, want the pattern reported as unusable", got)
	}
	if got := readOnDispatcher(t, a, a.journalView.Count); got != 0 {
		t.Fatalf("rows = %d after an unusable pattern", got)
	}
}

func TestTheJournalFilterHidesCommitsOlderThanThePeriod(t *testing.T) {
	a, _ := forkedApp(t, false)
	waitForJournalRows(t, a, 1)
	prev := journalNow
	journalNow = func() time.Time { return time.Now().AddDate(5, 0, 0) }
	t.Cleanup(func() { journalNow = prev })

	readOnDispatcher(t, a, func() bool {
		a.journalFilterPeriod.SetSelected(1)
		a.journalFilterPeriod.OnChange(1, a.journalFilterPeriod.SelectedText())
		return true
	})
	waitForPostQueueDrain(t, a)

	if got := readOnDispatcher(t, a, a.journalView.Count); got != 0 {
		t.Fatalf("rows = %d, want none newer than a day before the far future", got)
	}
	if filter := journalFilterOf(t, a); filter.Since.IsZero() || filter.Empty() {
		t.Fatalf("filter = %+v", filter)
	}
}

func TestTheJournalPeriodsFollowTheLanguageAndKeepTheChoice(t *testing.T) {
	a := newTestApp(t)
	readOnDispatcher(t, a, func() bool { a.journalFilterPeriod.SetSelected(3); return true })

	readOnDispatcher(t, a, func() bool { a.SetLanguage("ru"); return true })

	items := readOnDispatcher(t, a, a.journalFilterPeriod.Items)
	if len(items) != len(journalPeriods) || items[0] != i18n.T("Journal.Filter.Period.All") {
		t.Fatalf("periods = %q", items)
	}
	if got := readOnDispatcher(t, a, a.journalFilterPeriod.Selected); got != 3 {
		t.Fatalf("selected period = %d, want the month kept across the language change", got)
	}
	readOnDispatcher(t, a, func() bool { a.SetLanguage("en"); return true })
}

func TestEveryJournalPeriodReachesBackTheWayItSays(t *testing.T) {
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	want := map[string]time.Time{
		"Journal.Filter.Period.Day":   now.AddDate(0, 0, -1),
		"Journal.Filter.Period.Week":  now.AddDate(0, 0, -7),
		"Journal.Filter.Period.Month": now.AddDate(0, -1, 0),
		"Journal.Filter.Period.Year":  now.AddDate(-1, 0, 0),
	}
	if journalPeriods[0].back != nil {
		t.Fatal("the whole history must not reach back to a date")
	}
	for _, period := range journalPeriods[1:] {
		if got := period.back(now); !got.Equal(want[period.key]) {
			t.Fatalf("%s reaches back to %v, want %v", period.key, got, want[period.key])
		}
	}
}
