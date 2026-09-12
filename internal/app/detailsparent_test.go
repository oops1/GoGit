package app

import (
	"testing"

	"github.com/oops1/headless-gui/v3/widget/datagrid"

	"github.com/oops1/gogit/internal/gitcore/hash"
	"github.com/oops1/gogit/internal/ui/journal"
)

func TestTheParentButtonSelectsThatCommitInTheJournal(t *testing.T) {
	a := newTestApp(t)
	first := hash.SumSHA1("commit", []byte("first"))
	second := hash.SumSHA1("commit", []byte("second"))
	grid := a.journalGrid().Grid

	readOnDispatcher(t, a, func() bool {
		grid.SetItemsSource(datagrid.NewObservableCollectionFrom([]any{"not a row", journal.Row{ID: first}, journal.Row{ID: second}}))
		a.selectJournalCommit(second)
		return true
	})

	selected := readOnDispatcher(t, a, grid.SelectedItem)
	if row, ok := selected.(journal.Row); !ok || row.ID != second {
		t.Fatalf("selected = %v, want the parent", selected)
	}
}

func TestAParentMissingFromTheJournalChangesNothing(t *testing.T) {
	a := newTestApp(t)
	grid := a.journalGrid().Grid

	readOnDispatcher(t, a, func() bool {
		grid.SetItemsSource(datagrid.NewObservableCollectionFrom([]any{journal.Row{ID: hash.SumSHA1("commit", []byte("x"))}}))
		grid.SetSelectedIndex(0)
		a.selectJournalCommit(hash.SumSHA1("commit", []byte("missing")))
		return true
	})

	selected := readOnDispatcher(t, a, grid.SelectedItem)
	if row, ok := selected.(journal.Row); !ok || row.ID != hash.SumSHA1("commit", []byte("x")) {
		t.Fatalf("selected = %v, want the row that was already selected", selected)
	}
}
