package app

import (
	"slices"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/ui/branches"
	"github.com/oops1/gogit/internal/ui/journal"
)

const allBranchesKey = "Journal.Filter.AllBranches"

func (a *App) wireJournalFilter() {
	a.journalFilterBranch.OnChange = func(int, string) { a.onJournalFilterChanged() }
	a.journalFilterAuthor.OnChange = func(string) { a.onJournalFilterChanged() }
	a.journalFilterMessage.OnChange = func(string) { a.onJournalFilterChanged() }
	a.showJournalFilterCount(0, false)
}

func (a *App) onJournalFilterChanged() {
	a.startJournal()
}

func (a *App) journalFilter() journal.Filter {
	branch := a.journalFilterBranch.SelectedText()
	filter := journal.Filter{
		Author:  a.journalFilterAuthor.GetText(),
		Message: a.journalFilterMessage.GetText(),
	}
	if branch != "" && branch != i18n.T(allBranchesKey) {
		filter.Branch = branch
		filter.Tip = a.branchTipOf(branch)
	}
	return filter
}

func (a *App) branchTipOf(short string) hash.ObjectID {
	o := a.opened()
	if o == nil {
		return hash.Zero
	}
	snap, err := loadBranchSnapshot(o.store)
	if err != nil {
		a.log.Warn("read branches for the journal filter failed", "error", err)
		return hash.Zero
	}
	for _, branch := range everyBranch(snap) {
		if branch.Name.Short() == short {
			return branch.Target
		}
	}
	return hash.Zero
}

func everyBranch(snap branches.Snapshot) []branches.Branch {
	out := slices.Clone(snap.Local)
	for _, remote := range snap.Remotes {
		out = append(out, remote.Branches...)
	}
	return out
}

func (a *App) showJournalBranches(snap branches.Snapshot) {
	names := []string{i18n.T(allBranchesKey)}
	for _, branch := range everyBranch(snap) {
		names = append(names, branch.Name.Short())
	}
	chosen := a.journalFilterBranch.SelectedText()
	a.journalFilterBranch.SetItems(names)
	a.journalFilterBranch.SetSelected(max(slices.Index(names, chosen), 0))
}

func (a *App) showJournalFilterCount(shown int, filtered bool) {
	if filtered {
		a.journalFilterLabel.SetText(i18n.Tf("Journal.Filter.Found", shown))
		return
	}
	a.journalFilterLabel.SetText(i18n.Tf("Journal.Filter.Count", shown))
}
